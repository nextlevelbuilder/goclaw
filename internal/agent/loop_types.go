package agent

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/media"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// bootstrapAutoCleanupTurns is the number of user messages after which
// BOOTSTRAP.md is auto-removed if the LLM hasn't cleared it.
// Bootstrap typically completes in 2-3 conversation turns.
const bootstrapAutoCleanupTurns = 3

// EnsureUserFilesFunc seeds per-user context files on first chat.
// Returns the effective workspace path (from user_agent_profiles) for caching.
type EnsureUserFilesFunc func(ctx context.Context, agentID uuid.UUID, userID, agentType, workspace, channel string) (effectiveWorkspace string, err error)

// ContextFileLoaderFunc loads context files dynamically per-request.
type ContextFileLoaderFunc func(ctx context.Context, agentID uuid.UUID, userID, agentType string) []bootstrap.ContextFile

// BootstrapCleanupFunc removes BOOTSTRAP.md after a successful first run.
// Called automatically so the system doesn't rely on the LLM to delete it.
type BootstrapCleanupFunc func(ctx context.Context, agentID uuid.UUID, userID string) error

// Loop is the agent execution loop for one agent instance.
// Think → Act → Observe cycle with tool execution.
type Loop struct {
	id            string
	agentUUID     uuid.UUID // set for context propagation
	agentType     string    // "open" or "predefined"
	provider      providers.Provider
	model         string
	contextWindow int
	maxTokens     int // max output tokens per LLM call (0 = default 8192)
	maxIterations int
	maxToolCalls  int
	workspace        string
	workspaceSharing *store.WorkspaceSharingConfig

	// Per-agent overrides from DB (nil = use global defaults)
	restrictToWs  *bool
	subagentsCfg  *config.SubagentsConfig
	memoryCfg     *config.MemoryConfig
	sandboxCfg    *sandbox.Config

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

	// Per-user file seeding + dynamic context loading
	ensureUserFiles   EnsureUserFilesFunc
	contextFileLoader ContextFileLoaderFunc
	bootstrapCleanup  BootstrapCleanupFunc
	userWorkspaces    sync.Map // userID → string (expanded workspace path from user_agent_profiles)

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

	// Tracing collector (nil if not configured)
	traceCollector *tracing.Collector

	// Security: input scanning and message size limit
	inputGuard      *InputGuard
	injectionAction string // "log", "warn" (default), "block", "off"
	maxMessageChars int    // 0 = use default (32000)

	// Global builtin tool settings (from builtin_tools table)
	builtinToolSettings tools.BuiltinToolSettings

	// Thinking level for extended thinking support
	thinkingLevel string

	// Self-evolve: predefined agents can update SOUL.md through chat
	selfEvolve bool

	// Group writer cache for system prompt injection
	groupWriterCache *store.GroupWriterCache

	// Team store for cross-session pending task detection
	teamStore store.TeamStore

	// Persistent media storage for cross-turn image/document access
	mediaStore *media.Store

	// Model pricing config for cost tracking (nil = no cost calculation)
	modelPricing map[string]*config.ModelPricing

	// Budget enforcement: monthly spending limit in cents (0 = unlimited)
	budgetMonthlyCents int
	tracingStore       store.TracingStore
}

// AgentEvent is emitted during agent execution for WS broadcasting.
type AgentEvent struct {
	Type    string `json:"type"` // "run.started", "run.completed", "run.failed", "chunk", "tool.call", "tool.result"
	AgentID string `json:"agentId"`
	RunID   string `json:"runId"`
	RunKind string `json:"runKind,omitempty"` // "delegation", "announce" — omitted for user-initiated runs
	Payload any    `json:"payload,omitempty"`

	// Delegation context (omitempty — only present when agent runs inside a delegation)
	DelegationID  string `json:"delegationId,omitempty"`
	TeamID        string `json:"teamId,omitempty"`
	TeamTaskID    string `json:"teamTaskId,omitempty"`
	ParentAgentID string `json:"parentAgentId,omitempty"`

	// Routing context (helps WS clients filter by user/channel)
	UserID  string `json:"userId,omitempty"`
	Channel string `json:"channel,omitempty"`
	ChatID  string `json:"chatId,omitempty"`
}

// LoopConfig configures a new Loop.
type LoopConfig struct {
	ID              string
	Provider        providers.Provider
	Model           string
	ContextWindow   int
	MaxTokens       int // max output tokens per LLM call (0 = default 8192)
	MaxIterations   int
	MaxToolCalls    int
	Workspace        string
	WorkspaceSharing *store.WorkspaceSharingConfig

	// Per-agent DB overrides (nil = use global defaults)
	RestrictToWs *bool
	SubagentsCfg *config.SubagentsConfig
	MemoryCfg    *config.MemoryConfig
	SandboxCfg   *sandbox.Config

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

	// Agent UUID for context propagation to tools
	AgentUUID uuid.UUID
	AgentType string // "open" or "predefined"

	// Per-user file seeding + dynamic context loading
	EnsureUserFiles   EnsureUserFilesFunc
	ContextFileLoader ContextFileLoaderFunc
	BootstrapCleanup  BootstrapCleanupFunc

	// Tracing collector (nil = no tracing)
	TraceCollector *tracing.Collector

	// Security: input guard for injection detection, max message size
	InputGuard      *InputGuard // nil = auto-create when InjectionAction != "off"
	InjectionAction string      // "log", "warn" (default), "block", "off"
	MaxMessageChars int         // 0 = use default (32000)

	// Global builtin tool settings (from builtin_tools table)
	BuiltinToolSettings tools.BuiltinToolSettings

	// Thinking level: "off", "low", "medium", "high" (from agent other_config)
	ThinkingLevel string

	// Self-evolve: predefined agents can update SOUL.md (style/tone) through chat
	SelfEvolve bool

	// Group writer cache for system prompt injection
	GroupWriterCache *store.GroupWriterCache

	// Team store for cross-session pending task detection
	TeamStore store.TeamStore

	// Persistent media storage for cross-turn image/document access
	MediaStore *media.Store

	// Model pricing for cost tracking (key = "provider/model" or "model")
	ModelPricing map[string]*config.ModelPricing

	// Budget enforcement
	BudgetMonthlyCents int
	TracingStore       store.TracingStore
}

const defaultMaxTokens = 8192

// effectiveMaxTokens returns the configured max output tokens, defaulting to 8192.
func (l *Loop) effectiveMaxTokens() int {
	if l.maxTokens > 0 {
		return l.maxTokens
	}
	return defaultMaxTokens
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
		maxTokens:              cfg.MaxTokens,
		maxIterations:          cfg.MaxIterations,
		maxToolCalls:           cfg.MaxToolCalls,
		workspace:              cfg.Workspace,
		workspaceSharing:       cfg.WorkspaceSharing,
		restrictToWs:           cfg.RestrictToWs,
		subagentsCfg:           cfg.SubagentsCfg,
		memoryCfg:              cfg.MemoryCfg,
		sandboxCfg:             cfg.SandboxCfg,
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
		selfEvolve:             cfg.SelfEvolve,
		groupWriterCache:       cfg.GroupWriterCache,
		teamStore:              cfg.TeamStore,
		mediaStore:             cfg.MediaStore,
		modelPricing:           cfg.ModelPricing,
		budgetMonthlyCents:     cfg.BudgetMonthlyCents,
		tracingStore:           cfg.TracingStore,
	}
}

// RunRequest is the input for processing a message through the agent.
type RunRequest struct {
	SessionKey        string          // composite key: agent:{agentId}:{channel}:{peerKind}:{chatId}
	Message           string          // user message
	Media             []bus.MediaFile // local media files with MIME types
	ForwardMedia      []bus.MediaFile // media files to forward to output (from delegation results)
	Channel           string          // source channel instance name (e.g. "my-telegram-bot")
	ChannelType       string          // platform type (e.g. "zalo_personal", "telegram") — for system prompt context
	ChatID            string          // source chat ID
	PeerKind          string          // "direct" or "group" (for session key building and tool context)
	RunID             string          // unique run identifier
	UserID            string          // external user ID (TEXT, free-form) for multi-tenant scoping
	SenderID          string          // original individual sender ID (preserved in group chats for permission checks)
	Stream            bool            // whether to stream response chunks
	ExtraSystemPrompt string          // optional: injected into system prompt (skills, subagent context, etc.)
	SkillFilter       []string        // per-request skill override: nil=use agent default, []=no skills, ["x","y"]=whitelist
	HistoryLimit      int             // max user turns to keep in context (0=unlimited, from channel config)
	ToolAllow         []string        // per-group tool allow list (nil = no restriction, supports "group:xxx")
	LocalKey          string          // composite key with topic/thread suffix for routing (e.g. "-100123:topic:42")
	ParentTraceID     uuid.UUID       // if set, reuse parent trace instead of creating new (announce runs)
	ParentRootSpanID  uuid.UUID       // if set, nest announce agent span under this parent span
	LinkedTraceID     uuid.UUID       // if set, create new trace with parent_trace_id pointing to this (team task runs)
	TraceName         string          // override trace name (default: "chat <agentID>")
	TraceTags         []string        // additional tags for the trace (e.g. "cron")
	MaxIterations     int             // per-request override (0 = use agent default, must be lower)

	// Run classification
	RunKind       string // "delegation", "announce" — empty for user-initiated runs
	HideInput     bool   // don't persist input message in session history (announce runs)
	ContentSuffix string // appended to assistant response before saving (e.g. image markdown for WS)

	// Mid-run message injection channel (nil = disabled).
	// When set, the loop drains this channel at turn boundaries to inject
	// user follow-up messages into the running conversation.
	InjectCh <-chan InjectedMessage

	// Delegation context (set when running as a delegate agent)
	DelegationID  string // delegation ID for event correlation
	TeamID        string // team ID (if delegation is team-scoped)
	TeamTaskID    string // team task ID (if delegation has an associated task)
	ParentAgentID string // parent agent key that initiated the delegation

	// Workspace scope propagation (set by delegation, read by workspace tools)
	WorkspaceChannel string
	WorkspaceChatID  string
	// TeamWorkspace overrides the member agent's workspace with the team's workspace
	// so file operations (read/write/image/audio) use the shared team directory.
	TeamWorkspace string
}

// RunResult is the output of a completed agent run.
type RunResult struct {
	Content        string           `json:"content"`
	RunID          string           `json:"runId"`
	Iterations     int              `json:"iterations"`
	Usage          *providers.Usage `json:"usage,omitempty"`
	Media          []MediaResult    `json:"media,omitempty"`          // media files from tool results (MEDIA: prefix)
	Deliverables   []string         `json:"deliverables,omitempty"`   // actual content from tool outputs (for team task results)
	BlockReplies   int              `json:"blockReplies,omitempty"`   // number of block.reply events emitted
	LastBlockReply string           `json:"lastBlockReply,omitempty"` // last block reply content (for dedup)
}

// MediaResult represents a media file produced by a tool during the agent run.
type MediaResult struct {
	Path        string `json:"path"`                   // local file path
	ContentType string `json:"content_type,omitempty"` // MIME type
	AsVoice     bool   `json:"as_voice,omitempty"`     // send as voice message (Telegram OGG)
}
