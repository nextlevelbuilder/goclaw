package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// bootstrapAutoCleanupTurns is the number of user messages after which
// BOOTSTRAP.md is auto-removed if the LLM hasn't cleared it.
// Bootstrap typically completes in 2-3 conversation turns.
const bootstrapAutoCleanupTurns = 3

// EnsureUserFilesFunc seeds per-user context files on first chat (managed mode).
// Returns the effective workspace path (from user_agent_profiles) for caching.
type EnsureUserFilesFunc func(ctx context.Context, agentID uuid.UUID, userID, agentType, workspace, channel string) (effectiveWorkspace string, err error)

// ContextFileLoaderFunc loads context files dynamically per-request (managed mode).
type ContextFileLoaderFunc func(ctx context.Context, agentID uuid.UUID, userID, agentType string) []bootstrap.ContextFile

// BootstrapCleanupFunc removes BOOTSTRAP.md after a successful first run.
// Called automatically so the system doesn't rely on the LLM to delete it.
type BootstrapCleanupFunc func(ctx context.Context, agentID uuid.UUID, userID string) error

// Loop is the agent execution loop for one agent instance.
// Think → Act → Observe cycle with tool execution.
type Loop struct {
	id            string
	agentUUID     uuid.UUID // set in managed mode for context propagation
	agentType     string    // "open" or "predefined" (managed mode)
	provider      providers.Provider
	model         string
	contextWindow int
	maxIterations int
	maxToolCalls  int
	workspace     string

	eventPub   bus.EventPublisher // currently unused by Loop; kept for future use
	sessions   store.SessionStore
	tools           *tools.Registry
	toolPolicy      *tools.PolicyEngine    // optional: filters tools sent to LLM
	agentToolPolicy *config.ToolPolicySpec // per-agent tool policy from DB (nil = no restrictions)
	activeRuns atomic.Int32 // number of currently executing runs

	// Per-session summarization lock: prevents concurrent summarize goroutines for the same session.
	summarizeMu sync.Map // sessionKey → *sync.Mutex

	// Bootstrap/persona context (loaded at startup, injected into system prompt)
	ownerIDs       []string
	skillsLoader   *skills.Loader
	skillAllowList []string // nil = all, [] = none, ["x","y"] = filter
	hasMemory      bool
	contextFiles   []bootstrap.ContextFile

	// Per-user file seeding + dynamic context loading (managed mode)
	ensureUserFiles    EnsureUserFilesFunc
	contextFileLoader  ContextFileLoaderFunc
	bootstrapCleanup   BootstrapCleanupFunc
	userWorkspaces     sync.Map // userID → string (expanded workspace path from user_agent_profiles)

	// Compaction config (memory flush settings)
	compactionCfg *config.CompactionConfig

	// Context pruning config (trim old tool results in-memory)
	contextPruningCfg *config.ContextPruningConfig

	// Sandbox info
	sandboxEnabled        bool
	sandboxContainerDir   string
	sandboxWorkspaceAccess string

	// Event callback for broadcasting agent events (run.started, chunk, tool.call, etc.)
	onEvent func(event AgentEvent)

	// Tracing collector (nil in standalone mode)
	traceCollector *tracing.Collector

	// Security: input scanning and message size limit
	inputGuard      *InputGuard
	injectionAction string // "log", "warn" (default), "block", "off"
	maxMessageChars int    // 0 = use default (32000)

	// Global builtin tool settings (from builtin_tools table, managed mode)
	builtinToolSettings tools.BuiltinToolSettings

	// Thinking level for extended thinking support
	thinkingLevel string

	// Group writer cache for system prompt injection (managed mode)
	groupWriterCache *store.GroupWriterCache

	// Team store for cross-session pending task detection (managed mode)
	teamStore store.TeamStore
}

// AgentEvent is emitted during agent execution for WS broadcasting.
type AgentEvent struct {
	Type    string      `json:"type"`    // "run.started", "run.completed", "run.failed", "chunk", "tool.call", "tool.result"
	AgentID string      `json:"agentId"`
	RunID   string      `json:"runId"`
	Payload interface{} `json:"payload,omitempty"`
}

// LoopConfig configures a new Loop.
type LoopConfig struct {
	ID            string
	Provider      providers.Provider
	Model         string
	ContextWindow int
	MaxIterations int
	MaxToolCalls  int
	Workspace     string
	Bus           bus.EventPublisher
	Sessions      store.SessionStore
	Tools           *tools.Registry
	ToolPolicy      *tools.PolicyEngine    // optional: filters tools sent to LLM
	AgentToolPolicy *config.ToolPolicySpec // per-agent tool policy from DB (nil = no restrictions)
	OnEvent         func(AgentEvent)

	// Bootstrap/persona context
	OwnerIDs       []string
	SkillsLoader   *skills.Loader
	SkillAllowList []string // nil = all, [] = none, ["x","y"] = filter
	HasMemory      bool
	ContextFiles   []bootstrap.ContextFile

	// Compaction config
	CompactionCfg *config.CompactionConfig

	// Context pruning (trim old tool results to save context window)
	ContextPruningCfg *config.ContextPruningConfig

	// Sandbox info (injected into system prompt)
	SandboxEnabled        bool
	SandboxContainerDir   string // e.g. "/workspace"
	SandboxWorkspaceAccess string // "none", "ro", "rw"

	// Managed mode: agent UUID for context propagation to tools
	AgentUUID uuid.UUID
	AgentType string // "open" or "predefined" (managed mode)

	// Per-user file seeding + dynamic context loading (managed mode)
	EnsureUserFiles   EnsureUserFilesFunc
	ContextFileLoader ContextFileLoaderFunc
	BootstrapCleanup  BootstrapCleanupFunc

	// Tracing collector (nil = no tracing)
	TraceCollector *tracing.Collector

	// Security: input guard for injection detection, max message size
	InputGuard      *InputGuard    // nil = auto-create when InjectionAction != "off"
	InjectionAction string         // "log", "warn" (default), "block", "off"
	MaxMessageChars int            // 0 = use default (32000)

	// Global builtin tool settings (from builtin_tools table, managed mode)
	BuiltinToolSettings tools.BuiltinToolSettings

	// Thinking level: "off", "low", "medium", "high" (from agent other_config)
	ThinkingLevel string

	// Group writer cache for system prompt injection (managed mode)
	GroupWriterCache *store.GroupWriterCache

	// Team store for cross-session pending task detection (managed mode)
	TeamStore store.TeamStore
}

func NewLoop(cfg LoopConfig) *Loop {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 20
	}
	if cfg.ContextWindow <= 0 {
		cfg.ContextWindow = 200000
	}

	// Normalize injection action (default: "warn")
	action := cfg.InjectionAction
	switch action {
	case "log", "warn", "block", "off":
		// valid
	default:
		action = "warn"
	}

	// Auto-create InputGuard unless explicitly disabled
	guard := cfg.InputGuard
	if guard == nil && action != "off" {
		guard = NewInputGuard()
	}

	return &Loop{
		id:            cfg.ID,
		agentUUID:     cfg.AgentUUID,
		agentType:     cfg.AgentType,
		provider:      cfg.Provider,
		model:         cfg.Model,
		contextWindow: cfg.ContextWindow,
		maxIterations: cfg.MaxIterations,
		maxToolCalls:  cfg.MaxToolCalls,
		workspace:     cfg.Workspace,
		eventPub:      cfg.Bus,
		sessions:      cfg.Sessions,
		tools:           cfg.Tools,
		toolPolicy:      cfg.ToolPolicy,
		agentToolPolicy: cfg.AgentToolPolicy,
		onEvent:         cfg.OnEvent,
		ownerIDs:      cfg.OwnerIDs,
		skillsLoader:   cfg.SkillsLoader,
		skillAllowList: cfg.SkillAllowList,
		hasMemory:     cfg.HasMemory,
		contextFiles:  cfg.ContextFiles,
		ensureUserFiles:    cfg.EnsureUserFiles,
		contextFileLoader:  cfg.ContextFileLoader,
		bootstrapCleanup:   cfg.BootstrapCleanup,
		compactionCfg:     cfg.CompactionCfg,
		contextPruningCfg: cfg.ContextPruningCfg,
		sandboxEnabled:        cfg.SandboxEnabled,
		sandboxContainerDir:   cfg.SandboxContainerDir,
		sandboxWorkspaceAccess: cfg.SandboxWorkspaceAccess,
		traceCollector:        cfg.TraceCollector,
		inputGuard:            guard,
		injectionAction:       action,
		maxMessageChars:       cfg.MaxMessageChars,
		builtinToolSettings:   cfg.BuiltinToolSettings,
		thinkingLevel:         cfg.ThinkingLevel,
		groupWriterCache:      cfg.GroupWriterCache,
		teamStore:             cfg.TeamStore,
	}
}

// RunRequest is the input for processing a message through the agent.
type RunRequest struct {
	SessionKey       string // composite key: agent:{agentId}:{channel}:{peerKind}:{chatId}
	Message          string // user message
	Media            []string // local file paths to images (already sanitized)
	ForwardMedia     []string // media paths to forward to output (not deleted, from delegation results)
	Channel          string // source channel
	ChatID           string // source chat ID
	PeerKind         string // "direct" or "group" (for session key building and tool context)
	RunID            string // unique run identifier
	UserID           string // external user ID (TEXT, free-form) for multi-tenant scoping
	SenderID         string // original individual sender ID (preserved in group chats for permission checks)
	Stream           bool   // whether to stream response chunks
	ExtraSystemPrompt string   // optional: injected into system prompt (skills, subagent context, etc.)
	SkillFilter       []string // per-request skill override: nil=use agent default, []=no skills, ["x","y"]=whitelist
	HistoryLimit      int      // max user turns to keep in context (0=unlimited, from channel config)
	ToolAllow         []string // per-group tool allow list (nil = no restriction, supports "group:xxx")
	LocalKey         string    // composite key with topic/thread suffix for routing (e.g. "-100123:topic:42")
	ParentTraceID    uuid.UUID // if set, reuse parent trace instead of creating new (announce runs)
	ParentRootSpanID uuid.UUID // if set, nest announce agent span under this parent span
	TraceName        string    // override trace name (default: "chat <agentID>")
	TraceTags        []string  // additional tags for the trace (e.g. "cron")
	MaxIterations    int       // per-request override (0 = use agent default, must be lower)
}

// RunResult is the output of a completed agent run.
type RunResult struct {
	Content      string           `json:"content"`
	RunID        string           `json:"runId"`
	Iterations   int              `json:"iterations"`
	Usage        *providers.Usage `json:"usage,omitempty"`
	Media        []MediaResult    `json:"media,omitempty"`         // media files from tool results (MEDIA: prefix)
	Deliverables []string         `json:"deliverables,omitempty"`  // actual content from tool outputs (for team task results)
}

// MediaResult represents a media file produced by a tool during the agent run.
type MediaResult struct {
	Path        string `json:"path"`                  // local file path
	ContentType string `json:"content_type,omitempty"` // MIME type
	AsVoice     bool   `json:"as_voice,omitempty"`     // send as voice message (Telegram OGG)
}

// Run processes a single message through the agent loop.
// It blocks until completion and returns the final response.
func (l *Loop) Run(ctx context.Context, req RunRequest) (*RunResult, error) {
	l.activeRuns.Add(1)
	defer l.activeRuns.Add(-1)

	l.emit(AgentEvent{Type: protocol.AgentEventRunStarted, AgentID: l.id, RunID: req.RunID})

	// Create trace (managed mode only)
	var traceID uuid.UUID
	isChildTrace := req.ParentTraceID != uuid.Nil && l.traceCollector != nil

	if isChildTrace {
		// Announce run: reuse parent trace, don't create new trace record.
		// Spans will be added to the parent trace with proper nesting.
		traceID = req.ParentTraceID
		ctx = tracing.WithTraceID(ctx, traceID)
		ctx = tracing.WithCollector(ctx, l.traceCollector)
		ctx = tracing.WithParentSpanID(ctx, store.GenNewID())
		if req.ParentRootSpanID != uuid.Nil {
			ctx = tracing.WithAnnounceParentSpanID(ctx, req.ParentRootSpanID)
		}
	} else if l.traceCollector != nil {
		traceID = store.GenNewID()
		now := time.Now().UTC()
		traceName := "chat " + l.id
		if req.TraceName != "" {
			traceName = req.TraceName
		}
		trace := &store.TraceData{
			ID:           traceID,
			RunID:        req.RunID,
			SessionKey:   req.SessionKey,
			UserID:       req.UserID,
			Channel:      req.Channel,
			Name:         traceName,
			InputPreview: truncateStr(req.Message, 500),
			Status:       store.TraceStatusRunning,
			StartTime:    now,
			CreatedAt:    now,
			Tags:         req.TraceTags,
		}
		if l.agentUUID != uuid.Nil {
			trace.AgentID = &l.agentUUID
		}
		// Link to parent trace if this is a delegated run
		if delegateParent := tracing.DelegateParentTraceIDFromContext(ctx); delegateParent != uuid.Nil {
			trace.ParentTraceID = &delegateParent
		}
		if err := l.traceCollector.CreateTrace(ctx, trace); err != nil {
			slog.Warn("tracing: failed to create trace", "error", err)
		} else {
			ctx = tracing.WithTraceID(ctx, traceID)
			ctx = tracing.WithCollector(ctx, l.traceCollector)

			// Pre-generate root "agent" span ID so LLM/tool spans can reference it as parent.
			// The span itself is emitted after runLoop completes (with full timing data).
			ctx = tracing.WithParentSpanID(ctx, store.GenNewID())
		}
	}

	// Inject local key into tool context so delegation/subagent tools can
	// propagate topic/thread routing info back through announce messages.
	if req.LocalKey != "" {
		ctx = tools.WithToolLocalKey(ctx, req.LocalKey)
	}

	runStart := time.Now().UTC()
	result, err := l.runLoop(ctx, req)

	// Emit root "agent" span with full timing (parent for all LLM/tool spans).
	if l.traceCollector != nil && traceID != uuid.Nil {
		l.emitAgentSpan(ctx, runStart, result, err)
	}

	if err != nil {
		l.emit(AgentEvent{
			Type:    protocol.AgentEventRunFailed,
			AgentID: l.id,
			RunID:   req.RunID,
			Payload: map[string]string{"error": err.Error()},
		})
		// Only finish trace for root runs; child traces don't own the trace lifecycle.
		// Use background context when the run context is cancelled (/stop command)
		// so the DB update still succeeds.
		if !isChildTrace && l.traceCollector != nil && traceID != uuid.Nil {
			traceCtx := ctx
			traceStatus := store.TraceStatusError
			if ctx.Err() != nil {
				traceCtx = context.Background()
				traceStatus = store.TraceStatusCancelled
			}
			l.traceCollector.FinishTrace(traceCtx, traceID, traceStatus, err.Error(), "")
		}
		return nil, err
	}

	l.emit(AgentEvent{Type: protocol.AgentEventRunCompleted, AgentID: l.id, RunID: req.RunID})
	if !isChildTrace && l.traceCollector != nil && traceID != uuid.Nil {
		l.traceCollector.FinishTrace(ctx, traceID, store.TraceStatusCompleted, "", truncateStr(result.Content, 500))
	}
	return result, nil
}

func (l *Loop) runLoop(ctx context.Context, req RunRequest) (*RunResult, error) {
	// Inject agent UUID into context for tool routing (managed mode)
	if l.agentUUID != uuid.Nil {
		ctx = store.WithAgentID(ctx, l.agentUUID)
	}
	// Inject user ID into context for per-user scoping (memory, context files, etc.)
	if req.UserID != "" {
		ctx = store.WithUserID(ctx, req.UserID)
	}
	// Inject agent type into context for interceptor routing (managed mode)
	if l.agentType != "" {
		ctx = store.WithAgentType(ctx, l.agentType)
	}
	// Inject original sender ID for group file writer permission checks
	if req.SenderID != "" {
		ctx = store.WithSenderID(ctx, req.SenderID)
	}
	// Inject per-agent vision/imagegen config for read_image/create_image tools
	if l.agentToolPolicy != nil {
		if l.agentToolPolicy.Vision != nil {
			ctx = tools.WithVisionConfig(ctx, l.agentToolPolicy.Vision)
		}
		if l.agentToolPolicy.ImageGen != nil {
			ctx = tools.WithImageGenConfig(ctx, l.agentToolPolicy.ImageGen)
		}
	}
	// Inject global builtin tool settings (DB-level defaults, lower priority than per-agent)
	if l.builtinToolSettings != nil {
		ctx = tools.WithBuiltinToolSettings(ctx, l.builtinToolSettings)
	}

	// Per-user workspace isolation.
	// Workspace path comes from user_agent_profiles (includes channel segment
	// for cross-channel isolation). Cached in userWorkspaces to avoid repeated DB queries.
	if l.workspace != "" && req.UserID != "" {
		cachedWs, loaded := l.userWorkspaces.Load(req.UserID)
		if !loaded {
			// First request for this user: get/create profile → returns stored workspace.
			// Also seeds per-user context files on first chat (managed mode).
			ws := l.workspace
			if l.ensureUserFiles != nil {
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
		effectiveWorkspace := filepath.Join(cachedWs.(string), sanitizePathSegment(req.UserID))
		if err := os.MkdirAll(effectiveWorkspace, 0755); err != nil {
			slog.Warn("failed to create user workspace directory", "workspace", effectiveWorkspace, "user", req.UserID, "error", err)
		}
		ctx = tools.WithToolWorkspace(ctx, effectiveWorkspace)
	} else if l.workspace != "" {
		ctx = tools.WithToolWorkspace(ctx, l.workspace)
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

	// Inject agent key into context for tool-level resolution (managed mode: multiple agents share tool registry)
	ctx = tools.WithToolAgentKey(ctx, l.id)

	// Security: truncate oversized user messages gracefully (feed truncation notice into LLM)
	maxChars := l.maxMessageChars
	if maxChars <= 0 {
		maxChars = 32_000 // default ~8-10K tokens
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

	// 1. Build messages from session history
	history := l.sessions.GetHistory(req.SessionKey)
	summary := l.sessions.GetSummary(req.SessionKey)

	// buildMessages resolves context files once and also detects BOOTSTRAP.md presence
	// (hadBootstrap) — no extra DB roundtrip needed for bootstrap detection.
	messages, hadBootstrap := l.buildMessages(ctx, history, summary, req.Message, req.ExtraSystemPrompt, req.SessionKey, req.Channel, req.UserID, req.HistoryLimit, req.SkillFilter)

	// 2. Attach vision images to the current user message (last in messages slice).
	// Images are only attached to the live request, NOT persisted in session history.
	if len(req.Media) > 0 {
		if images := loadImages(req.Media); len(images) > 0 {
			messages[len(messages)-1].Images = images
			ctx = tools.WithMediaImages(ctx, images) // make images available to read_image tool
			slog.Info("vision: attached images to user message", "count", len(images), "agent", l.id, "session", req.SessionKey)
		}
		// Clean up temp media files — they're now base64-encoded in memory.
		for _, p := range req.Media {
			if err := os.Remove(p); err != nil {
				slog.Debug("vision: failed to clean temp media file", "path", p, "error", err)
			}
		}
	}

	// 2b. Cross-session recovery: notify team leads about orphaned pending tasks
	// and in-progress tasks being handled by delegates.
	// Safe because Bước 1 (early ClaimTask) ensures running tasks are in_progress,
	// so only truly un-spawned tasks remain pending.
	if l.teamStore != nil && l.agentUUID != uuid.Nil {
		if team, _ := l.teamStore.GetTeamForAgent(ctx, l.agentUUID); team != nil && team.LeadAgentID == l.agentUUID {
			if tasks, err := l.teamStore.ListTasks(ctx, team.ID, "newest", "", req.UserID); err == nil {
				var stale []string
				var inProgress []string
				for _, t := range tasks {
					if t.Status == store.TeamTaskStatusPending {
						age := time.Since(t.CreatedAt).Truncate(time.Minute)
						stale = append(stale, fmt.Sprintf("- %s: \"%s\" (pending %s)", t.ID, t.Subject, age))
					}
					if t.Status == store.TeamTaskStatusInProgress {
						age := time.Since(t.UpdatedAt).Truncate(time.Minute)
						inProgress = append(inProgress, fmt.Sprintf("- %s: \"%s\" (in progress %s)", t.ID, t.Subject, age))
					}
				}
				var parts []string
				if len(stale) > 0 {
					parts = append(parts, fmt.Sprintf(
						"You have %d pending team task(s) that were never spawned:\n%s\n"+
							"Spawn each one, or cancel with team_tasks action=cancel if no longer needed.",
						len(stale), strings.Join(stale, "\n")))
				}
				if len(inProgress) > 0 {
					parts = append(parts, fmt.Sprintf(
						"You have %d in-progress team task(s) being handled by delegates:\n%s\n"+
							"Their results will arrive automatically. Do NOT cancel, re-create, or re-spawn these tasks.",
						len(inProgress), strings.Join(inProgress, "\n")))
				}
				if len(parts) > 0 {
					reminder := "[System] " + strings.Join(parts, "\n\n")
					messages = append(messages,
						providers.Message{Role: "user", Content: reminder},
						providers.Message{Role: "assistant", Content: "I see the task status. Let me handle accordingly."},
					)
				}
			}
		}
	}

	// 3. Buffer new messages — write to session only AFTER the run completes.
	// This prevents concurrent runs from seeing each other's in-progress messages.
	// NOTE: pendingMsgs stores TEXT ONLY (no images) to avoid bloating session storage.
	var pendingMsgs []providers.Message
	pendingMsgs = append(pendingMsgs, providers.Message{
		Role:    "user",
		Content: req.Message,
	})

	// 4. Run LLM iteration loop
	var loopDetector toolLoopState // detects repeated no-progress tool calls
	var totalUsage providers.Usage
	iteration := 0
	totalToolCalls := 0
	var finalContent string
	var asyncToolCalls []string   // track async spawn tool names for fallback
	var mediaResults []MediaResult // media files from tool MEDIA: results
	var deliverables []string      // actual content from tool outputs (for team task results)

	// Mid-loop compaction: summarize in-memory messages when context exceeds threshold.
	// Uses same config as maybeSummarize (contextWindow * historyShare).
	var midLoopCompacted bool

	// Team task orphan detection: track team_tasks create vs spawn calls.
	// If the LLM creates tasks but forgets to spawn, inject a reminder.
	var teamTaskCreates int  // count of team_tasks action=create calls
	var teamTaskSpawns  int  // count of spawn calls with team_task_id
	var teamTaskRetried bool // only retry once to prevent infinite loops

	// Inject retry hook so channels can update placeholder on LLM retries.
	ctx = providers.WithRetryHook(ctx, func(attempt, maxAttempts int, err error) {
		l.emit(AgentEvent{
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

	for iteration < maxIter {
		iteration++

		slog.Debug("agent iteration", "agent", l.id, "iteration", iteration, "messages", len(messages))

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

		chatReq := providers.ChatRequest{
			Messages: messages,
			Tools:    toolDefs,
			Model:    l.model,
			Options: map[string]interface{}{
				providers.OptMaxTokens:   8192,
				providers.OptTemperature: 0.7,
				providers.OptSessionKey:  req.SessionKey,
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

		if req.Stream {
			resp, err = l.provider.ChatStream(ctx, chatReq, func(chunk providers.StreamChunk) {
				if chunk.Thinking != "" {
					l.emit(AgentEvent{
						Type:    protocol.ChatEventThinking,
						AgentID: l.id,
						RunID:   req.RunID,
						Payload: map[string]string{"content": chunk.Thinking},
					})
				}
				if chunk.Content != "" {
					l.emit(AgentEvent{
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
			l.emitLLMSpan(ctx, llmSpanStart, iteration, messages, nil, err)
			return nil, fmt.Errorf("LLM call failed (iteration %d): %w", iteration, err)
		}

		l.emitLLMSpan(ctx, llmSpanStart, iteration, messages, resp, nil)

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
			historyShare := 0.75
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

		// No tool calls → done
		if len(resp.ToolCalls) == 0 {
			// Guard: detect orphaned team_tasks create (created but not spawned).
			// Query DB for actual pending tasks instead of just counting tool calls,
			// because auto-created tasks (from spawn without team_task_id) bypass the counter.
			if teamTaskCreates > teamTaskSpawns && !teamTaskRetried {
				if l.teamStore != nil && l.agentUUID != uuid.Nil {
					if team, _ := l.teamStore.GetTeamForAgent(ctx, l.agentUUID); team != nil {
						if tasks, err := l.teamStore.ListTasks(ctx, team.ID, "newest", "", req.UserID); err == nil {
							var pendingIDs []string
							for _, t := range tasks {
								if t.Status == store.TeamTaskStatusPending {
									pendingIDs = append(pendingIDs, t.ID.String())
								}
							}
							if len(pendingIDs) > 0 {
								teamTaskRetried = true
								slog.Warn("team task orphan detected",
									"agent", l.id, "pending", len(pendingIDs),
									"creates", teamTaskCreates, "spawns", teamTaskSpawns)
								messages = append(messages,
									providers.Message{Role: "assistant", Content: resp.Content},
									providers.Message{
										Role:    "user",
										Content: fmt.Sprintf("[System] You have %d pending task(s) that were never delegated: %s. Call `spawn` for each, or cancel with team_tasks action=cancel.", len(pendingIDs), strings.Join(pendingIDs, ", ")),
									},
								)
								continue
							}
						}
					}
				}
			}
			finalContent = resp.Content
			break
		}

		// Build assistant message with tool calls
		assistantMsg := providers.Message{
			Role:                "assistant",
			Content:             resp.Content,
			ToolCalls:           resp.ToolCalls,
			RawAssistantContent: resp.RawAssistantContent, // preserve thinking blocks for Anthropic passback
		}
		messages = append(messages, assistantMsg)
		pendingMsgs = append(pendingMsgs, assistantMsg)

		// Track team_tasks create for orphan detection (argument-based, pre-execution).
		// Spawn counting is done post-execution so failed spawns don't get counted.
		for _, tc := range resp.ToolCalls {
			if tc.Name == "team_tasks" {
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

		// Execute tool calls (parallel when multiple, sequential when single)
		if len(resp.ToolCalls) == 1 {
			// Single tool: sequential — no goroutine overhead
			tc := resp.ToolCalls[0]
			l.emit(AgentEvent{
				Type:    protocol.AgentEventToolCall,
				AgentID: l.id,
				RunID:   req.RunID,
				Payload: map[string]interface{}{"name": tc.Name, "id": tc.ID},
			})

			argsJSON, _ := json.Marshal(tc.Arguments)
			slog.Info("tool call", "agent", l.id, "tool", tc.Name, "args_len", len(argsJSON))

			argsHash := loopDetector.record(tc.Name, tc.Arguments)

			toolSpanStart := time.Now().UTC()
			var result *tools.Result
			if allowedTools != nil && !allowedTools[tc.Name] {
				slog.Warn("security.tool_policy_blocked", "agent", l.id, "tool", tc.Name)
				result = tools.ErrorResult("tool not allowed by policy: " + tc.Name)
			} else {
				result = l.tools.ExecuteWithContext(ctx, tc.Name, tc.Arguments, req.Channel, req.ChatID, req.PeerKind, req.SessionKey, nil)
			}

			l.emitToolSpan(ctx, toolSpanStart, tc.Name, tc.ID, string(argsJSON), result)

			// Record result for loop detection.
			loopDetector.recordResult(argsHash, result.ForLLM)

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
			if tc.Name == "spawn" && !result.IsError {
				if tid, _ := tc.Arguments["team_task_id"].(string); tid != "" {
					teamTaskSpawns++
				}
			}

			l.emit(AgentEvent{
				Type:    protocol.AgentEventToolResult,
				AgentID: l.id,
				RunID:   req.RunID,
				Payload: map[string]interface{}{
					"name":     tc.Name,
					"id":       tc.ID,
					"is_error": result.IsError,
				},
			})

			// Collect MEDIA: paths from tool results
			if mr := parseMediaResult(result.ForLLM); mr != nil {
				mediaResults = append(mediaResults, *mr)
			}
			for _, p := range result.Media {
				mediaResults = append(mediaResults, MediaResult{Path: p, ContentType: mimeFromExt(filepath.Ext(p))})
			}
			if result.Deliverable != "" {
				deliverables = append(deliverables, result.Deliverable)
			}

			toolMsg := providers.Message{
				Role:       "tool",
				Content:    result.ForLLM,
				ToolCallID: tc.ID,
			}
			messages = append(messages, toolMsg)
			pendingMsgs = append(pendingMsgs, toolMsg)

			// Check for tool call loop after recording result.
			if level, msg := loopDetector.detect(tc.Name, argsHash); level != "" {
				if level == "critical" {
					slog.Warn("tool loop critical", "agent", l.id, "tool", tc.Name, "message", msg)
					finalContent = "I was unable to complete this task — I got stuck repeatedly calling " + tc.Name + " without making progress. Please try rephrasing your request."
					break
				}
				// Warning: inject message so model knows to change strategy.
				slog.Warn("tool loop warning", "agent", l.id, "tool", tc.Name, "message", msg)
				messages = append(messages, providers.Message{Role: "user", Content: msg})
			}
		} else {
			// Multiple tools: parallel execution via goroutines.
			// Tool instances are immutable (context-based) so concurrent access is safe.
			// Results are collected then processed sequentially for deterministic ordering.
			type indexedResult struct {
				idx       int
				tc        providers.ToolCall
				result    *tools.Result
				argsJSON  string
				spanStart time.Time
			}

			// 1. Emit all tool.call events upfront (client sees all calls starting)
			for _, tc := range resp.ToolCalls {
				l.emit(AgentEvent{
					Type:    protocol.AgentEventToolCall,
					AgentID: l.id,
					RunID:   req.RunID,
					Payload: map[string]interface{}{"name": tc.Name, "id": tc.ID},
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
					var result *tools.Result
					if allowedTools != nil && !allowedTools[tc.Name] {
						slog.Warn("security.tool_policy_blocked", "agent", l.id, "tool", tc.Name)
						result = tools.ErrorResult("tool not allowed by policy: " + tc.Name)
					} else {
						result = l.tools.ExecuteWithContext(ctx, tc.Name, tc.Arguments, req.Channel, req.ChatID, req.PeerKind, req.SessionKey, nil)
					}
					resultCh <- indexedResult{idx: idx, tc: tc, result: result, argsJSON: string(argsJSON), spanStart: spanStart}
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
			var loopStuck bool
			for _, r := range collected {
				l.emitToolSpan(ctx, r.spanStart, r.tc.Name, r.tc.ID, r.argsJSON, r.result)

				// Record for loop detection.
				argsHash := loopDetector.record(r.tc.Name, r.tc.Arguments)
				loopDetector.recordResult(argsHash, r.result.ForLLM)

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
				if r.tc.Name == "spawn" && !r.result.IsError {
					if tid, _ := r.tc.Arguments["team_task_id"].(string); tid != "" {
						teamTaskSpawns++
					}
				}

				l.emit(AgentEvent{
					Type:    protocol.AgentEventToolResult,
					AgentID: l.id,
					RunID:   req.RunID,
					Payload: map[string]interface{}{
						"name":     r.tc.Name,
						"id":       r.tc.ID,
						"is_error": r.result.IsError,
					},
				})

				// Collect MEDIA: paths from tool results
				if mr := parseMediaResult(r.result.ForLLM); mr != nil {
					mediaResults = append(mediaResults, *mr)
				}
				for _, p := range r.result.Media {
					mediaResults = append(mediaResults, MediaResult{Path: p, ContentType: mimeFromExt(filepath.Ext(p))})
				}
				if r.result.Deliverable != "" {
					deliverables = append(deliverables, r.result.Deliverable)
				}

				toolMsg := providers.Message{
					Role:       "tool",
					Content:    r.result.ForLLM,
					ToolCallID: r.tc.ID,
				}
				messages = append(messages, toolMsg)
				pendingMsgs = append(pendingMsgs, toolMsg)

				// Check for tool call loop.
				if level, msg := loopDetector.detect(r.tc.Name, argsHash); level != "" {
					if level == "critical" {
						slog.Warn("tool loop critical", "agent", l.id, "tool", r.tc.Name, "message", msg)
						finalContent = "I was unable to complete this task — I got stuck repeatedly calling " + r.tc.Name + " without making progress. Please try rephrasing your request."
						loopStuck = true
						break
					}
					slog.Warn("tool loop warning", "agent", l.id, "tool", r.tc.Name, "message", msg)
					messages = append(messages, providers.Message{Role: "user", Content: msg})
				}
			}
			if loopStuck {
				break
			}
		}
	}

	// 4. Full sanitization pipeline (matching TS extractAssistantText + sanitizeUserFacingText)
	finalContent = SanitizeAssistantContent(finalContent)

	// 5. Handle NO_REPLY: save to session for context but mark as silent.
	// Matching TS: NO_REPLY is saved (via resolveSilentReplyFallbackText) but
	// filtered at the payload level before delivery.
	isSilent := IsSilentReply(finalContent)

	// 6. Fallback for empty content
	if finalContent == "" {
		if len(asyncToolCalls) > 0 {
			finalContent = "..."
		} else {
			finalContent = "..."
		}
	}

	pendingMsgs = append(pendingMsgs, providers.Message{
		Role:    "assistant",
		Content: finalContent,
	})

	// Flush all buffered messages to session atomically.
	// This ensures concurrent runs never see each other's in-progress messages.
	for _, msg := range pendingMsgs {
		l.sessions.AddMessage(req.SessionKey, msg)
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

	// Include forwarded media from delegation results (not cleaned up like req.Media)
	for _, p := range req.ForwardMedia {
		mediaResults = append(mediaResults, MediaResult{Path: p, ContentType: mimeFromExt(filepath.Ext(p))})
	}

	return &RunResult{
		Content:      finalContent,
		RunID:        req.RunID,
		Iterations:   iteration,
		Usage:        &totalUsage,
		Media:        mediaResults,
		Deliverables: deliverables,
	}, nil
}

// compactMessagesInPlace summarizes the first ~70% of messages into a condensed
// summary, keeping the last ~30% intact. Operates purely on the local messages
// slice — no session state touched, no locks needed.
// Returns nil on failure (caller keeps original messages).
func (l *Loop) compactMessagesInPlace(ctx context.Context, messages []providers.Message) []providers.Message {
	if len(messages) < 6 {
		return nil
	}

	// Resolve keepCount from compaction config (same defaults as maybeSummarize).
	keepCount := 4
	if l.compactionCfg != nil && l.compactionCfg.KeepLastMessages > 0 {
		keepCount = l.compactionCfg.KeepLastMessages
	}
	// Ensure we keep at least 30% of messages.
	if minKeep := len(messages) * 3 / 10; minKeep > keepCount {
		keepCount = minKeep
	}

	splitIdx := len(messages) - keepCount

	// Walk backward from splitIdx to find a clean boundary —
	// avoid splitting tool_use → tool_result pairs.
	for splitIdx > 0 {
		m := messages[splitIdx]
		if m.Role == "tool" || (m.Role == "assistant" && len(m.ToolCalls) > 0) {
			splitIdx--
			continue
		}
		break
	}
	if splitIdx <= 1 {
		return nil
	}

	// Build summary input (same pattern as maybeSummarize in loop_history.go).
	toSummarize := messages[:splitIdx]
	var sb strings.Builder
	for _, m := range toSummarize {
		switch m.Role {
		case "user":
			fmt.Fprintf(&sb, "user: %s\n", m.Content)
		case "assistant":
			fmt.Fprintf(&sb, "assistant: %s\n", SanitizeAssistantContent(m.Content))
		}
	}

	sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := l.provider.Chat(sctx, providers.ChatRequest{
		Messages: []providers.Message{{
			Role:    "user",
			Content: "Provide a concise summary of this conversation, preserving key findings, data, and context:\n\n" + sb.String(),
		}},
		Model:   l.model,
		Options: map[string]interface{}{"max_tokens": 1024, "temperature": 0.3},
	})
	if err != nil {
		slog.Warn("mid_loop_compaction_failed", "agent", l.id, "error", err)
		return nil
	}

	summary := providers.Message{
		Role:    "user",
		Content: "[Summary of earlier conversation]\n" + SanitizeAssistantContent(resp.Content),
	}
	result := make([]providers.Message, 0, 1+keepCount)
	result = append(result, summary)
	result = append(result, messages[splitIdx:]...)

	slog.Info("mid_loop_compacted",
		"agent", l.id,
		"original_msgs", len(messages),
		"summarized", splitIdx,
		"kept", len(result))

	return result
}

// parseMediaResult extracts a MediaResult from a tool result string containing "MEDIA:" prefix.
// Handles formats: "MEDIA:/path/to/file" and "[[audio_as_voice]]\nMEDIA:/path/to/file".
// Returns nil if no MEDIA: prefix is found.
//
// IMPORTANT: Only matches "MEDIA:" at the start of the (trimmed) string to avoid false
// positives when tool output contains "MEDIA:" in arbitrary text (e.g. a web page
// mentioning a commit message like "return MEDIA: path from screenshot").
func parseMediaResult(toolOutput string) *MediaResult {
	s := toolOutput
	asVoice := false

	// Check for [[audio_as_voice]] tag (TTS voice messages)
	if strings.Contains(s, "[[audio_as_voice]]") {
		asVoice = true
		s = strings.ReplaceAll(s, "[[audio_as_voice]]", "")
	}

	s = strings.TrimSpace(s)

	// Only match MEDIA: at the beginning of the string.
	if !strings.HasPrefix(s, "MEDIA:") {
		return nil
	}
	path := strings.TrimSpace(s[6:])
	if path == "" {
		return nil
	}
	// Take only the first line (in case there's trailing text)
	if nl := strings.IndexByte(path, '\n'); nl >= 0 {
		path = strings.TrimSpace(path[:nl])
	}

	return &MediaResult{
		Path:        path,
		ContentType: mimeFromExt(filepath.Ext(path)),
		AsVoice:     asVoice,
	}
}

// mimeFromExt returns a MIME type for common media file extensions.
func mimeFromExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp4":
		return "video/mp4"
	case ".ogg", ".opus":
		return "audio/ogg"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".txt":
		return "text/plain"
	case ".pdf":
		return "application/pdf"
	case ".csv":
		return "text/csv"
	case ".json":
		return "application/json"
	case ".html", ".htm":
		return "text/html"
	case ".xml":
		return "application/xml"
	case ".zip":
		return "application/zip"
	case ".doc", ".docx":
		return "application/msword"
	case ".xls", ".xlsx":
		return "application/vnd.ms-excel"
	default:
		return "application/octet-stream"
	}
}

// sanitizePathSegment makes a userID safe for use as a directory name.
// Replaces colons, spaces, and other unsafe chars with underscores.
func sanitizePathSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// InvalidateUserWorkspace clears the cached workspace for a user,
// forcing the next request to re-read from user_agent_profiles.
func (l *Loop) InvalidateUserWorkspace(userID string) {
	l.userWorkspaces.Delete(userID)
}

// ProviderName returns the name of this agent's LLM provider (e.g. "anthropic", "openai").
func (l *Loop) ProviderName() string {
	if l.provider == nil {
		return ""
	}
	return l.provider.Name()
}
