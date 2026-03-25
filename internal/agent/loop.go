package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/safego"
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
		event.SessionKey = req.SessionKey
		event.TenantID = l.tenantID
		l.emit(event)
	}

	// Inject context: agent/tenant/user/workspace scoping, input guard, message truncation.
	ctxSetup, err := l.injectContext(ctx, &req)
	if err != nil {
		return nil, err
	}
	ctx = ctxSetup.ctx
	resolvedTeamSettings := ctxSetup.resolvedTeamSettings

	// 0. Cache agent's context window on the session (first run only).
	// Enables scheduler's adaptive throttle to use the real value instead of hardcoded 200K.
	if l.sessions.GetContextWindow(ctx, req.SessionKey) <= 0 {
		l.sessions.SetContextWindow(ctx, req.SessionKey, l.contextWindow)
	}

	// 0b. Load adaptive tool timing from session metadata.
	toolTiming := ParseToolTiming(l.sessions.GetSessionMetadata(ctx, req.SessionKey))

	// Resolve slow_tool notification config from already-loaded team settings (no extra DB query).
	slowToolEnabled := tools.ParseTeamNotifyConfig(resolvedTeamSettings).SlowTool

	// 1. Build messages from session history
	history := l.sessions.GetHistory(ctx, req.SessionKey)
	summary := l.sessions.GetSummary(ctx, req.SessionKey)

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
		mediaRefs = l.persistMedia(req.SessionKey, req.Media, tools.ToolWorkspaceFromCtx(ctx))
		// Load current-turn images from persisted refs (Path is always set for new uploads).
		var imageFiles []bus.MediaFile
		for _, ref := range mediaRefs {
			if ref.Kind == "image" && ref.Path != "" {
				imageFiles = append(imageFiles, bus.MediaFile{Path: ref.Path, MimeType: ref.MimeType})
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

	// 2a. Load historical images into context for read_image tool.
	// Without this, read_image can only see current-turn images, not previous turns.
	// Both inline and tool-deferred modes need this — inline mode attaches images to
	// messages for the main LLM, but the read_image tool also needs context access
	// to analyze historical images on demand.
	if l.mediaStore != nil {
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
			// Prefer workspace-local path (.uploads/) over canonical .media/ path.
			if ref.Path != "" {
				mediaPaths = append(mediaPaths, ref.Path)
			} else if p, err := l.mediaStore.LoadPath(ref.ID); err == nil {
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
	// Initialized here before rs is created; moved to rs after construction.
	var initPendingMsgs []providers.Message
	if !req.HideInput {
		initPendingMsgs = append(initPendingMsgs, providers.Message{
			Role:      "user",
			Content:   req.Message,
			MediaRefs: mediaRefs,
		})
	}

	// 4. Run LLM iteration loop — all mutable state encapsulated in runState.
	rs := &runState{
		pendingMsgs: initPendingMsgs,
	}

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

	for rs.iteration < maxIter {
		rs.iteration++

		slog.Debug("agent iteration", "agent", l.id, "iteration", rs.iteration, "messages", len(messages))

		// Skill evolution: budget pressure nudges at 70% and 90% of iteration budget.
		// Ephemeral (in-memory only, not persisted to session) — LLM sees them during this run only.
		if l.skillEvolve && maxIter > 0 {
			locale := store.LocaleFromContext(ctx)
			iterPct := float64(rs.iteration) / float64(maxIter)
			if iterPct >= 0.90 && !rs.skillNudge90Sent {
				rs.skillNudge90Sent = true
				messages = append(messages, providers.Message{
					Role:    "user",
					Content: i18n.T(locale, i18n.MsgSkillNudge90Pct),
				})
			} else if iterPct >= 0.70 && !rs.skillNudge70Sent {
				rs.skillNudge70Sent = true
				messages = append(messages, providers.Message{
					Role:    "user",
					Content: i18n.T(locale, i18n.MsgSkillNudge70Pct),
				})
			}
		}

		// Member progress nudge: remind to report progress every 6 iterations.
		// Suggests percent based on iteration ratio — model can adjust but has a baseline.
		if req.TeamTaskID != "" && memberTaskSubject != "" && rs.iteration > 0 && rs.iteration%6 == 0 {
			var nudge string
			if maxIter > 0 {
				suggestedPct := rs.iteration * 100 / maxIter
				nudge = fmt.Sprintf(
					"[System] You are at iteration %d/%d (~%d%% of budget) working on task #%d: %q. "+
						"Report your progress now: team_tasks(action=\"progress\", percent=%d, text=\"what you've accomplished so far\"). "+
						"Adjust percent based on actual work completed.",
					rs.iteration, maxIter, suggestedPct, memberTaskNumber, memberTaskSubject, suggestedPct)
			} else {
				nudge = fmt.Sprintf(
					"[System] You are at iteration %d working on task #%d: %q. "+
						"Report your progress now: team_tasks(action=\"progress\", percent=50, text=\"what you've accomplished so far\"). "+
						"Adjust percent based on actual work completed.",
					rs.iteration, memberTaskNumber, memberTaskSubject)
			}
			messages = append(messages, providers.Message{Role: "user", Content: nudge})
		}

		// Iteration budget nudge: when model has used 75% of iterations without
		// producing any text response, warn it to start summarizing.
		if maxIter > 0 && rs.iteration > 1 && rs.iteration == maxIter*3/4 && rs.finalContent == "" {
			messages = append(messages, providers.Message{
				Role:    "user",
				Content: "[System] You have used 75% of your iteration budget without providing a text response. Start summarizing your findings and respond to the user within the next few iterations.",
			})
		}

		// Inject iteration progress into context so tools can adapt (e.g. web_fetch reduces maxChars).
		iterCtx := tools.WithIterationProgress(ctx, tools.IterationProgress{
			Current: rs.iteration,
			Max:     maxIter,
		})

		// Emit activity event: thinking phase
		emitRun(AgentEvent{
			Type:    protocol.AgentEventActivity,
			AgentID: l.id,
			RunID:   req.RunID,
			Payload: map[string]any{"phase": "thinking", "iteration": rs.iteration},
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

		// Per-tenant tool exclusions: remove tools disabled for this agent's tenant.
		if len(l.disabledTools) > 0 {
			filtered := toolDefs[:0]
			for _, td := range toolDefs {
				if !l.disabledTools[td.Function.Name] {
					filtered = append(filtered, td)
				} else {
					delete(allowedTools, td.Function.Name)
				}
			}
			toolDefs = filtered
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
		if rs.iteration == maxIter {
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
				providers.OptWorkspace:   tools.ToolWorkspaceFromCtx(ctx),
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

		callCtx := providers.WithChatGPTOAuthRoutingObservation(ctx, providers.NewChatGPTOAuthRoutingObservation())
		llmSpanStart := time.Now().UTC()
		llmSpanID := l.emitLLMSpanStart(callCtx, llmSpanStart, rs.iteration, messages)

		if req.Stream {
			resp, err = l.provider.ChatStream(callCtx, chatReq, func(chunk providers.StreamChunk) {
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
			resp, err = l.provider.Chat(callCtx, chatReq)
		}

		if err != nil {
			l.emitLLMSpanEnd(callCtx, llmSpanID, llmSpanStart, nil, err)
			return nil, fmt.Errorf("LLM call failed (iteration %d): %w", rs.iteration, err)
		}

		l.emitLLMSpanEnd(callCtx, llmSpanID, llmSpanStart, resp, nil)

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
			rs.totalUsage.PromptTokens += resp.Usage.PromptTokens
			rs.totalUsage.CompletionTokens += resp.Usage.CompletionTokens
			rs.totalUsage.TotalTokens += resp.Usage.TotalTokens
			rs.totalUsage.ThinkingTokens += resp.Usage.ThinkingTokens
		}

		// Mid-loop compaction: same threshold as maybeSummarize (contextWindow * historyShare)
		// but applied to in-memory messages during the run. Prevents context overflow for
		// long-running agents (e.g. delegated research tasks that accumulate many tool results).
		if !rs.midLoopCompacted && l.contextWindow > 0 {
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
				rs.midLoopCompacted = true
				emitRun(AgentEvent{
					Type:    protocol.AgentEventActivity,
					AgentID: l.id,
					RunID:   req.RunID,
					Payload: map[string]any{"phase": "compacting", "iteration": rs.iteration},
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
				"agent", l.id, "iteration", rs.iteration, "max_tokens", l.effectiveMaxTokens())
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
				rs.pendingMsgs = append(rs.pendingMsgs, providers.Message{Role: "assistant", Content: resp.Content})
				rs.pendingMsgs = append(rs.pendingMsgs, forSession...)
				continue
			}

			rs.finalContent = resp.Content
			rs.finalThinking = resp.Thinking
			break
		}

		// Ensure globally unique tool call IDs (OpenAI-compatible APIs return 400 on duplicates).
		// Skip if raw content is present (Anthropic thinking passback) to avoid desync.
		if resp.RawAssistantContent == nil {
			resp.ToolCalls = uniquifyToolCallIDs(resp.ToolCalls, req.RunID, rs.iteration)
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
		rs.pendingMsgs = append(rs.pendingMsgs, assistantMsg)

		// Emit block.reply for intermediate assistant content during tool iterations.
		// Non-streaming channels (Zalo, Discord, WhatsApp) would otherwise lose this text.
		if resp.Content != "" {
			sanitized := SanitizeAssistantContent(resp.Content)
			if sanitized != "" && !IsSilentReply(sanitized) {
				rs.blockReplies++
				rs.lastBlockReply = sanitized
				emitRun(AgentEvent{
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
					rs.teamTaskCreates++
				}
			}
		}

		// Tool budget check: soft stop when total tool calls exceed the per-agent limit.
		// Same pattern as maxIterations — no error thrown, LLM summarizes and returns.
		rs.totalToolCalls += len(resp.ToolCalls)
		if l.maxToolCalls > 0 && rs.totalToolCalls > l.maxToolCalls {
			slog.Warn("security.tool_budget_exceeded",
				"agent", l.id, "total", rs.totalToolCalls, "limit", l.maxToolCalls)
			messages = append(messages, providers.Message{
				Role:    "user",
				Content: fmt.Sprintf("[System] Tool call budget reached (%d/%d). Do NOT call any more tools. Summarize results so far and respond to the user.", rs.totalToolCalls, l.maxToolCalls),
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
					"iteration": rs.iteration,
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

			// Process tool result: loop detection, events, media, deliverables.
			toolMsg, warningMsgs, action := l.processToolResult(ctx, rs, &req, emitRun, tc, registryName, result, hadBootstrap)
			messages = append(messages, toolMsg)
			rs.pendingMsgs = append(rs.pendingMsgs, toolMsg)
			messages = append(messages, warningMsgs...)
			if action == toolResultBreak {
				break
			}

			// Check for read-only streak (single tool path).
			if warnMsg, shouldBreak := l.checkReadOnlyStreak(rs, &req); shouldBreak {
				break
			} else if warnMsg != nil {
				messages = append(messages, *warnMsg)
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
					defer safego.Recover(func(v any) {
						resultCh <- indexedResult{
							idx:          idx,
							tc:           tc,
							registryName: tc.Name,
							result:       tools.ErrorResult(fmt.Sprintf("tool %q panicked: %v", tc.Name, v)),
						}
					}, "agent", l.id, "tool", tc.Name)
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

				// Process tool result: loop detection, events, media, deliverables.
				toolMsg, warningMsgs, action := l.processToolResult(ctx, rs, &req, emitRun, r.tc, r.registryName, r.result, hadBootstrap)
				messages = append(messages, toolMsg)
				rs.pendingMsgs = append(rs.pendingMsgs, toolMsg)
				messages = append(messages, warningMsgs...)
				if action == toolResultBreak {
					loopStuck = true
					break
				}
			}

			// Check read-only streak after processing all parallel results.
			if !loopStuck {
				if warnMsg, shouldBreak := l.checkReadOnlyStreak(rs, &req); shouldBreak {
					loopStuck = true
				} else if warnMsg != nil {
					messages = append(messages, *warnMsg)
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
			rs.pendingMsgs = append(rs.pendingMsgs, forSession...)
		}

		// Periodic checkpoint: flush pending messages to session every 5 iterations
		// to limit data loss on container crash (#294). Trade-off: partial visibility
		// to concurrent reads vs full data loss on crash.
		// AddMessage writes to in-memory cache; Save persists to DB. We must clear
		// rs.pendingMsgs after AddMessage to prevent double-add in the final flush.
		const checkpointInterval = 5
		if rs.iteration > 0 && rs.iteration%checkpointInterval == 0 && len(rs.pendingMsgs) > 0 {
			for _, msg := range rs.pendingMsgs {
				l.sessions.AddMessage(ctx, req.SessionKey, msg)
			}
			rs.checkpointFlushedMsgs += len(rs.pendingMsgs)
			rs.pendingMsgs = rs.pendingMsgs[:0]
			l.sessions.Save(ctx, req.SessionKey) //nolint:errcheck — best-effort persistence
		}

	}

	// 5-9. Finalize: sanitize, media dedup, session flush, bootstrap cleanup, build result.
	return l.finalizeRun(ctx, rs, &req, history, hadBootstrap, toolTiming), nil
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
