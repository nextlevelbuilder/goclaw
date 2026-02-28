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
type EnsureUserFilesFunc func(ctx context.Context, agentID uuid.UUID, userID, agentType, workspace string) error

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
	workspace     string

	eventPub        bus.EventPublisher // currently unused by Loop; kept for future use
	sessions        store.SessionStore
	tools           *tools.Registry
	toolPolicy      *tools.PolicyEngine    // optional: filters tools sent to LLM
	agentToolPolicy *config.ToolPolicySpec // per-agent tool policy from DB (nil = no restrictions)
	activeRuns      atomic.Int32           // number of currently executing runs

	// Per-session summarization lock: prevents concurrent summarize goroutines for the same session.
	summarizeMu sync.Map // sessionKey → *sync.Mutex

	// Bootstrap/persona context (loaded at startup, injected into system prompt)
	ownerIDs       []string
	skillsLoader   *skills.Loader
	skillAllowList []string // nil = all, [] = none, ["x","y"] = filter
	hasMemory      bool
	contextFiles   []bootstrap.ContextFile

	// Per-user file seeding + dynamic context loading (managed mode)
	ensureUserFiles   EnsureUserFilesFunc
	contextFileLoader ContextFileLoaderFunc
	bootstrapCleanup  BootstrapCleanupFunc
	seededUsers       sync.Map // userID → true, avoid re-check per request

	// Compaction config (memory flush settings)
	compactionCfg *config.CompactionConfig

	// Context pruning config (trim old tool results in-memory)
	contextPruningCfg *config.ContextPruningConfig

	// Sandbox info
	sandboxEnabled         bool
	sandboxContainerDir    string
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
}

// AgentEvent is emitted during agent execution for WS broadcasting.
type AgentEvent struct {
	Type    string      `json:"type"` // "run.started", "run.completed", "run.failed", "chunk", "tool.call", "tool.result"
	AgentID string      `json:"agentId"`
	RunID   string      `json:"runId"`
	Payload interface{} `json:"payload,omitempty"`
}

// LoopConfig configures a new Loop.
type LoopConfig struct {
	ID              string
	Provider        providers.Provider
	Model           string
	ContextWindow   int
	MaxIterations   int
	Workspace       string
	Bus             bus.EventPublisher
	Sessions        store.SessionStore
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
	SandboxEnabled         bool
	SandboxContainerDir    string // e.g. "/workspace"
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
	InputGuard      *InputGuard // nil = auto-create when InjectionAction != "off"
	InjectionAction string      // "log", "warn" (default), "block", "off"
	MaxMessageChars int         // 0 = use default (32000)

	// Global builtin tool settings (from builtin_tools table, managed mode)
	BuiltinToolSettings tools.BuiltinToolSettings

	// Thinking level: "off", "low", "medium", "high" (from agent other_config)
	ThinkingLevel string
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
		id:                     cfg.ID,
		agentUUID:              cfg.AgentUUID,
		agentType:              cfg.AgentType,
		provider:               cfg.Provider,
		model:                  cfg.Model,
		contextWindow:          cfg.ContextWindow,
		maxIterations:          cfg.MaxIterations,
		workspace:              cfg.Workspace,
		eventPub:               cfg.Bus,
		sessions:               cfg.Sessions,
		tools:                  cfg.Tools,
		toolPolicy:             cfg.ToolPolicy,
		agentToolPolicy:        cfg.AgentToolPolicy,
		onEvent:                cfg.OnEvent,
		ownerIDs:               cfg.OwnerIDs,
		skillsLoader:           cfg.SkillsLoader,
		skillAllowList:         cfg.SkillAllowList,
		hasMemory:              cfg.HasMemory,
		contextFiles:           cfg.ContextFiles,
		ensureUserFiles:        cfg.EnsureUserFiles,
		contextFileLoader:      cfg.ContextFileLoader,
		bootstrapCleanup:       cfg.BootstrapCleanup,
		compactionCfg:          cfg.CompactionCfg,
		contextPruningCfg:      cfg.ContextPruningCfg,
		sandboxEnabled:         cfg.SandboxEnabled,
		sandboxContainerDir:    cfg.SandboxContainerDir,
		sandboxWorkspaceAccess: cfg.SandboxWorkspaceAccess,
		traceCollector:         cfg.TraceCollector,
		inputGuard:             guard,
		injectionAction:        action,
		maxMessageChars:        cfg.MaxMessageChars,
		builtinToolSettings:    cfg.BuiltinToolSettings,
		thinkingLevel:          cfg.ThinkingLevel,
	}
}

// RunRequest is the input for processing a message through the agent.
type RunRequest struct {
	SessionKey        string    // composite key: agent:{agentId}:{channel}:{peerKind}:{chatId}
	Message           string    // user message
	Media             []string  // local file paths to images (already sanitized)
	Channel           string    // source channel
	ChatID            string    // source chat ID
	PeerKind          string    // "direct" or "group" (for session key building and tool context)
	RunID             string    // unique run identifier
	UserID            string    // external user ID (TEXT, free-form) for multi-tenant scoping
	SenderID          string    // original individual sender ID (preserved in group chats for permission checks)
	Stream            bool      // whether to stream response chunks
	ExtraSystemPrompt string    // optional: injected into system prompt (skills, subagent context, etc.)
	HistoryLimit      int       // max user turns to keep in context (0=unlimited, from channel config)
	ParentTraceID     uuid.UUID // if set, reuse parent trace instead of creating new (announce runs)
	ParentRootSpanID  uuid.UUID // if set, nest announce agent span under this parent span
	TraceName         string    // override trace name (default: "chat <agentID>")
	TraceTags         []string  // additional tags for the trace (e.g. "cron")
}

// RunResult is the output of a completed agent run.
type RunResult struct {
	Content    string           `json:"content"`
	RunID      string           `json:"runId"`
	Iterations int              `json:"iterations"`
	Usage      *providers.Usage `json:"usage,omitempty"`
	Media      []MediaResult    `json:"media,omitempty"` // media files from tool results (MEDIA: prefix)
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
	// 1. Prepare context: inject per-request values, setup workspace, security checks
	var err error
	ctx, req, err = l.prepareContext(ctx, req)
	if err != nil {
		return nil, err
	}

	// 2. Build messages from session history
	history := l.sessions.GetHistory(req.SessionKey)
	summary := l.sessions.GetSummary(req.SessionKey)

	// buildMessages resolves context files once and also detects BOOTSTRAP.md presence
	// (hadBootstrap) — no extra DB roundtrip needed for bootstrap detection.
	messages, hadBootstrap := l.buildMessages(ctx, history, summary, req.Message, req.ExtraSystemPrompt, req.SessionKey, req.Channel, req.UserID, req.HistoryLimit)

	// 3. Attach vision images to the current user message (last in messages slice).
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

	// 4. Buffer new messages — write to session only AFTER the run completes.
	// This prevents concurrent runs from seeing each other's in-progress messages.
	// NOTE: pendingMsgs stores TEXT ONLY (no images) to avoid bloating session storage.
	var pendingMsgs []providers.Message
	pendingMsgs = append(pendingMsgs, providers.Message{
		Role:    "user",
		Content: req.Message,
	})

	// 5. Run LLM iteration loop
	var loopDetector toolLoopState // detects repeated no-progress tool calls
	var totalUsage providers.Usage
	iteration := 0
	var finalContent string
	var asyncToolCalls []string    // track async spawn tool names for fallback
	var mediaResults []MediaResult // media files from tool MEDIA: results

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

	for iteration < l.maxIterations {
		iteration++

		slog.Debug("agent iteration", "agent", l.id, "iteration", iteration, "messages", len(messages))

		// Build provider request with policy-filtered tools
		var toolDefs []providers.ToolDefinition
		if l.toolPolicy != nil {
			toolDefs = l.toolPolicy.FilterTools(l.tools, l.id, l.provider.Name(), l.agentToolPolicy, nil, false, false)
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
		var llmErr error

		llmSpanStart := time.Now().UTC()

		if req.Stream {
			resp, llmErr = l.provider.ChatStream(ctx, chatReq, func(chunk providers.StreamChunk) {
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
			resp, llmErr = l.provider.Chat(ctx, chatReq)
		}

		if llmErr != nil {
			l.emitLLMSpan(ctx, llmSpanStart, iteration, messages, nil, llmErr)
			return nil, fmt.Errorf("LLM call failed (iteration %d): %w", iteration, llmErr)
		}

		l.emitLLMSpan(ctx, llmSpanStart, iteration, messages, resp, nil)

		if resp.Usage != nil {
			totalUsage.PromptTokens += resp.Usage.PromptTokens
			totalUsage.CompletionTokens += resp.Usage.CompletionTokens
			totalUsage.TotalTokens += resp.Usage.TotalTokens
			totalUsage.ThinkingTokens += resp.Usage.ThinkingTokens
		}

		// No tool calls → done
		if len(resp.ToolCalls) == 0 {
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

		// Execute tool calls and collect results
		var toolResults []toolExecResult
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

			spanStart := time.Now().UTC()
			result := l.tools.ExecuteWithContext(ctx, tc.Name, tc.Arguments, req.Channel, req.ChatID, req.PeerKind, req.SessionKey, nil)
			toolResults = []toolExecResult{{tc: tc, result: result, argsJSON: string(argsJSON), spanStart: spanStart}}
		} else {
			// Multiple tools: emit all tool.call events upfront, then execute in parallel
			for _, tc := range resp.ToolCalls {
				l.emit(AgentEvent{
					Type:    protocol.AgentEventToolCall,
					AgentID: l.id,
					RunID:   req.RunID,
					Payload: map[string]interface{}{"name": tc.Name, "id": tc.ID},
				})
			}
			toolResults = l.executeToolsParallel(ctx, resp.ToolCalls, req)
		}

		// Process all tool results (unified path for single and parallel)
		var loopStuck bool
		for _, r := range toolResults {
			toolMsg, level, loopMsg := l.processToolResult(ctx, r, req.RunID, &loopDetector, &asyncToolCalls, &mediaResults)
			messages = append(messages, toolMsg)
			pendingMsgs = append(pendingMsgs, toolMsg)

			if level == "critical" {
				finalContent = "I was unable to complete this task \u2014 I got stuck repeatedly calling " + r.tc.Name + " without making progress. Please try rephrasing your request."
				loopStuck = true
				break
			}
			if level == "warning" {
				messages = append(messages, providers.Message{Role: "user", Content: loopMsg})
			}
		}
		if loopStuck {
			break
		}
	}

	// 6. Full sanitization pipeline (matching TS extractAssistantText + sanitizeUserFacingText)
	finalContent = SanitizeAssistantContent(finalContent)

	// 7. Handle NO_REPLY: save to session for context but mark as silent.
	// Matching TS: NO_REPLY is saved (via resolveSilentReplyFallbackText) but
	// filtered at the payload level before delivery.
	isSilent := IsSilentReply(finalContent)

	// 8. Fallback for empty content
	if finalContent == "" {
		finalContent = "..."
	}

	pendingMsgs = append(pendingMsgs, providers.Message{
		Role:    "assistant",
		Content: finalContent,
	})

	// 9. Persist run: flush messages, update metadata, bootstrap cleanup
	l.persistRun(ctx, req, pendingMsgs, history, hadBootstrap, totalUsage)

	// If silent, return empty content so gateway suppresses delivery.
	if isSilent {
		slog.Info("agent loop: NO_REPLY detected, suppressing delivery",
			"agent", l.id, "session", req.SessionKey)
		finalContent = ""
	}

	// 10. Maybe summarize
	l.maybeSummarize(ctx, req.SessionKey)

	return &RunResult{
		Content:    finalContent,
		RunID:      req.RunID,
		Iterations: iteration,
		Usage:      &totalUsage,
		Media:      mediaResults,
	}, nil
}

// prepareContext injects per-request values into the context and performs
// pre-processing: workspace setup, user seeding, input guard, message truncation.
// Returns the enriched context, possibly modified request, and any blocking error.
func (l *Loop) prepareContext(ctx context.Context, req RunRequest) (context.Context, RunRequest, error) {
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
	// Each user gets a subdirectory within the agent's workspace.
	if l.workspace != "" {
		effectiveWorkspace := l.workspace
		if req.UserID != "" {
			effectiveWorkspace = filepath.Join(l.workspace, sanitizePathSegment(req.UserID))
			if err := os.MkdirAll(effectiveWorkspace, 0755); err != nil {
				slog.Warn("failed to create user workspace directory", "workspace", effectiveWorkspace, "user", req.UserID, "error", err)
			}
		}
		ctx = tools.WithToolWorkspace(ctx, effectiveWorkspace)
	}

	// Ensure per-user context files exist (first-chat seeding, managed mode)
	if l.ensureUserFiles != nil && req.UserID != "" {
		if _, loaded := l.seededUsers.LoadOrStore(req.UserID, true); !loaded {
			if err := l.ensureUserFiles(ctx, l.agentUUID, req.UserID, l.agentType, l.workspace); err != nil {
				slog.Warn("failed to ensure user context files", "error", err)
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
				return ctx, req, fmt.Errorf("message blocked: potential prompt injection detected (%s)", matchStr)
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

	// Cache agent's context window on the session (first run only).
	// Enables scheduler's adaptive throttle to use the real value instead of hardcoded 200K.
	if l.sessions.GetContextWindow(req.SessionKey) <= 0 {
		l.sessions.SetContextWindow(req.SessionKey, l.contextWindow)
	}

	return ctx, req, nil
}

// toolExecResult holds one tool execution outcome (used for both single and parallel paths).
type toolExecResult struct {
	tc        providers.ToolCall
	result    *tools.Result
	argsJSON  string
	spanStart time.Time
}

// processToolResult handles the common per-tool-result work:
// emit tracing span + result event, record loop detection, log errors, collect media.
// Returns the tool message and any loop detection (level, message).
func (l *Loop) processToolResult(
	ctx context.Context,
	r toolExecResult,
	runID string,
	loopDetector *toolLoopState,
	asyncToolCalls *[]string,
	mediaResults *[]MediaResult,
) (providers.Message, string, string) {
	l.emitToolSpan(ctx, r.spanStart, r.tc.Name, r.tc.ID, r.argsJSON, r.result)

	// Record for loop detection.
	argsHash := loopDetector.record(r.tc.Name, r.tc.Arguments)
	loopDetector.recordResult(argsHash, r.result.ForLLM)

	if r.result.Async {
		*asyncToolCalls = append(*asyncToolCalls, r.tc.Name)
	}

	if r.result.IsError {
		errMsg := r.result.ForLLM
		if len(errMsg) > 200 {
			errMsg = errMsg[:200] + "..."
		}
		slog.Warn("tool error", "agent", l.id, "tool", r.tc.Name, "error", errMsg)
	}

	l.emit(AgentEvent{
		Type:    protocol.AgentEventToolResult,
		AgentID: l.id,
		RunID:   runID,
		Payload: map[string]interface{}{
			"name":     r.tc.Name,
			"id":       r.tc.ID,
			"is_error": r.result.IsError,
		},
	})

	// Collect MEDIA: paths from tool results
	if mr := parseMediaResult(r.result.ForLLM); mr != nil {
		*mediaResults = append(*mediaResults, *mr)
	}

	toolMsg := providers.Message{
		Role:       "tool",
		Content:    r.result.ForLLM,
		ToolCallID: r.tc.ID,
	}

	// Check for tool call loop.
	level, loopMsg := loopDetector.detect(r.tc.Name, argsHash)
	if level == "critical" {
		slog.Warn("tool loop critical", "agent", l.id, "tool", r.tc.Name, "message", loopMsg)
	} else if level == "warning" {
		slog.Warn("tool loop warning", "agent", l.id, "tool", r.tc.Name, "message", loopMsg)
	}

	return toolMsg, level, loopMsg
}

// executeToolsParallel runs multiple tool calls concurrently via goroutines.
// Tool instances are immutable (context-based) so concurrent access is safe.
// Results are collected then sorted by original index for deterministic ordering.
func (l *Loop) executeToolsParallel(ctx context.Context, toolCalls []providers.ToolCall, req RunRequest) []toolExecResult {
	type indexedResult struct {
		idx int
		toolExecResult
	}

	resultCh := make(chan indexedResult, len(toolCalls))
	var wg sync.WaitGroup

	for i, tc := range toolCalls {
		wg.Add(1)
		go func(idx int, tc providers.ToolCall) {
			defer wg.Done()
			argsJSON, _ := json.Marshal(tc.Arguments)
			slog.Info("tool call", "agent", l.id, "tool", tc.Name, "args_len", len(argsJSON), "parallel", true)
			spanStart := time.Now().UTC()
			result := l.tools.ExecuteWithContext(ctx, tc.Name, tc.Arguments, req.Channel, req.ChatID, req.PeerKind, req.SessionKey, nil)
			resultCh <- indexedResult{idx: idx, toolExecResult: toolExecResult{tc: tc, result: result, argsJSON: string(argsJSON), spanStart: spanStart}}
		}(i, tc)
	}

	// Close channel after all goroutines complete (run in separate goroutine to avoid deadlock)
	go func() { wg.Wait(); close(resultCh) }()

	// Collect results
	collected := make([]indexedResult, 0, len(toolCalls))
	for r := range resultCh {
		collected = append(collected, r)
	}

	// Sort by original index → deterministic message ordering
	sort.Slice(collected, func(i, j int) bool {
		return collected[i].idx < collected[j].idx
	})

	// Extract results in order
	results := make([]toolExecResult, len(collected))
	for i, c := range collected {
		results[i] = c.toolExecResult
	}
	return results
}

// persistRun flushes buffered messages to the session store, updates metadata,
// calibrates token estimation, and handles bootstrap auto-cleanup.
func (l *Loop) persistRun(ctx context.Context, req RunRequest, pendingMsgs, history []providers.Message, hadBootstrap bool, totalUsage providers.Usage) {
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
