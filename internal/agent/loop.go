package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func (l *Loop) runLoop(ctx context.Context, req RunRequest) (*RunResult, error) {
	// Per-run emit wrapper: enriches every AgentEvent with delegation + routing context.
	emitRun := func(event AgentEvent) {
		event.RunKind = req.RunKind
		event.DelegationID = req.DelegationID
		event.TeamID = req.TeamID
		event.TeamTaskID = req.TeamTaskID
		event.ParentAgentID = req.ParentAgentID
		event.UserID = req.UserID
		event.Channel = req.Channel
		event.ChatID = req.ChatID
		l.emit(event)
	}

	// Inject agent UUID into context for tool routing
	if l.agentUUID != uuid.Nil {
		ctx = store.WithAgentID(ctx, l.agentUUID)
	}
	// Inject user ID into context for per-user scoping (memory, context files, etc.)
	if req.UserID != "" {
		ctx = store.WithUserID(ctx, req.UserID)
	}
	// Inject agent type into context for interceptor routing
	if l.agentType != "" {
		ctx = store.WithAgentType(ctx, l.agentType)
	}
	// Inject self-evolve flag for predefined agents that can update SOUL.md
	if l.selfEvolve {
		ctx = store.WithSelfEvolve(ctx, true)
	}
	// Inject original sender ID for group file writer permission checks
	if req.SenderID != "" {
		ctx = store.WithSenderID(ctx, req.SenderID)
	}
	// Inject global builtin tool settings for media tools (provider chain)
	if l.builtinToolSettings != nil {
		ctx = tools.WithBuiltinToolSettings(ctx, l.builtinToolSettings)
	}
	// Inject channel type into context for tools (e.g. message tool needs it for Zalo group routing)
	if req.ChannelType != "" {
		ctx = tools.WithToolChannelType(ctx, req.ChannelType)
	}
	// Inject per-agent overrides from DB so tools honor per-agent settings.
	if l.restrictToWs != nil {
		ctx = tools.WithRestrictToWorkspace(ctx, *l.restrictToWs)
	}
	if l.subagentsCfg != nil {
		ctx = tools.WithSubagentConfig(ctx, l.subagentsCfg)
	}
	// Pass the agent's model and provider so subagents inherit the correct combo.
	if l.model != "" {
		ctx = tools.WithParentModel(ctx, l.model)
	}
	if l.provider != nil {
		ctx = tools.WithParentProvider(ctx, l.provider.Name())
	}
	if l.memoryCfg != nil {
		ctx = tools.WithMemoryConfig(ctx, l.memoryCfg)
	}
	if l.sandboxCfg != nil {
		ctx = tools.WithSandboxConfig(ctx, l.sandboxCfg)
	}
	if l.shellDenyGroups != nil {
		ctx = store.WithShellDenyGroups(ctx, l.shellDenyGroups)
	}

	// Workspace scope propagation (delegation origin → workspace tools).
	if req.WorkspaceChannel != "" {
		ctx = tools.WithWorkspaceChannel(ctx, req.WorkspaceChannel)
	}
	if req.WorkspaceChatID != "" {
		ctx = tools.WithWorkspaceChatID(ctx, req.WorkspaceChatID)
	}
	if req.TeamTaskID != "" {
		ctx = tools.WithTeamTaskID(ctx, req.TeamTaskID)
	}

	// Per-user workspace isolation.
	// Workspace path comes from user_agent_profiles (includes channel segment
	// for cross-channel isolation). Cached in userWorkspaces to avoid repeated DB queries.
	isTeamSession := bootstrap.IsTeamSession(req.SessionKey)
	if l.workspace != "" && req.UserID != "" {
		cachedWs, loaded := l.userWorkspaces.Load(req.UserID)
		if !loaded {
			// First request for this user: get/create profile → returns stored workspace.
			// Also seeds per-user context files on first chat.
			// Team-dispatched sessions skip seeding — members process tasks with full
			// capabilities, no bootstrap/user onboarding needed.
			ws := l.workspace
			if l.ensureUserFiles != nil && !isTeamSession {
				var err error
				ws, err = l.ensureUserFiles(ctx, l.agentUUID, req.UserID, l.agentType, l.workspace, req.Channel)
				if err != nil {
					slog.Warn("failed to ensure user context files", "error", err)
					ws = l.workspace
				}
			}
			// Expand ~ and convert to absolute for filesystem operations.
			ws = config.ExpandHome(ws)
			if !filepath.IsAbs(ws) {
				ws, _ = filepath.Abs(ws)
			}
			l.userWorkspaces.Store(req.UserID, ws)
			cachedWs = ws
		}
		effectiveWorkspace := cachedWs.(string)
		if !l.shouldShareWorkspace(req.UserID, req.PeerKind) {
			effectiveWorkspace = filepath.Join(effectiveWorkspace, sanitizePathSegment(req.UserID))
		}
		if l.shouldShareMemory() {
			ctx = store.WithSharedMemory(ctx)
		}
		if l.shouldShareKnowledgeGraph() {
			ctx = store.WithSharedKG(ctx)
		}
		if err := os.MkdirAll(effectiveWorkspace, 0755); err != nil {
			slog.Warn("failed to create user workspace directory", "workspace", effectiveWorkspace, "user", req.UserID, "error", err)
		}
		ctx = tools.WithToolWorkspace(ctx, effectiveWorkspace)
	} else if l.workspace != "" {
		ctx = tools.WithToolWorkspace(ctx, l.workspace)
	}

	// Team workspace handling:
	// - Dispatched task (req.TeamWorkspace set): override default workspace so
	//   relative paths resolve to team workspace. Agent workspace is accessible
	//   via ToolTeamWorkspace for absolute-path access.
	// - Direct chat (auto-resolved): keep agent workspace as default, team
	//   workspace accessible via absolute path.
	if req.TeamWorkspace != "" {
		if err := os.MkdirAll(req.TeamWorkspace, 0755); err != nil {
			slog.Warn("failed to create team workspace directory", "workspace", req.TeamWorkspace, "error", err)
		}
		ctx = tools.WithToolTeamWorkspace(ctx, req.TeamWorkspace)
		ctx = tools.WithToolWorkspace(ctx, req.TeamWorkspace) // default for relative paths
	}
	if req.TeamID != "" {
		ctx = tools.WithToolTeamID(ctx, req.TeamID)
	}

	// Auto-resolve team workspace for agents not dispatched via team task.
	// Lead agents default to team workspace (primary job is team coordination).
	// Non-lead members keep own workspace; team workspace is accessible via absolute path.
	// resolvedTeamSettings caches team settings from workspace resolution
	// to avoid re-querying when checking slow_tool notification config.
	var resolvedTeamSettings json.RawMessage
	if req.TeamWorkspace == "" && l.teamStore != nil && l.agentUUID != uuid.Nil {
		if team, _ := l.teamStore.GetTeamForAgent(ctx, l.agentUUID); team != nil {
			resolvedTeamSettings = team.Settings
			// Shared workspace: scope by teamID only. Isolated (default): scope by chatID too.
			wsChat := req.ChatID
			if wsChat == "" {
				wsChat = req.UserID
			}
			if tools.IsSharedWorkspace(team.Settings) {
				wsChat = ""
			}
			if wsDir, err := tools.WorkspaceDir(l.dataDir, team.ID, wsChat); err == nil {
				ctx = tools.WithToolTeamWorkspace(ctx, wsDir)
				if team.LeadAgentID == l.agentUUID {
					ctx = tools.WithToolWorkspace(ctx, wsDir)
				}
			}
			if req.TeamID == "" {
				ctx = tools.WithToolTeamID(ctx, team.ID.String())
			}
		}
	}

	// Persist agent UUID + user ID on the session (for querying/tracing)
	if l.agentUUID != uuid.Nil || req.UserID != "" {
		l.sessions.SetAgentInfo(req.SessionKey, l.agentUUID, req.UserID)
	}

	// Security: scan user message for injection patterns.
	// Action is configurable: "log" (info), "warn" (default), "block" (reject message).
	if l.inputGuard != nil {
		if matches := l.inputGuard.Scan(req.Message); len(matches) > 0 {
			matchStr := strings.Join(matches, ",")
			switch l.injectionAction {
			case "block":
				slog.Warn("security.injection_blocked",
					"agent", l.id, "user", req.UserID,
					"patterns", matchStr, "message_len", len(req.Message),
				)
				return nil, fmt.Errorf("message blocked: potential prompt injection detected (%s)", matchStr)
			case "log":
				slog.Info("security.injection_detected",
					"agent", l.id, "user", req.UserID,
					"patterns", matchStr, "message_len", len(req.Message),
				)
			default: // "warn"
				slog.Warn("security.injection_detected",
					"agent", l.id, "user", req.UserID,
					"patterns", matchStr, "message_len", len(req.Message),
				)
			}
		}
	}

	// Inject agent key into context for tool-level resolution (multiple agents share tool registry)
	ctx = tools.WithToolAgentKey(ctx, l.id)

	// Security: truncate oversized user messages gracefully (feed truncation notice into LLM)
	maxChars := l.maxMessageChars
	if maxChars <= 0 {
		maxChars = config.DefaultMaxMessageChars
	}
	if len(req.Message) > maxChars {
		originalLen := len(req.Message)
		req.Message = req.Message[:maxChars] +
			fmt.Sprintf("\n\n[System: Message was truncated from %d to %d characters due to size limit. "+
				"Please ask the user to send shorter messages or use the read_file tool for large content.]",
				originalLen, maxChars)
		slog.Warn("security.message_truncated",
			"agent", l.id, "user", req.UserID,
			"original_len", originalLen, "truncated_to", maxChars,
		)
	}

	// 0. Cache agent's context window on the session (first run only).
	// Enables scheduler's adaptive throttle to use the real value instead of hardcoded 200K.
	if l.sessions.GetContextWindow(req.SessionKey) <= 0 {
		l.sessions.SetContextWindow(req.SessionKey, l.contextWindow)
	}

	// 0b. Load adaptive tool timing from session metadata.
	toolTiming := ParseToolTiming(l.sessions.GetSessionMetadata(req.SessionKey))

	// Resolve slow_tool notification config from already-loaded team settings (no extra DB query).
	slowToolEnabled := tools.ParseTeamNotifyConfig(resolvedTeamSettings).SlowTool

	// 1. Build messages from session history
	history := l.sessions.GetHistory(req.SessionKey)
	summary := l.sessions.GetSummary(req.SessionKey)

	// buildMessages resolves context files once and also detects BOOTSTRAP.md presence
	// (hadBootstrap) — no extra DB roundtrip needed for bootstrap detection.
	messages, hadBootstrap := l.buildMessages(ctx, history, summary, req.Message, req.ExtraSystemPrompt, req.SessionKey, req.Channel, req.ChannelType, req.PeerKind, req.UserID, req.HistoryLimit, req.SkillFilter, req.LightContext)

	// 1b. Determine image routing strategy.
	// If read_image tool has a dedicated vision provider, images are NOT attached inline
	// to the main LLM — the agent calls read_image tool instead. This avoids sending
	// images to providers that don't support vision or have strict content filters.
	deferToReadImageTool := l.hasReadImageProvider()

	if !deferToReadImageTool {
		// Inline mode: reload historical images directly into messages for main provider.
		l.reloadMediaForMessages(messages, maxMediaReloadMessages)
	}

	// 2. Process media: sanitize images, persist to media store.
	var mediaRefs []providers.MediaRef
	if len(req.Media) > 0 {
		mediaRefs = l.persistMedia(req.SessionKey, req.Media)
		// Load current-turn images from persisted refs.
		var imageFiles []bus.MediaFile
		for _, ref := range mediaRefs {
			if ref.Kind == "image" {
				if p, err := l.mediaStore.LoadPath(ref.ID); err == nil {
					imageFiles = append(imageFiles, bus.MediaFile{Path: p, MimeType: ref.MimeType})
				}
			}
		}
		if images := loadImages(imageFiles); len(images) > 0 {
			if deferToReadImageTool {
				// Tool mode: store in context only — agent calls read_image tool.
				ctx = tools.WithMediaImages(ctx, images)
				slog.Info("vision: deferring to read_image tool", "count", len(images), "agent", l.id)
			} else {
				// Inline mode: attach to message + context.
				messages[len(messages)-1].Images = images
				ctx = tools.WithMediaImages(ctx, images)
				slog.Info("vision: attached images inline to main provider", "count", len(images), "agent", l.id)
			}
		}
	}

	// 2a. Tool mode: also load historical images into context for read_image tool.
	// Without this, read_image can only see current-turn images, not previous turns.
	if deferToReadImageTool && l.mediaStore != nil {
		ctx = l.loadHistoricalImagesForTool(ctx, mediaRefs, messages)
	}

	// 2b. Collect document MediaRefs (historical + current) for read_document tool.
	// Historical first, current last — so refs[len-1] is always the most recent file.
	var docRefs []providers.MediaRef
	for i := len(messages) - 1; i >= 0; i-- {
		for _, ref := range messages[i].MediaRefs {
			if ref.Kind == "document" {
				docRefs = append(docRefs, ref)
			}
		}
	}
	for _, ref := range mediaRefs {
		if ref.Kind == "document" {
			docRefs = append(docRefs, ref)
		}
	}
	if len(docRefs) > 0 {
		ctx = tools.WithMediaDocRefs(ctx, docRefs)
		// Enrich the last user message with persisted file paths so skills can access
		// documents via exec (e.g. pypdf). Only for current-turn refs (just persisted).
		l.enrichDocumentPaths(messages, mediaRefs)
	}

	// 2c. Collect audio MediaRefs (historical + current) for read_audio tool.
	var audioRefs []providers.MediaRef
	for i := len(messages) - 1; i >= 0; i-- {
		for _, ref := range messages[i].MediaRefs {
			if ref.Kind == "audio" {
				audioRefs = append(audioRefs, ref)
			}
		}
	}
	for _, ref := range mediaRefs {
		if ref.Kind == "audio" {
			audioRefs = append(audioRefs, ref)
		}
	}
	if len(audioRefs) > 0 {
		ctx = tools.WithMediaAudioRefs(ctx, audioRefs)
		// Embed media IDs into <media:audio> tags so LLM can reference them.
		l.enrichAudioIDs(messages, mediaRefs)
	}

	// 2d. Collect video MediaRefs (historical + current) for read_video tool.
	var videoRefs []providers.MediaRef
	for i := len(messages) - 1; i >= 0; i-- {
		for _, ref := range messages[i].MediaRefs {
			if ref.Kind == "video" {
				videoRefs = append(videoRefs, ref)
			}
		}
	}
	for _, ref := range mediaRefs {
		if ref.Kind == "video" {
			videoRefs = append(videoRefs, ref)
		}
	}
	if len(videoRefs) > 0 {
		ctx = tools.WithMediaVideoRefs(ctx, videoRefs)
		// Embed media IDs into <media:video> tags so LLM can reference them.
		l.enrichVideoIDs(messages, mediaRefs)
	}

	// 2e. Enrich <media:image> tags with persisted media IDs so the LLM
	// knows images were received and stored (consistent with audio/video enrichment).
	l.enrichImageIDs(messages, mediaRefs)

	// 2f. Collect all media file paths for team workspace auto-collect.
	// When the leader calls team_tasks(create), these paths are copied to the
	// team workspace so members can access attached files.
	if len(mediaRefs) > 0 && l.mediaStore != nil {
		var mediaPaths []string
		for _, ref := range mediaRefs {
			if p, err := l.mediaStore.LoadPath(ref.ID); err == nil {
				mediaPaths = append(mediaPaths, p)
			}
		}
		if len(mediaPaths) > 0 {
			ctx = tools.WithRunMediaPaths(ctx, mediaPaths)
			// Extract original filenames from <media:document name="X" path="Y"> tags
			// in the last user message (enriched in step 2b above).
			if lastMsg := messages[len(messages)-1]; lastMsg.Role == "user" {
				if nameMap := tools.ExtractMediaNameMap(lastMsg.Content); len(nameMap) > 0 {
					ctx = tools.WithRunMediaNames(ctx, nameMap)
				}
			}
		}
	}

	// 2g. Cross-session task reminder: notify team leads about pending and in-progress tasks.
	// Stale recovery (expired lock → pending) is handled by the background TaskTicker.
	// Reminders are injected BEFORE the user message so the user's actual message is always
	// the last message — prevents trailing assistant messages that proxy providers reject.
	if l.teamStore != nil && l.agentUUID != uuid.Nil {
		if team, _ := l.teamStore.GetTeamForAgent(ctx, l.agentUUID); team != nil && team.LeadAgentID == l.agentUUID {
			if tasks, err := l.teamStore.ListTasks(ctx, team.ID, "newest", "active", req.UserID, "", "", 0, 0); err == nil {
				var stale []string
				var inProgress []string
				for _, t := range tasks {
					if t.Status == store.TeamTaskStatusPending {
						age := time.Since(t.CreatedAt).Truncate(time.Minute)
						stale = append(stale, fmt.Sprintf("- %s: \"%s\" (pending %s)", t.ID, t.Subject, age))
					}
					if t.Status == store.TeamTaskStatusInProgress {
						age := time.Since(t.UpdatedAt).Truncate(time.Minute)
						progressInfo := fmt.Sprintf("in progress %s", age)
						if t.ProgressPercent > 0 {
							if t.ProgressStep != "" {
								progressInfo = fmt.Sprintf("%d%% — %s, %s", t.ProgressPercent, t.ProgressStep, age)
							} else {
								progressInfo = fmt.Sprintf("%d%%, %s", t.ProgressPercent, age)
							}
						}
						inProgress = append(inProgress, fmt.Sprintf("- %s: \"%s\" (%s)", t.ID, t.Subject, progressInfo))
					}
				}
				var parts []string
				if len(stale) > 0 {
					parts = append(parts, fmt.Sprintf(
						"You have %d pending team task(s) awaiting dispatch:\n%s\n"+
							"These tasks will be auto-dispatched to available team members. If no longer needed, cancel with team_tasks action=cancel.",
						len(stale), strings.Join(stale, "\n")))
				}
				if len(inProgress) > 0 {
					parts = append(parts, fmt.Sprintf(
						"You have %d in-progress team task(s) being handled by team members:\n%s\n"+
							"Their results will arrive automatically. Do NOT cancel, re-create, or re-spawn these tasks.",
						len(inProgress), strings.Join(inProgress, "\n")))
				}
				if len(parts) > 0 {
					reminder := "[System] " + strings.Join(parts, "\n\n")
					// Pop user message, inject reminder, push user message back
					userMsg := messages[len(messages)-1]
					messages = messages[:len(messages)-1]
					messages = append(messages,
						providers.Message{Role: "user", Content: reminder},
						providers.Message{Role: "assistant", Content: "I see the task status. Let me handle accordingly."},
						userMsg,
					)
				}
			}
		}
	}

	// 2h. Member task reminder: inject task context for members working on dispatched tasks.
	// Caches task subject/number for mid-loop progress nudge (avoids extra DB query).
	var memberTaskSubject string
	var memberTaskNumber int
	if req.TeamTaskID != "" && l.teamStore != nil {
		if taskUUID, err := uuid.Parse(req.TeamTaskID); err == nil {
			if task, err := l.teamStore.GetTask(ctx, taskUUID); err == nil && task != nil {
				memberTaskSubject = task.Subject
				memberTaskNumber = task.TaskNumber
				reminder := fmt.Sprintf(
					"[System] You are working on team task #%d: %q. "+
						"Stay focused on this task. Your final response becomes the task result — make it clear and complete. "+
						"For long tasks, report progress: team_tasks(action=\"progress\", percent=50, text=\"status\").",
					task.TaskNumber, task.Subject)
				// Pop user message, inject reminder, push user message back
				userMsg := messages[len(messages)-1]
				messages = messages[:len(messages)-1]
				messages = append(messages,
					providers.Message{Role: "user", Content: reminder},
					providers.Message{Role: "assistant", Content: "Understood. I'll focus on this task and report progress."},
					userMsg,
				)
			}
		}
	}

	// 3. Buffer new messages — write to session only AFTER the run completes.
	// This prevents concurrent runs from seeing each other's in-progress messages.
	// NOTE: pendingMsgs stores text + lightweight MediaRefs (not base64 images).
	var pendingMsgs []providers.Message
	if !req.HideInput {
		pendingMsgs = append(pendingMsgs, providers.Message{
			Role:      "user",
			Content:   req.Message,
			MediaRefs: mediaRefs,
		})
	}

	// 4. Run LLM iteration loop
	var loopDetector toolLoopState // detects repeated no-progress tool calls
	var totalUsage providers.Usage
	iteration := 0
	totalToolCalls := 0
	var finalContent string
	var finalThinking string
	var asyncToolCalls []string    // track async spawn tool names for fallback
	var bootstrapWriteDetected bool // track if write_file was called during bootstrap
	var mediaResults []MediaResult // media files from tool MEDIA: results
	var deliverables []string      // actual content from tool outputs (for team task results)
	var blockReplies int           // count of block.reply events emitted (for dedup in consumer)
	var lastBlockReply string      // last block reply content

	// Mid-loop compaction: summarize in-memory messages when context exceeds threshold.
	// Uses same config as maybeSummarize (contextWindow * historyShare).
	var midLoopCompacted bool

	// Team task orphan detection: track team_tasks create vs spawn calls.
	// If the LLM creates tasks but forgets to spawn, inject a reminder.
	var teamTaskCreates int // count of team_tasks action=create calls
	var teamTaskSpawns int  // count of spawn calls with team_task_id

	// Skill evolution: budget pressure nudge state (sent at most once each per run).
	var skillNudge70Sent, skillNudge90Sent bool
	var skillPostscriptSent bool

	// Member progress nudge: remind dispatched members to report progress (every 6 iterations).

	// Inject retry hook so channels can update placeholder on LLM retries.
	ctx = providers.WithRetryHook(ctx, func(attempt, maxAttempts int, err error) {
		emitRun(AgentEvent{
			Type:    protocol.AgentEventRunRetrying,
			AgentID: l.id,
			RunID:   req.RunID,
			Payload: map[string]string{
				"attempt":     fmt.Sprintf("%d", attempt),
				"maxAttempts": fmt.Sprintf("%d", maxAttempts),
				"error":       err.Error(),
			},
		})
	})

	maxIter := l.maxIterations
	if req.MaxIterations > 0 && req.MaxIterations < maxIter {
		maxIter = req.MaxIterations
	}

	// Budget check: query monthly spent once before starting iterations.
	if l.budgetMonthlyCents > 0 && l.tracingStore != nil && l.agentUUID != uuid.Nil {
		now := time.Now().UTC()
		spent, err := l.tracingStore.GetMonthlyAgentCost(ctx, l.agentUUID, now.Year(), now.Month())
		if err == nil {
			spentCents := int(spent * 100)
			if spentCents >= l.budgetMonthlyCents {
				slog.Warn("agent budget exceeded", "agent", l.id, "spent_cents", spentCents, "budget_cents", l.budgetMonthlyCents)
				return nil, fmt.Errorf("monthly budget exceeded ($%.2f / $%.2f)", spent, float64(l.budgetMonthlyCents)/100)
			}
		}
	}

	for iteration < maxIter {
		iteration++

		slog.Debug("agent iteration", "agent", l.id, "iteration", iteration, "messages", len(messages))

		// Skill evolution: budget pressure nudges at 70% and 90% of iteration budget.
		// Ephemeral (in-memory only, not persisted to session) — LLM sees them during this run only.
		if l.skillEvolve && maxIter > 0 {
			locale := store.LocaleFromContext(ctx)
			iterPct := float64(iteration) / float64(maxIter)
			if iterPct >= 0.90 && !skillNudge90Sent {
				skillNudge90Sent = true
				messages = append(messages, providers.Message{
					Role:    "user",
					Content: i18n.T(locale, i18n.MsgSkillNudge90Pct),
				})
			} else if iterPct >= 0.70 && !skillNudge70Sent {
				skillNudge70Sent = true
				messages = append(messages, providers.Message{
					Role:    "user",
					Content: i18n.T(locale, i18n.MsgSkillNudge70Pct),
				})
			}
		}

		// Member progress nudge: remind to report progress every 6 iterations.
		// Suggests percent based on iteration ratio — model can adjust but has a baseline.
		if req.TeamTaskID != "" && memberTaskSubject != "" && iteration > 0 && iteration%6 == 0 {
			var nudge string
			if maxIter > 0 {
				suggestedPct := iteration * 100 / maxIter
				nudge = fmt.Sprintf(
					"[System] You are at iteration %d/%d (~%d%% of budget) working on task #%d: %q. "+
						"Report your progress now: team_tasks(action=\"progress\", percent=%d, text=\"what you've accomplished so far\"). "+
						"Adjust percent based on actual work completed.",
					iteration, maxIter, suggestedPct, memberTaskNumber, memberTaskSubject, suggestedPct)
			} else {
				nudge = fmt.Sprintf(
					"[System] You are at iteration %d working on task #%d: %q. "+
						"Report your progress now: team_tasks(action=\"progress\", percent=50, text=\"what you've accomplished so far\"). "+
						"Adjust percent based on actual work completed.",
					iteration, memberTaskNumber, memberTaskSubject)
			}
			messages = append(messages, providers.Message{Role: "user", Content: nudge})
		}

		// Iteration budget nudge: when model has used 75% of iterations without
		// producing any text response, warn it to start summarizing.
		if maxIter > 0 && iteration > 1 && iteration == maxIter*3/4 && finalContent == "" {
			messages = append(messages, providers.Message{
				Role:    "user",
				Content: "[System] You have used 75% of your iteration budget without providing a text response. Start summarizing your findings and respond to the user within the next few iterations.",
			})
		}

		// Inject iteration progress into context so tools can adapt (e.g. web_fetch reduces maxChars).
		iterCtx := tools.WithIterationProgress(ctx, tools.IterationProgress{
			Current: iteration,
			Max:     maxIter,
		})

		// Emit activity event: thinking phase
		emitRun(AgentEvent{
			Type:    protocol.AgentEventActivity,
			AgentID: l.id,
			RunID:   req.RunID,
			Payload: map[string]any{"phase": "thinking", "iteration": iteration},
		})

		// Build provider request with policy-filtered tools
		var toolDefs []providers.ToolDefinition
		var allowedTools map[string]bool
		if l.toolPolicy != nil {
			toolDefs = l.toolPolicy.FilterTools(l.tools, l.id, l.provider.Name(), l.agentToolPolicy, req.ToolAllow, false, false)
			allowedTools = make(map[string]bool, len(toolDefs))
			for _, td := range toolDefs {
				allowedTools[td.Function.Name] = true
			}
		} else {
			toolDefs = l.tools.ProviderDefs()
		}

		// Bootstrap mode: restrict API tool definitions to write_file only (open agents).
		// Predefined agents keep all tools — BOOTSTRAP.md guides behavior.
		if hadBootstrap && l.agentType != store.AgentTypePredefined {
			var bootstrapDefs []providers.ToolDefinition
			for _, td := range toolDefs {
				if bootstrapToolAllowlist[td.Function.Name] {
					bootstrapDefs = append(bootstrapDefs, td)
				}
			}
			toolDefs = bootstrapDefs
		}

		// Hide skill_manage from LLM when skill_evolve is off.
		// Tool stays in the registry (shared) but won't appear in API tool definitions.
		if !l.skillEvolve {
			filtered := toolDefs[:0:0]
			for _, td := range toolDefs {
				if td.Function.Name != "skill_manage" {
					filtered = append(filtered, td)
				}
			}
			toolDefs = filtered
		}

		// Hide channel-specific tools when channel type doesn't match.
		if req.ChannelType != "" {
			filtered := toolDefs[:0:0]
			for _, td := range toolDefs {
				if tool, ok := l.tools.Get(td.Function.Name); ok {
					if ca, ok := tool.(tools.ChannelAware); ok {
						if !slices.Contains(ca.RequiredChannelTypes(), req.ChannelType) {
							continue
						}
					}
				}
				filtered = append(filtered, td)
			}
			toolDefs = filtered
		}

		// Final iteration: strip all tools to force a text-only response.
		// Without this the model may keep requesting tools and exit with "...".
		if iteration == maxIter {
			toolDefs = nil
			messages = append(messages, providers.Message{
				Role:    "user",
				Content: "[System] Final iteration reached. Summarize all findings and respond to the user now. No more tool calls allowed.",
			})
		}

		// Use per-request model override if set (e.g. heartbeat uses cheaper model).
		model := l.model
		if req.ModelOverride != "" {
			model = req.ModelOverride
		}

		chatReq := providers.ChatRequest{
			Messages: messages,
			Tools:    toolDefs,
			Model:    model,
			Options: map[string]any{
				providers.OptMaxTokens:   l.effectiveMaxTokens(),
				providers.OptTemperature: config.DefaultTemperature,
				providers.OptSessionKey:  req.SessionKey,
				providers.OptAgentID:     l.agentUUID.String(),
				providers.OptUserID:      req.UserID,
				providers.OptChannel:     req.Channel,
				providers.OptChatID:      req.ChatID,
				providers.OptPeerKind:    req.PeerKind,
			},
		}
		if l.thinkingLevel != "" && l.thinkingLevel != "off" {
			if tc, ok := l.provider.(providers.ThinkingCapable); ok && tc.SupportsThinking() {
				chatReq.Options[providers.OptThinkingLevel] = l.thinkingLevel
			} else {
				slog.Debug("thinking_level ignored: provider does not support thinking",
					"provider", l.provider.Name(), "level", l.thinkingLevel)
			}
		}

		// Call LLM (streaming or non-streaming)
		var resp *providers.ChatResponse
		var err error

		llmSpanStart := time.Now().UTC()
		llmSpanID := l.emitLLMSpanStart(ctx, llmSpanStart, iteration, messages)

		if req.Stream {
			resp, err = l.provider.ChatStream(ctx, chatReq, func(chunk providers.StreamChunk) {
				if chunk.Thinking != "" {
					emitRun(AgentEvent{
						Type:    protocol.ChatEventThinking,
						AgentID: l.id,
						RunID:   req.RunID,
						Payload: map[string]string{"content": chunk.Thinking},
					})
				}
				if chunk.Content != "" {
					emitRun(AgentEvent{
						Type:    protocol.ChatEventChunk,
						AgentID: l.id,
						RunID:   req.RunID,
						Payload: map[string]string{"content": chunk.Content},
					})
				}
			})
		} else {
			resp, err = l.provider.Chat(ctx, chatReq)
		}

		if err != nil {
			l.emitLLMSpanEnd(ctx, llmSpanID, llmSpanStart, nil, err)
			return nil, fmt.Errorf("LLM call failed (iteration %d): %w", iteration, err)
		}

		l.emitLLMSpanEnd(ctx, llmSpanID, llmSpanStart, resp, nil)

		// For non-streaming responses, emit thinking and content as single events
		if !req.Stream {
			if resp.Thinking != "" {
				emitRun(AgentEvent{
					Type:    protocol.ChatEventThinking,
					AgentID: l.id,
					RunID:   req.RunID,
					Payload: map[string]string{"content": resp.Thinking},
				})
			}
			if resp.Content != "" {
				emitRun(AgentEvent{
					Type:    protocol.ChatEventChunk,
					AgentID: l.id,
					RunID:   req.RunID,
					Payload: map[string]string{"content": resp.Content},
				})
			}
		}

		if resp.Usage != nil {
			totalUsage.PromptTokens += resp.Usage.PromptTokens
			totalUsage.CompletionTokens += resp.Usage.CompletionTokens
			totalUsage.TotalTokens += resp.Usage.TotalTokens
			totalUsage.ThinkingTokens += resp.Usage.ThinkingTokens
		}

		// Mid-loop compaction: same threshold as maybeSummarize (contextWindow * historyShare)
		// but applied to in-memory messages during the run. Prevents context overflow for
		// long-running agents (e.g. delegated research tasks that accumulate many tool results).
		if !midLoopCompacted && l.contextWindow > 0 {
			historyShare := config.DefaultHistoryShare
			if l.compactionCfg != nil && l.compactionCfg.MaxHistoryShare > 0 {
				historyShare = l.compactionCfg.MaxHistoryShare
			}
			threshold := int(float64(l.contextWindow) * historyShare)

			promptTokens := 0
			if resp.Usage != nil && resp.Usage.PromptTokens > 0 {
				promptTokens = resp.Usage.PromptTokens
			} else {
				promptTokens = EstimateTokens(messages)
			}

			if promptTokens >= threshold {
				midLoopCompacted = true
				emitRun(AgentEvent{
					Type:    protocol.AgentEventActivity,
					AgentID: l.id,
					RunID:   req.RunID,
					Payload: map[string]any{"phase": "compacting", "iteration": iteration},
				})
				if compacted := l.compactMessagesInPlace(ctx, messages); compacted != nil {
					messages = compacted
				}
				slog.Info("mid_loop_compaction",
					"agent", l.id,
					"prompt_tokens", promptTokens,
					"threshold", threshold,
					"context_window", l.contextWindow)
			}
		}

		// Output truncated (max_tokens hit). Tool call args are likely incomplete.
		// Inject a system hint so the model can retry with shorter output.
		if resp.FinishReason == "length" && len(resp.ToolCalls) > 0 {
			slog.Warn("output truncated (max_tokens), tool calls may have incomplete args",
				"agent", l.id, "iteration", iteration, "max_tokens", l.effectiveMaxTokens())
			messages = append(messages,
				providers.Message{Role: "assistant", Content: resp.Content},
				providers.Message{
					Role:    "user",
					Content: "[System] Your output was truncated because it exceeded max_tokens. Your tool call arguments were incomplete. Please retry with shorter content — split large writes into multiple smaller calls, or reduce the amount of text.",
				},
			)
			continue
		}

		// No tool calls → done
		if len(resp.ToolCalls) == 0 {
			// Mid-run injection (Point B): drain all buffered user follow-up messages
			// before exiting. If found, save current assistant response and continue
			// the loop so the LLM can respond to the injected messages.
			if forLLM, forSession := l.drainInjectChannel(req.InjectCh, emitRun); len(forLLM) > 0 {
				messages = append(messages, providers.Message{Role: "assistant", Content: resp.Content})
				messages = append(messages, forLLM...)
				pendingMsgs = append(pendingMsgs, providers.Message{Role: "assistant", Content: resp.Content})
				pendingMsgs = append(pendingMsgs, forSession...)
				continue
			}

			finalContent = resp.Content
			finalThinking = resp.Thinking
			break
		}

		// Ensure globally unique tool call IDs (OpenAI-compatible APIs return 400 on duplicates).
		// Skip if raw content is present (Anthropic thinking passback) to avoid desync.
		if resp.RawAssistantContent == nil {
			resp.ToolCalls = uniquifyToolCallIDs(resp.ToolCalls, req.RunID, iteration)
		}

		// Build assistant message with tool calls
		assistantMsg := providers.Message{
			Role:                "assistant",
			Content:             resp.Content,
			Thinking:            resp.Thinking, // reasoning_content passback for thinking models (Kimi, DeepSeek)
			ToolCalls:           resp.ToolCalls,
			Phase:               resp.Phase,               // preserve Codex phase metadata (gpt-5.3-codex)
			RawAssistantContent: resp.RawAssistantContent, // preserve thinking blocks for Anthropic passback
		}
		messages = append(messages, assistantMsg)
		pendingMsgs = append(pendingMsgs, assistantMsg)

		// Emit block.reply for intermediate assistant content during tool iterations.
		// Non-streaming channels (Zalo, Discord, WhatsApp) would otherwise lose this text.
		if resp.Content != "" {
			sanitized := SanitizeAssistantContent(resp.Content)
			if sanitized != "" && !IsSilentReply(sanitized) {
				blockReplies++
				lastBlockReply = sanitized
				l.emit(AgentEvent{
					Type:    protocol.AgentEventBlockReply,
					AgentID: l.id,
					RunID:   req.RunID,
					Payload: map[string]string{"content": sanitized},
				})
			}
		}

		// Track team_tasks create for orphan detection (argument-based, pre-execution).
		// Spawn counting is done post-execution so failed spawns don't get counted.
		for _, tc := range resp.ToolCalls {
			if l.resolveToolCallName(tc.Name) == "team_tasks" {
				if action, _ := tc.Arguments["action"].(string); action == "create" {
					teamTaskCreates++
				}
			}
		}

		// Tool budget check: soft stop when total tool calls exceed the per-agent limit.
		// Same pattern as maxIterations — no error thrown, LLM summarizes and returns.
		totalToolCalls += len(resp.ToolCalls)
		if l.maxToolCalls > 0 && totalToolCalls > l.maxToolCalls {
			slog.Warn("security.tool_budget_exceeded",
				"agent", l.id, "total", totalToolCalls, "limit", l.maxToolCalls)
			messages = append(messages, providers.Message{
				Role:    "user",
				Content: fmt.Sprintf("[System] Tool call budget reached (%d/%d). Do NOT call any more tools. Summarize results so far and respond to the user.", totalToolCalls, l.maxToolCalls),
			})
			continue // one more LLM call for summarization, then loop exits (no tool calls)
		}

		// Emit activity event: tool execution phase
		if len(resp.ToolCalls) > 0 {
			toolNames := make([]string, len(resp.ToolCalls))
			for i, tc := range resp.ToolCalls {
				toolNames[i] = tc.Name
			}
			emitRun(AgentEvent{
				Type:    protocol.AgentEventActivity,
				AgentID: l.id,
				RunID:   req.RunID,
				Payload: map[string]any{
					"phase":     "tool_exec",
					"tool":      toolNames[0],
					"tools":     toolNames,
					"iteration": iteration,
				},
			})
		}

		// Execute tool calls (parallel when multiple, sequential when single)
		if len(resp.ToolCalls) == 1 {
			// Single tool: sequential — no goroutine overhead
			tc := resp.ToolCalls[0]
			emitRun(AgentEvent{
				Type:    protocol.AgentEventToolCall,
				AgentID: l.id,
				RunID:   req.RunID,
				Payload: map[string]any{"name": tc.Name, "id": tc.ID, "arguments": truncateToolArgs(tc.Arguments, 500)},
			})

			argsJSON, _ := json.Marshal(tc.Arguments)
			slog.Info("tool call", "agent", l.id, "tool", tc.Name, "args_len", len(argsJSON))

			registryName := l.resolveToolCallName(tc.Name)
			argsHash := loopDetector.record(registryName, tc.Arguments)

			toolSpanStart := time.Now().UTC()
			toolSpanID := l.emitToolSpanStart(ctx, toolSpanStart, tc.Name, tc.ID, string(argsJSON))

			stopSlowTimer := toolTiming.StartSlowTimer(tc.Name, l.id, req.RunID, slowToolEnabled, emitRun)
			var result *tools.Result
			if allowedTools != nil && !allowedTools[registryName] {
				// Attempt lazy activation: deferred MCP tools can be activated on first call
				// so the LLM can call them by name directly without mcp_tool_search.
				if l.tools.TryActivateDeferred(registryName) {
					// Verify tool isn't explicitly denied by policy before allowing.
					if l.toolPolicy != nil && l.toolPolicy.IsDenied(registryName, l.agentToolPolicy) {
						slog.Warn("security.tool_policy_denied_lazy", "agent", l.id, "tool", tc.Name, "resolved", registryName)
						result = tools.ErrorResult("tool not allowed by policy: " + tc.Name)
					} else {
						allowedTools[registryName] = true
						slog.Info("mcp.tool.lazy_activated", "agent", l.id, "tool", tc.Name, "resolved", registryName)
					}
				} else {
					slog.Warn("security.tool_policy_blocked", "agent", l.id, "tool", tc.Name, "resolved", registryName)
					result = tools.ErrorResult("tool not allowed by policy: " + tc.Name)
				}
			}
			if result == nil {
				result = l.tools.ExecuteWithContext(iterCtx, registryName, tc.Arguments, req.Channel, req.ChatID, req.PeerKind, req.SessionKey, nil)
			}
			stopSlowTimer()

			l.emitToolSpanEnd(ctx, toolSpanID, toolSpanStart, result)

			// Record tool execution time for adaptive thresholds.
			toolTiming.Record(tc.Name, time.Since(toolSpanStart).Milliseconds())

			// Record result for loop detection.
			loopDetector.recordResult(argsHash, result.ForLLM)
			loopDetector.recordMutation(registryName)

			if result.Async {
				asyncToolCalls = append(asyncToolCalls, tc.Name)
			}

			if result.IsError {
				errMsg := result.ForLLM
				if len(errMsg) > 200 {
					errMsg = errMsg[:200] + "..."
				}
				slog.Warn("tool error", "agent", l.id, "tool", tc.Name, "error", errMsg)
			}

			// Count successful spawn calls for orphan detection (post-execution).
			if registryName == "spawn" && !result.IsError {
				if tid, _ := tc.Arguments["team_task_id"].(string); tid != "" {
					teamTaskSpawns++
				}
			}
			if hadBootstrap && bootstrapToolAllowlist[registryName] {
				bootstrapWriteDetected = true
			}

			toolResultPayload := map[string]any{
				"name":      tc.Name,
				"id":        tc.ID,
				"is_error":  result.IsError,
				"arguments": tc.Arguments,
				"result":    truncateStr(result.ForLLM, 1000),
			}
			if result.IsError && result.ForLLM != "" {
				toolResultPayload["content"] = result.ForLLM
			}
			emitRun(AgentEvent{
				Type:    protocol.AgentEventToolResult,
				AgentID: l.id,
				RunID:   req.RunID,
				Payload: toolResultPayload,
			})

			l.scanWebToolResult(tc.Name, result)

			// Collect MEDIA: paths from tool results.
			// Prefer result.Media (explicit) over ForLLM MEDIA: prefix (legacy) to avoid duplicates.
			if len(result.Media) > 0 {
				for _, mf := range result.Media {
					ct := mf.MimeType
					if ct == "" {
						ct = mimeFromExt(filepath.Ext(mf.Path))
					}
					mediaResults = append(mediaResults, MediaResult{Path: mf.Path, ContentType: ct})
				}
			} else if mr := parseMediaResult(result.ForLLM); mr != nil {
				mediaResults = append(mediaResults, *mr)
			}
			if result.Deliverable != "" {
				deliverables = append(deliverables, result.Deliverable)
			}

			toolMsg := providers.Message{
				Role:       "tool",
				Content:    result.ForLLM,
				ToolCallID: tc.ID,
				IsError:    result.IsError,
			}
			messages = append(messages, toolMsg)
			pendingMsgs = append(pendingMsgs, toolMsg)

			// Check for tool call loop after recording result.
			if level, msg := loopDetector.detect(registryName, argsHash); level != "" {
				if level == "critical" {
					slog.Warn("tool loop critical", "agent", l.id, "tool", registryName, "message", msg)
					finalContent = "I was unable to complete this task — I got stuck repeatedly calling " + registryName + " without making progress. Please try rephrasing your request."
					break
				}
				// Warning: inject message so model knows to change strategy.
				slog.Warn("tool loop warning", "agent", l.id, "tool", registryName, "message", msg)
				messages = append(messages, providers.Message{Role: "user", Content: msg})
			}

			// Check for read-only streak (no write/edit actions).
			if level, msg := loopDetector.detectReadOnlyStreak(); level != "" {
				if level == "critical" {
					slog.Warn("tool loop critical: read-only streak",
						"streak", loopDetector.readOnlyStreak, "agent", l.id, "run", req.RunID)
					finalContent = msg
					break
				}
				slog.Warn("tool loop warning: read-only streak",
					"streak", loopDetector.readOnlyStreak, "agent", l.id, "run", req.RunID)
				messages = append(messages, providers.Message{Role: "user", Content: msg})
			}

			// Check for same tool returning identical results with different args.
			if rh := hashResult(result.ForLLM); rh != "" {
				if level, msg := loopDetector.detectSameResult(registryName, rh); level != "" {
					if level == "critical" {
						slog.Warn("tool loop critical: same result",
							"tool", registryName, "agent", l.id, "run", req.RunID)
						finalContent = msg
						break
					}
					messages = append(messages, providers.Message{Role: "user", Content: msg})
				}
			}
		} else {
			// Multiple tools: parallel execution via goroutines.
			// Tool instances are immutable (context-based) so concurrent access is safe.
			// Results are collected then processed sequentially for deterministic ordering.
			type indexedResult struct {
				idx          int
				tc           providers.ToolCall
				registryName string
				result       *tools.Result
				argsJSON     string
				spanStart    time.Time
			}

			// 1. Emit all tool.call events upfront (client sees all calls starting)
			for _, tc := range resp.ToolCalls {
				emitRun(AgentEvent{
					Type:    protocol.AgentEventToolCall,
					AgentID: l.id,
					RunID:   req.RunID,
					Payload: map[string]any{"name": tc.Name, "id": tc.ID, "arguments": truncateToolArgs(tc.Arguments, 500)},
				})
			}

			// 2. Execute all tools in parallel
			resultCh := make(chan indexedResult, len(resp.ToolCalls))
			var wg sync.WaitGroup

			for i, tc := range resp.ToolCalls {
				wg.Add(1)
				go func(idx int, tc providers.ToolCall) {
					defer wg.Done()
					argsJSON, _ := json.Marshal(tc.Arguments)
					slog.Info("tool call", "agent", l.id, "tool", tc.Name, "args_len", len(argsJSON), "parallel", true)
					spanStart := time.Now().UTC()
					registryName := l.resolveToolCallName(tc.Name)
					// Emit running span inside goroutine — goroutine-safe (channel send only).
					// End is also emitted here to prevent orphans on ctx cancellation.
					spanID := l.emitToolSpanStart(ctx, spanStart, tc.Name, tc.ID, string(argsJSON))

					stopSlowTimer := toolTiming.StartSlowTimer(tc.Name, l.id, req.RunID, slowToolEnabled, emitRun)
					var result *tools.Result
					if allowedTools != nil && !allowedTools[registryName] {
						// Attempt lazy activation for deferred MCP tools.
						// Note: don't write back to allowedTools — concurrent goroutines share
						// the map and writes would race. TryActivateDeferred is idempotent.
						if l.tools.TryActivateDeferred(registryName) {
							// Verify tool isn't explicitly denied by policy before allowing.
							if l.toolPolicy != nil && l.toolPolicy.IsDenied(registryName, l.agentToolPolicy) {
								slog.Warn("security.tool_policy_denied_lazy", "agent", l.id, "tool", tc.Name, "resolved", registryName)
								result = tools.ErrorResult("tool not allowed by policy: " + tc.Name)
							} else {
								slog.Info("mcp.tool.lazy_activated", "agent", l.id, "tool", tc.Name, "resolved", registryName)
							}
						} else {
							slog.Warn("security.tool_policy_blocked", "agent", l.id, "tool", tc.Name, "resolved", registryName)
							result = tools.ErrorResult("tool not allowed by policy: " + tc.Name)
						}
					}
					if result == nil {
						result = l.tools.ExecuteWithContext(iterCtx, registryName, tc.Arguments, req.Channel, req.ChatID, req.PeerKind, req.SessionKey, nil)
					}
					stopSlowTimer()
					l.emitToolSpanEnd(ctx, spanID, spanStart, result)
					resultCh <- indexedResult{idx: idx, tc: tc, registryName: registryName, result: result, argsJSON: string(argsJSON), spanStart: spanStart}
				}(i, tc)
			}

			// Close channel after all goroutines complete (run in separate goroutine to avoid deadlock)
			go func() { wg.Wait(); close(resultCh) }()

			// 3. Collect results
			collected := make([]indexedResult, 0, len(resp.ToolCalls))
			for r := range resultCh {
				collected = append(collected, r)
			}

			// 4. Sort by original index → deterministic message ordering
			sort.Slice(collected, func(i, j int) bool {
				return collected[i].idx < collected[j].idx
			})

			// 5. Process results sequentially: emit events, append messages, save to session
			// Note: tool span start/end already emitted inside goroutines above.
			var loopStuck bool
			for _, r := range collected {
				// Record tool execution time for adaptive thresholds.
				toolTiming.Record(r.tc.Name, time.Since(r.spanStart).Milliseconds())

				// Record for loop detection.
				argsHash := loopDetector.record(r.registryName, r.tc.Arguments)
				loopDetector.recordResult(argsHash, r.result.ForLLM)
				loopDetector.recordMutation(r.registryName)

				if r.result.Async {
					asyncToolCalls = append(asyncToolCalls, r.tc.Name)
				}

				if r.result.IsError {
					errMsg := r.result.ForLLM
					if len(errMsg) > 200 {
						errMsg = errMsg[:200] + "..."
					}
					slog.Warn("tool error", "agent", l.id, "tool", r.tc.Name, "error", errMsg)
				}

				// Count successful spawn calls for orphan detection (post-execution).
				if r.registryName == "spawn" && !r.result.IsError {
					if tid, _ := r.tc.Arguments["team_task_id"].(string); tid != "" {
						teamTaskSpawns++
					}
				}
				if hadBootstrap && bootstrapToolAllowlist[r.registryName] {
					bootstrapWriteDetected = true
				}

				parToolResultPayload := map[string]any{
					"name":      r.tc.Name,
					"id":        r.tc.ID,
					"is_error":  r.result.IsError,
					"arguments": r.tc.Arguments,
					"result":    truncateStr(r.result.ForLLM, 1000),
				}
				if r.result.IsError && r.result.ForLLM != "" {
					parToolResultPayload["content"] = r.result.ForLLM
				}
				emitRun(AgentEvent{
					Type:    protocol.AgentEventToolResult,
					AgentID: l.id,
					RunID:   req.RunID,
					Payload: parToolResultPayload,
				})

				l.scanWebToolResult(r.tc.Name, r.result)

				// Collect MEDIA: paths from tool results.
				// Prefer result.Media (explicit) over ForLLM MEDIA: prefix (legacy) to avoid duplicates.
				if len(r.result.Media) > 0 {
					for _, mf := range r.result.Media {
						ct := mf.MimeType
						if ct == "" {
							ct = mimeFromExt(filepath.Ext(mf.Path))
						}
						mediaResults = append(mediaResults, MediaResult{Path: mf.Path, ContentType: ct})
					}
				} else if mr := parseMediaResult(r.result.ForLLM); mr != nil {
					mediaResults = append(mediaResults, *mr)
				}
				if r.result.Deliverable != "" {
					deliverables = append(deliverables, r.result.Deliverable)
				}

				toolMsg := providers.Message{
					Role:       "tool",
					Content:    r.result.ForLLM,
					ToolCallID: r.tc.ID,
					IsError:    r.result.IsError,
				}
				messages = append(messages, toolMsg)
				pendingMsgs = append(pendingMsgs, toolMsg)

				// Check for tool call loop.
				if level, msg := loopDetector.detect(r.registryName, argsHash); level != "" {
					if level == "critical" {
						slog.Warn("tool loop critical", "agent", l.id, "tool", r.registryName, "message", msg)
						finalContent = "I was unable to complete this task — I got stuck repeatedly calling " + r.registryName + " without making progress. Please try rephrasing your request."
						loopStuck = true
						break
					}
					slog.Warn("tool loop warning", "agent", l.id, "tool", r.registryName, "message", msg)
					messages = append(messages, providers.Message{Role: "user", Content: msg})
				}

				// Check for same tool returning identical results with different args.
				if rh := hashResult(r.result.ForLLM); rh != "" {
					if level, msg := loopDetector.detectSameResult(r.registryName, rh); level != "" {
						if level == "critical" {
							slog.Warn("tool loop critical: same result",
								"tool", r.registryName, "agent", l.id, "run", req.RunID)
							finalContent = msg
							loopStuck = true
							break
						}
						messages = append(messages, providers.Message{Role: "user", Content: msg})
					}
				}
			}

			// Check read-only streak after processing all parallel results.
			if !loopStuck {
				if level, msg := loopDetector.detectReadOnlyStreak(); level != "" {
					if level == "critical" {
						slog.Warn("tool loop critical: read-only streak",
							"streak", loopDetector.readOnlyStreak, "agent", l.id, "run", req.RunID)
						finalContent = msg
						loopStuck = true
					} else {
						slog.Warn("tool loop warning: read-only streak",
							"streak", loopDetector.readOnlyStreak, "agent", l.id, "run", req.RunID)
						messages = append(messages, providers.Message{Role: "user", Content: msg})
					}
				}
			}

			if loopStuck {
				break
			}
		}

		// Mid-run injection (Point A): drain any user follow-up messages
		// that arrived during tool execution. Append them after tool results
		// so the next LLM call sees: [tool results...] + [user follow-ups...].
		if forLLM, forSession := l.drainInjectChannel(req.InjectCh, emitRun); len(forLLM) > 0 {
			messages = append(messages, forLLM...)
			pendingMsgs = append(pendingMsgs, forSession...)
		}

	}

	// 4. Full sanitization pipeline (matching TS extractAssistantText + sanitizeUserFacingText)
	finalContent = SanitizeAssistantContent(finalContent)

	// 4b. Config leak detection — disabled: too many false positives
	// (e.g. agent explaining public architecture mentioning SOUL.md etc.)
	// finalContent = StripConfigLeak(finalContent, l.agentType)

	// 5. Handle NO_REPLY: save to session for context but mark as silent.
	// Matching TS: NO_REPLY is saved (via resolveSilentReplyFallbackText) but
	// filtered at the payload level before delivery.
	isSilent := IsSilentReply(finalContent)

	// 5b. Skill evolution: postscript suggestion after complex tasks.
	// Fires when skill_evolve=true AND the run involved enough tool calls to warrant a skill.
	// Appended to the agent's own final response so the user sees it inline and can explicitly
	// consent ("save as skill") before anything is created. No mid-loop injection, no async
	// goroutine, no session contamination — the next user turn naturally triggers skill creation.
	if l.skillEvolve && l.skillNudgeInterval > 0 &&
		totalToolCalls >= l.skillNudgeInterval &&
		finalContent != "" && !isSilent && !skillPostscriptSent {
		skillPostscriptSent = true
		locale := store.LocaleFromContext(ctx)
		finalContent += "\n\n---\n_" + i18n.T(locale, i18n.MsgSkillNudgePostscript) + "_"
	}

	// 6. Fallback for empty content
	if finalContent == "" {
		if len(asyncToolCalls) > 0 {
			finalContent = "..."
		} else {
			finalContent = "..."
		}
	}

	// Append content suffix (e.g. image markdown for WS) before saving to session.
	if req.ContentSuffix != "" && !strings.Contains(finalContent, req.ContentSuffix) {
		finalContent += req.ContentSuffix
	}

	// Collect forwarded media + dedup + populate sizes BEFORE saving to session,
	// so we can attach output MediaRefs to the assistant message for history reload.
	for _, mf := range req.ForwardMedia {
		ct := mf.MimeType
		if ct == "" {
			ct = mimeFromExt(filepath.Ext(mf.Path))
		}
		mediaResults = append(mediaResults, MediaResult{Path: mf.Path, ContentType: ct})
	}
	mediaResults = deduplicateMedia(mediaResults)
	for i := range mediaResults {
		if mediaResults[i].Size == 0 {
			if info, err := os.Stat(mediaResults[i].Path); err == nil {
				mediaResults[i].Size = info.Size()
			}
		}
	}

	// Build final assistant message with output media refs for history persistence.
	assistantMsg := providers.Message{
		Role:     "assistant",
		Content:  finalContent,
		Thinking: finalThinking,
	}
	for _, mr := range mediaResults {
		kind := "document"
		if strings.HasPrefix(mr.ContentType, "image/") {
			kind = "image"
		} else if strings.HasPrefix(mr.ContentType, "audio/") {
			kind = "audio"
		} else if strings.HasPrefix(mr.ContentType, "video/") {
			kind = "video"
		}
		assistantMsg.MediaRefs = append(assistantMsg.MediaRefs, providers.MediaRef{
			ID:       filepath.Base(mr.Path),
			MimeType: mr.ContentType,
			Kind:     kind,
		})
	}
	pendingMsgs = append(pendingMsgs, assistantMsg)

	// Bootstrap nudge: if model didn't call write_file on turn 2+, inject reminder
	// into session history so the next turn sees it. Appended to pendingMsgs so it's
	// flushed in the single Save below (avoids double I/O).
	// Note: the nudge counts as a "user" turn in history, which accelerates auto-cleanup
	// by one turn — acceptable since bootstrap should complete in 2-3 turns anyway.
	if hadBootstrap && l.bootstrapCleanup != nil {
		nudgeUserTurns := 1
		for _, m := range history {
			if m.Role == "user" {
				nudgeUserTurns++
			}
		}
		if !bootstrapWriteDetected && nudgeUserTurns >= 2 && nudgeUserTurns < bootstrapAutoCleanupTurns {
			pendingMsgs = append(pendingMsgs, providers.Message{
				Role:    "user",
				Content: "[System] You haven't completed onboarding yet. Please update USER.md with the user's details and clear BOOTSTRAP.md as instructed.",
			})
		}
	}

	// Flush all buffered messages to session atomically.
	// This ensures concurrent runs never see each other's in-progress messages.
	for _, msg := range pendingMsgs {
		l.sessions.AddMessage(req.SessionKey, msg)
	}

	// Persist adaptive tool timing to session metadata.
	if serialized := toolTiming.Serialize(); serialized != "" {
		l.sessions.SetSessionMetadata(req.SessionKey, map[string]string{"tool_timing": serialized})
	}

	// Write session metadata (matching TS session entry updates)
	l.sessions.UpdateMetadata(req.SessionKey, l.model, l.provider.Name(), req.Channel)
	l.sessions.AccumulateTokens(req.SessionKey, int64(totalUsage.PromptTokens), int64(totalUsage.CompletionTokens))

	// Calibrate token estimation: store actual prompt tokens + message count.
	// Next time EstimateTokensWithCalibration() is called, it uses this as a base
	// instead of the chars/3 heuristic (more accurate for multilingual content).
	if totalUsage.PromptTokens > 0 {
		msgCount := len(history) + len(pendingMsgs)
		l.sessions.SetLastPromptTokens(req.SessionKey, totalUsage.PromptTokens, msgCount)
	}

	l.sessions.Save(req.SessionKey)

	// Bootstrap auto-cleanup: after enough conversation turns, remove BOOTSTRAP.md
	// as a safety net in case the LLM didn't clear it itself.
	// Bootstrap typically completes in 2-3 turns; we auto-cleanup after 3 user messages.
	// Uses pre-run history (already loaded) + 1 for current message — no extra DB call.
	if hadBootstrap && l.bootstrapCleanup != nil {
		userTurns := 1 // current user message
		for _, m := range history {
			if m.Role == "user" {
				userTurns++
			}
		}
		if userTurns >= bootstrapAutoCleanupTurns {
			if cleanErr := l.bootstrapCleanup(ctx, l.agentUUID, req.UserID); cleanErr != nil {
				slog.Warn("bootstrap auto-cleanup failed", "error", cleanErr, "agent", l.id, "user", req.UserID)
			} else {
				slog.Info("bootstrap auto-cleanup completed", "agent", l.id, "user", req.UserID, "turns", userTurns)
			}
		}
	}

	// If silent, return empty content so gateway suppresses delivery.
	if isSilent {
		slog.Info("agent loop: NO_REPLY detected, suppressing delivery",
			"agent", l.id, "session", req.SessionKey)
		finalContent = ""
	}

	// 5. Maybe summarize
	l.maybeSummarize(ctx, req.SessionKey)

	return &RunResult{
		Content:        finalContent,
		RunID:          req.RunID,
		Iterations:     iteration,
		Usage:          &totalUsage,
		Media:          mediaResults,
		Deliverables:   deliverables,
		BlockReplies:   blockReplies,
		LastBlockReply: lastBlockReply,
	}, nil
}

// resolveToolCallName strips the configured tool call prefix from a name
// returned by the model, returning the original registry name.
// Example: prefix "proxy_" + model calls "proxy_exec" → returns "exec".
func (l *Loop) resolveToolCallName(name string) string {
	if l.agentToolPolicy != nil && l.agentToolPolicy.ToolCallPrefix != "" {
		return tools.StripToolPrefix(l.agentToolPolicy.ToolCallPrefix, name)
	}
	return name
}

func truncateToolArgs(args map[string]any, maxLen int) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		if s, ok := v.(string); ok && len(s) > maxLen {
			out[k] = truncateStr(s, maxLen)
		} else {
			out[k] = v
		}
	}
	return out
}
