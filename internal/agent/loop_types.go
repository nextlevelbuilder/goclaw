package agent

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	mcpbridge "github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/media"
	"github.com/nextlevelbuilder/goclaw/internal/memory"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/secret"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tokencount"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// bootstrapAutoCleanupTurns is the number of user messages after which
// BOOTSTRAP.md is auto-removed if the LLM hasn't cleared it.
// Bootstrap typically completes in 2-3 conversation turns.
const bootstrapAutoCleanupTurns = 3

// userSetup tracks per-user initialization state within a Loop instance.
// Consolidates workspace resolution and context file seeding into one struct
// to prevent desync between the two concerns.
type userSetup struct {
	workspace         string                  // effective workspace from user_agent_profiles (expanded, absolute)
	seeded            bool                    // whether SeedUserFiles has been called this instance
	fallbackBootstrap []bootstrap.ContextFile // in-memory fallback when DB seed fails (e.g. SQLITE_BUSY)
}

// EnsureUserProfileFunc creates/resolves a user's profile and workspace.
// Returns the effective workspace path from user_agent_profiles.
// Does NOT seed context files — that's SeedUserFilesFunc's responsibility.
type EnsureUserProfileFunc func(ctx context.Context, agentID uuid.UUID, userID, workspace, channel string) (effectiveWorkspace string, isNew bool, err error)

// SeedUserFilesFunc seeds per-user context files (BOOTSTRAP.md, USER.md, etc.).
// Called once per user per Loop instance, independent of workspace.
// isNew indicates whether the profile was just created (seed all) or already existed
// (only seed if user has zero files — avoids re-seeding after BOOTSTRAP.md cleanup).
// channelMeta carries optional channel-provided contact info for bootstrap skip decisions.
type SeedUserFilesFunc func(ctx context.Context, agentID uuid.UUID, userID, agentType string, isNew bool, channelMeta *bootstrap.ChannelMeta) error

// EnsureUserFilesFunc is the legacy combined callback (profile + seed + workspace).
// Deprecated: use EnsureUserProfileFunc + SeedUserFilesFunc separately.
// Kept for backward compatibility with existing callers during migration.
type EnsureUserFilesFunc func(ctx context.Context, agentID uuid.UUID, userID, agentType, workspace, channel string) (effectiveWorkspace string, err error)

// ContextFileLoaderFunc loads context files dynamically per-request.
type ContextFileLoaderFunc func(ctx context.Context, agentID uuid.UUID, userID, agentType string) []bootstrap.ContextFile

// BootstrapCleanupFunc removes BOOTSTRAP.md after a successful first run.
// Called automatically so the system doesn't rely on the LLM to delete it.
type BootstrapCleanupFunc func(ctx context.Context, agentID uuid.UUID, userID string) error

// CacheInvalidateFunc invalidates the context file cache for a user after seeding.
// SeedUserFiles writes via raw agentStore (bypassing ContextFileInterceptor cache),
// so this callback ensures LoadContextFiles sees the newly seeded files.
type CacheInvalidateFunc func(agentID uuid.UUID, userID string)

// Loop is the agent execution loop for one agent instance.
// Think → Act → Observe cycle with tool execution.
type Loop struct {
	// channelInstanceStore lets the loop enumerate active channel_instances
	// at prompt-build time so the agent knows which delivery targets are
	// already wired (used by buildConnectedChannelsSection). Nil-safe.
	channelInstanceStore store.ChannelInstanceStore

	// id is the human-readable agent_key (e.g. "goctech-leader"). Use for logs,
	// UI events, system prompt rendering, filesystem paths, and context keys.
	// NEVER set on DB FK columns or DomainEvent.AgentID — those require UUID.
	// See docs/agent-identity-conventions.md.
	id          string
	displayName string
	// agentUUID is the canonical DB primary key. Use for SQL WHERE/JOIN,
	// DomainEvent.AgentID, OTel span attributes, and context propagation via
	// store.WithAgentID. See docs/agent-identity-conventions.md.
	agentUUID  uuid.UUID
	tenantID   uuid.UUID // agent's owning tenant
	tenantSlug string    // canonical tenant slug — see Config.TenantSlug
	// externalOrgID is the web-backend organizations.id (UUID) for this
	// tenant, sourced from tenants.settings.external_org_id. When non-empty
	// it overrides tenantSlug in outbound X-Actor-Org-ID headers, removing
	// the slug-format coupling between goclaw and downstream attribution.
	// Empty during rollout for tenants the auth-proxy hasn't stamped yet —
	// loop_context.go falls back to the slug in that case.
	externalOrgID string
	// agentOtherConfig is a defensive byte copy of agents.other_config JSONB.
	// Copied once at Loop construction; used to build AgentAudioSnapshot at tool dispatch.
	agentOtherConfig json.RawMessage
	agentType        string // "open" or "predefined"
	// customInstructions holds the agent's agents.system_prompt (migration 000063).
	// Empty for the tenant default agent. Threaded into SystemPromptConfig
	// on every BuildSystemPrompt call from loop_history.go.
	customInstructions string
	// isLocked mirrors agents.is_locked; threaded into SystemPromptConfig
	// so BuildSystemPrompt can decide whether to inject the locked-agent
	// preamble (lockedAgentPreamble const).
	isLocked         bool
	defaultTimezone  string // system default timezone for bootstrap pre-fill
	provider         providers.Provider
	model            string
	modelRegistry    providers.ModelRegistry // resolves per-model context window at run time (nil = use static contextWindow)
	contextWindow    int
	maxTokens        int // max output tokens per LLM call (0 = default 8192)
	maxIterations    int
	maxToolCalls     int
	workspace        string
	dataDir          string // global workspace root for team workspace resolution
	workspaceSharing *store.WorkspaceSharingConfig

	// Per-agent overrides from DB (nil = use global defaults)
	restrictToWs *bool
	subagentsCfg *config.SubagentsConfig
	memoryCfg    *config.MemoryConfig
	sandboxCfg   *sandbox.Config

	// subagentMgr is the in-process SubagentManager so Loop.Run can drain
	// spawned children before emitting run.completed. Nil-safe: when the
	// manager isn't wired (older configurations, lite edition) the run still
	// completes normally and the legacy announce queue handles the async
	// fan-out fallback.
	subagentMgr *tools.SubagentManager

	// v3 memory/retrieval flags removed — always true at runtime.
	// Memory flush runs if callback != nil; auto-inject runs if AutoInjector != nil.
	autoInjector memory.AutoInjector // v3 L0 memory auto-inject (nil = disabled)

	eventPub        bus.EventPublisher      // currently unused by Loop; kept for future use
	domainBus       eventbus.DomainEventBus // V3 domain event bus for consolidation pipeline
	sessions        store.SessionStore
	tools           tools.ToolExecutor
	registry        *tools.Registry        // direct registry access for MergeToolGroup (per-Registry tool groups)
	toolPolicy      *tools.PolicyEngine    // optional: filters tools sent to LLM
	agentToolPolicy *config.ToolPolicySpec // per-agent tool policy from DB (nil = no restrictions)
	activeRuns      atomic.Int32           // number of currently executing runs

	// Per-session summarization lock: prevents concurrent summarize goroutines for the same session.
	summarizeMu sync.Map // sessionKey → *sync.Mutex

	// Bootstrap/persona context (loaded at startup, injected into system prompt)
	ownerIDs       []string
	skillsLoader   *skills.Loader
	skillAllowList []string // nil = all, [] = none, ["x","y"] = filter
	// skillAccess sources per-tenant skills (DB) for the prompt summary. The
	// loader is filesystem-only and never sees DB-stored / per-tenant skills,
	// so when this is set the summary is built from ListAccessible instead.
	skillAccess  store.SkillAccessStore
	hasMemory    bool
	contextFiles []bootstrap.ContextFile

	// Per-user profile + file seeding + dynamic context loading
	ensureUserProfile EnsureUserProfileFunc // create/resolve user profile + workspace
	seedUserFiles     SeedUserFilesFunc     // seed context files (BOOTSTRAP.md, USER.md)
	ensureUserFiles   EnsureUserFilesFunc   // legacy combined callback (fallback)
	contextFileLoader ContextFileLoaderFunc
	bootstrapCleanup  BootstrapCleanupFunc
	cacheInvalidate   CacheInvalidateFunc // invalidate context file cache after seeding
	userSetups        sync.Map            // userID → *userSetup (workspace + seeding state, per Loop instance)

	// Per-user MCP tools: servers requiring user credentials get connected per-request.
	mcpStore        store.MCPServerStore  // for credential lookup
	mcpPool         *mcpbridge.Pool       // user-keyed connection pool
	mcpUserCredSrvs []store.MCPAccessInfo // servers needing per-user creds
	mcpUserTools    sync.Map              // userID → []tools.Tool (legacy cache, kept until callers migrate)
	// mcpServerToolNames maps mcp_server.id → []registeredToolName. Used to
	// know whether the BridgeTools for a given user-cred server are already
	// installed in the shared registry, so getUserMCPTools doesn't pay the
	// bootstrap connect cost on every turn. Tool names themselves are stable
	// across users — the per-user bit is the underlying HTTP connection,
	// resolved per-call by BridgeTool.WithResolveClient.
	mcpServerToolNames sync.Map               // uuid.UUID → []string
	mcpGrantChecker    mcpbridge.GrantChecker // runtime grant verification (nil = skip)

	// Compaction config (memory flush settings)
	compactionCfg *config.CompactionConfig

	// Context pruning config (trim old tool results in-memory)
	contextPruningCfg *config.ContextPruningConfig

	// tokenCounter provides accurate per-model token counting for context pruning.
	// Nil means the legacy char-based heuristic is used.
	tokenCounter tokencount.TokenCounter

	// Sandbox info
	sandboxEnabled         bool
	sandboxContainerDir    string
	sandboxWorkspaceAccess string

	// Shell deny group overrides from agent other_config (nil = all defaults)
	shellDenyGroups map[string]bool

	// Event callback for broadcasting agent events (run.started, chunk, tool.call, etc.)
	onEvent func(event AgentEvent)

	// Tracing collector (nil if not configured)
	traceCollector *tracing.Collector

	// Security: input scanning and message size limit
	inputGuard      *InputGuard
	injectionAction string // "log", "warn" (default), "block", "off"
	maxMessageChars int    // 0 = use default (32000)

	// Global builtin tool settings (from builtin_tools.settings table).
	// Tier 3 in the overlay — tenant (tier 2) and future per-agent (tier 1) sit above.
	builtinToolSettings tools.BuiltinToolSettings

	// Tenant-layer tool settings overlay (from builtin_tool_tenant_configs.settings).
	// Tier 2 — sits above global (tier 3) and is merged at read time in
	// BuiltinToolSettingsFromCtx with global winning at tool-name level.
	tenantToolSettings tools.BuiltinToolSettings

	// Tenant-specific allowed paths beyond workspace (from system_configs['allowed_paths']).
	// Filesystem tools (read_file, write_file, edit, list_files) check these at execution time.
	tenantAllowedPaths []string

	// Per-tenant disabled tools (tool name → true means excluded from LLM)
	disabledTools map[string]bool

	// Requested reasoning config parsed from agent other_config.
	reasoningConfig store.AgentReasoningConfig

	// Prompt mode from agent other_config (empty = full).
	promptMode PromptMode

	// Pinned skills from agent other_config (always inline, max 10).
	pinnedSkills []string

	// Self-evolve: predefined agents can update SOUL.md through chat
	selfEvolve bool

	// TTS auto mode from config: "off", "always", "inbound", "tagged"
	ttsAutoMode string

	// Skill learning loop: when skillEvolve=true, the loop injects nudges reminding
	// the agent to capture reusable patterns as skills via skill_manage.
	skillEvolve        bool
	skillNudgeInterval int // nudge every N tool calls (0 = disabled, 15 = default)

	// isTeamLead indicates this agent is the lead of its primary team.
	// Determines whether team context is injected for inbound (non-dispatch) sessions.
	isTeamLead bool

	// Config permission store for group file writer checks
	configPermStore store.ConfigPermissionStore

	// Team store for cross-session pending task detection
	teamStore store.TeamStore

	// Secure CLI store for credentialed exec context injection
	secureCLIStore store.SecureCLIStore

	// Vault hook: called when a text file is persisted from user upload.
	// Enables vault registration without agent package importing vault.
	onTextUploaded func(ctx context.Context, path, content string)

	// Persistent media storage for cross-turn image/document access
	mediaStore *media.Store

	// Model pricing config for cost tracking (nil = no cost calculation)
	modelPricing map[string]*config.ModelPricing

	// Budget enforcement: monthly spending limit in cents (0 = unlimited)
	budgetMonthlyCents int
	tracingStore       store.TracingStore

	// Memory store for extractive memory fallback (writes directly when LLM flush fails)
	memStore store.MemoryStore

	// v3 orchestration mode (spawn/delegate/team) — controls tool visibility
	orchMode        OrchestrationMode
	delegateTargets []DelegateTargetEntry   // delegation targets for prompt injection
	connectedAgents []ConnectedAgentSummary // connected external agents (delegate_external) for prompt awareness

	// v3 evolution metrics store (nil = disabled)
	evolutionMetricsStore store.EvolutionMetricsStore

	// User identity resolver: maps channel contacts to merged tenant users for credential lookups.
	userResolver UserIdentityResolver

	// Per-session cache-touch timestamps for the cache-TTL pruning gate (Phase 06).
	// Key: sessionKey (string), Value: time.Time of last prune mutation.
	// sync.Map zero value is ready to use — no init required.
	// Grows with distinct sessions; typical gateway has bounded session count.
	// Note: in-memory only — timestamps reset on process restart (one extra prune
	// per session on restart, then steady-state resumes).
	cacheTouchBySession sync.Map

	// hookDispatcher fires lifecycle hook events (Issue #875). Nil-safe: when
	// nil the pipeline fast-path skips all hook overhead. Populated from
	// LoopConfig.HookDispatcher during startup wiring.
	hookDispatcher hooks.Dispatcher
}

// AgentEvent is emitted during agent execution for WS broadcasting.
type AgentEvent struct {
	Type    string `json:"type"` // "run.started", "run.completed", "run.failed", "run.cancelled", "chunk", "tool.call", "tool.result"
	AgentID string `json:"agentId"`
	RunID   string `json:"runId"`
	RunKind string `json:"runKind,omitempty"` // "delegation", "announce" — omitted for user-initiated runs
	Payload any    `json:"payload,omitempty"`
	// Seq is the monotonic per-run sequence number assigned by the router
	// at emit time (see Router.StampAndBufferEvent). The client stores the
	// last seq it received and asks for events.since(N) on reconnect via
	// the runs.subscribe method, so the UI can heal a partial stream
	// without falling back to a full sessions.preview reload. Zero when
	// the event was emitted on a code path that bypasses the router
	// (legacy / tests / channel handlers).
	Seq int64 `json:"seq,omitempty"`

	// Delegation context (omitempty — only present when agent runs inside a delegation)
	DelegationID  string `json:"delegationId,omitempty"`
	TeamID        string `json:"teamId,omitempty"`
	TeamTaskID    string `json:"teamTaskId,omitempty"`
	ParentAgentID string `json:"parentAgentId,omitempty"`

	// Routing context (helps WS clients filter by user/channel/session)
	UserID     string `json:"userId,omitempty"`
	Channel    string `json:"channel,omitempty"`
	ChatID     string `json:"chatId,omitempty"`
	SessionKey string `json:"sessionKey,omitempty"`

	// TenantID scopes this event to a specific tenant for filtering (not serialized).
	TenantID uuid.UUID `json:"-"`
}

// LoopConfig configures a new Loop.
type LoopConfig struct {
	ID                   string
	Provider             providers.Provider
	Model                string
	ContextWindow        int
	MaxTokens            int // max output tokens per LLM call (0 = default 8192)
	MaxIterations        int
	MaxToolCalls         int
	Workspace            string
	DataDir              string // global workspace root for team workspace resolution
	WorkspaceSharing     *store.WorkspaceSharingConfig
	ChannelInstanceStore store.ChannelInstanceStore

	// v3 memory/retrieval flags removed — always true at runtime.
	AutoInjector memory.AutoInjector // v3 L0 memory auto-inject (nil = disabled)

	// Per-agent DB overrides (nil = use global defaults)
	RestrictToWs *bool
	SubagentsCfg *config.SubagentsConfig
	MemoryCfg    *config.MemoryConfig
	SandboxCfg   *sandbox.Config

	// SubagentMgr is required when the pre-finalize barrier is in use
	// (BarrierMode on the manager). When nil the loop skips the barrier
	// entirely and falls back to the legacy announce queue path.
	SubagentMgr *tools.SubagentManager

	// ModelRegistry resolves provider/model → ModelSpec for per-run context
	// window lookup. Nil = fall back to static LoopConfig.ContextWindow.
	ModelRegistry providers.ModelRegistry

	Bus             bus.EventPublisher
	DomainBus       eventbus.DomainEventBus // V3 domain event bus for consolidation pipeline
	HookDispatcher  hooks.Dispatcher        // lifecycle hook dispatcher (nil = noop)
	Sessions        store.SessionStore
	Tools           *tools.Registry
	ToolPolicy      *tools.PolicyEngine    // optional: filters tools sent to LLM
	AgentToolPolicy *config.ToolPolicySpec // per-agent tool policy from DB (nil = no restrictions)
	OnEvent         func(AgentEvent)

	// Bootstrap/persona context
	OwnerIDs       []string
	SkillsLoader   *skills.Loader
	SkillAllowList []string // nil = all, [] = none, ["x","y"] = filter
	// SkillAccessStore sources per-tenant skills (DB) for the prompt summary.
	SkillAccessStore store.SkillAccessStore
	HasMemory        bool
	ContextFiles     []bootstrap.ContextFile

	// Compaction config
	CompactionCfg *config.CompactionConfig

	// Context pruning (trim old tool results to save context window)
	ContextPruningCfg *config.ContextPruningConfig

	// Sandbox info (injected into system prompt)
	SandboxEnabled         bool
	SandboxContainerDir    string // e.g. "/workspace"
	SandboxWorkspaceAccess string // "none", "ro", "rw"

	// Shell deny group overrides (nil = all defaults)
	ShellDenyGroups map[string]bool

	// Agent UUID + tenant for context propagation to tools
	AgentUUID        uuid.UUID
	TenantID         uuid.UUID       // agent's owning tenant — injected into execution context
	TenantSlug       string          // canonical tenant slug, e.g. "org-team-acme"; pre-resolved at construction so the loop has a stable identifier for downstream service-token calls when the per-request context doesn't carry one (Telegram path).
	ExternalOrgID    string          // web-backend organizations.id (UUID), stamped onto tenants.settings.external_org_id by auth-proxy. Preferred over TenantSlug for outbound X-Actor-Org-ID; empty until first login after the stamp lands.
	AgentOtherConfig json.RawMessage // raw other_config JSONB — copied defensively in NewLoop
	AgentType        string          // "open" or "predefined"
	DisplayName      string          // human-readable agent display name (for runtime section)
	IsTeamLead       bool            // agent leads a team (from resolver detection)
	// CustomInstructions is the agent's own configured system prompt
	// (agents.system_prompt column from migration 000063). Threaded
	// through to BuildSystemPrompt so it's injected near the top of the
	// generated prompt. Empty falls through to the default behaviour.
	CustomInstructions string
	// IsLocked passes through from agents.is_locked. See Loop.isLocked.
	IsLocked bool

	// Per-user profile + file seeding + dynamic context loading
	EnsureUserProfile EnsureUserProfileFunc // preferred: separate profile + workspace
	SeedUserFiles     SeedUserFilesFunc     // preferred: separate context file seeding
	EnsureUserFiles   EnsureUserFilesFunc   // legacy: combined (used when above are nil)
	ContextFileLoader ContextFileLoaderFunc
	BootstrapCleanup  BootstrapCleanupFunc
	CacheInvalidate   CacheInvalidateFunc // invalidate context file cache after seeding
	DefaultTimezone   string              // system default timezone for bootstrap pre-fill

	// Tracing collector (nil = no tracing)
	TraceCollector *tracing.Collector

	// Security: input guard for injection detection, max message size
	InputGuard      *InputGuard // nil = auto-create when InjectionAction != "off"
	InjectionAction string      // "log", "warn" (default), "block", "off"
	MaxMessageChars int         // 0 = use default (32000)

	// Global builtin tool settings (from builtin_tools table, merged with per-agent overrides)
	BuiltinToolSettings tools.BuiltinToolSettings

	// Tenant-layer tool settings overlay (from builtin_tool_tenant_configs.settings).
	TenantToolSettings tools.BuiltinToolSettings

	// Tenant-specific allowed paths beyond workspace (from system_configs['allowed_paths']).
	TenantAllowedPaths []string

	// Per-tenant disabled tools (tool name → true means excluded)
	DisabledTools map[string]bool

	// Requested reasoning config parsed from agent other_config.
	ReasoningConfig store.AgentReasoningConfig

	// Prompt mode from agent other_config ("full", "task", "minimal", "none")
	PromptMode PromptMode

	// Pinned skills from agent other_config (always inline, max 10)
	PinnedSkills []string

	// Self-evolve: predefined agents can update SOUL.md (style/tone) through chat
	SelfEvolve bool

	// TTS auto mode from config: "off", "always", "inbound", "tagged"
	// When "tagged", inject [[tts]] directive guidance into system prompt.
	TTSAutoMode string

	// Skill evolution: agent learning loop config (from other_config JSONB)
	SkillEvolve        bool
	SkillNudgeInterval int // 0 = disabled, 15 = default

	// Config permission store for group file writer checks
	ConfigPermStore store.ConfigPermissionStore

	// Team store for cross-session pending task detection
	TeamStore store.TeamStore

	// Secure CLI store for credentialed exec context injection
	SecureCLIStore store.SecureCLIStore

	// Vault hook: called asynchronously when a text file is persisted from user upload.
	OnTextUploaded func(ctx context.Context, path, content string)

	// Persistent media storage for cross-turn image/document access
	MediaStore *media.Store

	// Model pricing for cost tracking (key = "provider/model" or "model")
	ModelPricing map[string]*config.ModelPricing

	// Budget enforcement
	BudgetMonthlyCents int
	TracingStore       store.TracingStore

	// Memory store for extractive memory fallback (writes directly when LLM flush fails)
	MemoryStore store.MemoryStore

	// Per-user MCP tools (servers requiring per-user credentials)
	MCPStore        store.MCPServerStore   // for credential lookup
	MCPPool         *mcpbridge.Pool        // user-keyed connection pool
	MCPUserCredSrvs []store.MCPAccessInfo  // servers needing per-user creds
	MCPGrantChecker mcpbridge.GrantChecker // runtime grant verification (nil = skip)
	MCPSSMResolver  *secret.SSMResolver    // ${ssm:/path} expansion in headers (nil = passthrough)

	// V3 orchestration mode (resolved by resolver, controls tool visibility)
	OrchMode        OrchestrationMode
	DelegateTargets []DelegateTargetEntry   // delegation targets for prompt injection
	ConnectedAgents []ConnectedAgentSummary // connected external agents (delegate_external) for prompt awareness

	// V3 evolution metrics store for recording tool/retrieval/feedback metrics
	EvolutionMetricsStore store.EvolutionMetricsStore

	// User identity resolver for credential lookups (maps channel contacts → tenant users)
	UserResolver UserIdentityResolver
}

const defaultMaxTokens = config.DefaultMaxTokens

// effectiveMaxTokens returns the configured max output tokens, defaulting to 8192.
func (l *Loop) effectiveMaxTokens() int {
	if l.maxTokens > 0 {
		return l.maxTokens
	}
	return defaultMaxTokens
}

func NewLoop(cfg LoopConfig) *Loop {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = config.DefaultMaxIterations
	}
	if cfg.ContextWindow <= 0 {
		cfg.ContextWindow = config.DefaultContextWindow
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
		displayName:            cfg.DisplayName,
		agentUUID:              cfg.AgentUUID,
		tenantID:               cfg.TenantID,
		tenantSlug:             cfg.TenantSlug,
		externalOrgID:          cfg.ExternalOrgID,
		agentOtherConfig:       append([]byte(nil), cfg.AgentOtherConfig...), // defensive copy
		agentType:              cfg.AgentType,
		customInstructions:     cfg.CustomInstructions,
		isLocked:               cfg.IsLocked,
		provider:               cfg.Provider,
		model:                  cfg.Model,
		modelRegistry:          cfg.ModelRegistry,
		contextWindow:          cfg.ContextWindow,
		maxTokens:              cfg.MaxTokens,
		maxIterations:          cfg.MaxIterations,
		maxToolCalls:           cfg.MaxToolCalls,
		workspace:              cfg.Workspace,
		dataDir:                cfg.DataDir,
		workspaceSharing:       cfg.WorkspaceSharing,
		channelInstanceStore:   cfg.ChannelInstanceStore,
		autoInjector:           cfg.AutoInjector,
		restrictToWs:           cfg.RestrictToWs,
		subagentsCfg:           cfg.SubagentsCfg,
		subagentMgr:            cfg.SubagentMgr,
		memoryCfg:              cfg.MemoryCfg,
		sandboxCfg:             cfg.SandboxCfg,
		eventPub:               cfg.Bus,
		domainBus:              cfg.DomainBus,
		hookDispatcher:         cfg.HookDispatcher,
		sessions:               cfg.Sessions,
		tools:                  cfg.Tools,
		registry:               cfg.Tools,
		toolPolicy:             cfg.ToolPolicy,
		agentToolPolicy:        cfg.AgentToolPolicy,
		onEvent:                cfg.OnEvent,
		ownerIDs:               cfg.OwnerIDs,
		skillsLoader:           cfg.SkillsLoader,
		skillAllowList:         cfg.SkillAllowList,
		skillAccess:            cfg.SkillAccessStore,
		hasMemory:              cfg.HasMemory,
		contextFiles:           cfg.ContextFiles,
		defaultTimezone:        cfg.DefaultTimezone,
		ensureUserProfile:      cfg.EnsureUserProfile,
		seedUserFiles:          cfg.SeedUserFiles,
		ensureUserFiles:        cfg.EnsureUserFiles,
		contextFileLoader:      cfg.ContextFileLoader,
		bootstrapCleanup:       cfg.BootstrapCleanup,
		cacheInvalidate:        cfg.CacheInvalidate,
		compactionCfg:          cfg.CompactionCfg,
		contextPruningCfg:      cfg.ContextPruningCfg,
		tokenCounter:           tokencount.NewTiktokenCounter(),
		sandboxEnabled:         cfg.SandboxEnabled,
		sandboxContainerDir:    cfg.SandboxContainerDir,
		sandboxWorkspaceAccess: cfg.SandboxWorkspaceAccess,
		shellDenyGroups:        cfg.ShellDenyGroups,
		traceCollector:         cfg.TraceCollector,
		inputGuard:             guard,
		injectionAction:        action,
		maxMessageChars:        cfg.MaxMessageChars,
		builtinToolSettings:    cfg.BuiltinToolSettings,
		tenantToolSettings:     cfg.TenantToolSettings,
		tenantAllowedPaths:     cfg.TenantAllowedPaths,
		disabledTools:          cfg.DisabledTools,
		reasoningConfig:        cfg.ReasoningConfig,
		promptMode:             cfg.PromptMode,
		pinnedSkills:           cfg.PinnedSkills,
		selfEvolve:             cfg.SelfEvolve,
		ttsAutoMode:            cfg.TTSAutoMode,
		skillEvolve:            cfg.SkillEvolve,
		skillNudgeInterval:     cfg.SkillNudgeInterval,
		isTeamLead:             cfg.IsTeamLead,
		configPermStore:        cfg.ConfigPermStore,
		teamStore:              cfg.TeamStore,
		secureCLIStore:         cfg.SecureCLIStore,
		onTextUploaded:         cfg.OnTextUploaded,
		mediaStore:             cfg.MediaStore,
		modelPricing:           cfg.ModelPricing,
		budgetMonthlyCents:     cfg.BudgetMonthlyCents,
		tracingStore:           cfg.TracingStore,
		memStore:               cfg.MemoryStore,
		mcpStore:               cfg.MCPStore,
		mcpPool:                cfg.MCPPool,
		mcpUserCredSrvs:        cfg.MCPUserCredSrvs,
		mcpGrantChecker:        cfg.MCPGrantChecker,
		orchMode:               cfg.OrchMode,
		delegateTargets:        cfg.DelegateTargets,
		connectedAgents:        cfg.ConnectedAgents,
		evolutionMetricsStore:  cfg.EvolutionMetricsStore,
		userResolver:           cfg.UserResolver,
	}
}

// RunRequest is the input for processing a message through the agent.
type RunRequest struct {
	SessionKey        string             // composite key: agent:{agentId}:{channel}:{peerKind}:{chatId}
	Message           string             // user message
	Media             []bus.MediaFile    // local media files with MIME types
	ForwardMedia      []bus.MediaFile    // media files to forward to output (from delegation results)
	Channel           string             // source channel instance name (e.g. "my-telegram-bot")
	ChannelType       string             // platform type (e.g. "zalo_personal", "telegram") — for system prompt context
	ChatTitle         string             // group chat display name (e.g. Telegram group title)
	ChatID            string             // source chat ID
	PeerKind          string             // "direct" or "group" (for session key building and tool context)
	RunID             string             // unique run identifier
	UserID            string             // IDENTITY user ID (TEXT, free-form) for memory / session scope. May be a sender numeric (Telegram chat_id) when no merged contact is set. NOT used for billing.
	BillingUserID     string             // BILLING owner — channel_instance.created_by, always a real platform UUID. Used in actor headers (X-Actor-User-ID) so downstream attribution (ai_tasks.user_id, org quota) charges the bot owner. Falls back to UserID for in-app callers (extension chat) where there's no separate bot owner.
	SenderID          string             // original individual sender ID (preserved in group chats for permission checks)
	SenderName        string             // display name from channel metadata (for bootstrap auto-contact)
	Role              string             // caller's RBAC role (admin/operator/viewer/owner); bypasses per-user grants for authenticated admins (#915)
	Stream            bool               // whether to stream response chunks
	ExtraSystemPrompt string             // optional: injected into system prompt (skills, subagent context, etc.)
	SkillFilter       []string           // per-request skill override: nil=use agent default, []=no skills, ["x","y"]=whitelist
	HistoryLimit      int                // max user turns to keep in context (0=unlimited, from channel config)
	ToolAllow         []string           // per-group tool allow list (nil = no restriction, supports "group:xxx")
	LocalKey          string             // composite key with topic/thread suffix for routing (e.g. "-100123:topic:42")
	ParentTraceID     uuid.UUID          // if set, reuse parent trace instead of creating new (announce runs)
	ParentRootSpanID  uuid.UUID          // if set, nest announce agent span under this parent span
	LinkedTraceID     uuid.UUID          // if set, create new trace with parent_trace_id pointing to this (team task runs)
	TraceName         string             // override trace name (default: "chat <agentID>")
	TraceTags         []string           // additional tags for the trace (e.g. "cron")
	MaxIterations     int                // per-request override (0 = use agent default, must be lower)
	ModelOverride     string             // per-request model override (heartbeat uses cheaper model)
	ProviderOverride  providers.Provider // per-request provider override (heartbeat uses different provider)
	LightContext      bool               // skip loading context files (only inject ExtraSystemPrompt)

	// Run classification
	RunKind       string // "delegation", "announce" — empty for user-initiated runs
	HideInput     bool   // don't persist input message in session history (announce runs)
	ContentSuffix string // appended to assistant response before saving (e.g. image markdown for WS)

	// ClientKind identifies the WS caller for tool-palette tailoring.
	// "extension" keeps browser page-tools (execute_action, execute_js, navigate, …);
	// "website" or any other non-extension value strips them so the LLM doesn't
	// waste turns invoking client tools the website cannot service. Empty =
	// legacy behavior (no filtering) for non-WS channels and pre-flag callers.
	ClientKind string

	// Mid-run message injection channel (nil = disabled).
	// When set, the loop drains this channel at turn boundaries to inject
	// user follow-up messages into the running conversation.
	InjectCh <-chan InjectedMessage

	// OnTraceCreated is called once the trace UUID is determined for this run.
	// Used by the gateway to associate the trace ID with the active run entry
	// so force-abort can mark the correct trace as cancelled. Nil = no-op.
	OnTraceCreated func(traceID uuid.UUID)

	// Delegation context (set when running as a delegate agent)
	DelegationID  string // delegation ID for event correlation
	TeamID        string // team ID (if delegation is team-scoped)
	TeamTaskID    string // team task ID (if delegation has an associated task)
	ParentAgentID string // parent agent key that initiated the delegation
	LeaderAgentID string // leader agent UUID for member memory read fallback

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
	Thinking       string           `json:"thinking,omitempty"` // reasoning content from thinking models (Claude, o3, DeepSeek-R1, Kimi)
	RunID          string           `json:"runId"`
	Iterations     int              `json:"iterations"`
	Usage          *providers.Usage `json:"usage,omitempty"`
	Media          []MediaResult    `json:"media,omitempty"`          // media files from tool results (MEDIA: prefix)
	Deliverables   []string         `json:"deliverables,omitempty"`   // actual content from tool outputs (for team task results)
	BlockReplies   int              `json:"blockReplies,omitempty"`   // number of block.reply events emitted
	LastBlockReply string           `json:"lastBlockReply,omitempty"` // last block reply content (for dedup)
	LoopKilled     bool             `json:"loopKilled,omitempty"`     // true when run was terminated by loop detector
	// StopReason explains why the run ended when it's not a normal completion.
	// "max_steps" means the agent exhausted its per-run tool-iteration budget;
	// the client surfaces this as a "Continue" affordance. Empty = normal stop.
	StopReason string `json:"stopReason,omitempty"`
}

// MediaResult represents a media file produced by a tool during the agent run.
type MediaResult struct {
	Path        string `json:"path"`                   // local file path
	ContentType string `json:"content_type,omitempty"` // MIME type
	Size        int64  `json:"size,omitempty"`         // file size in bytes
	AsVoice     bool   `json:"as_voice,omitempty"`     // send as voice message (Telegram OGG)
	Filename    string `json:"filename,omitempty"`     // display name for chat UI (uuids look ugly)
}

// runState encapsulates all mutable state for a single agent run.
// Grouping these fields enables extracting loop sub-operations into methods
// on *runState without passing 20+ individual variables.
type runState struct {
	// Loop control
	loopDetector toolLoopState
	totalUsage   providers.Usage
	// lastPromptTokens is the prompt_tokens from the FINAL LLM iteration
	// of this run (last-write-wins as iterations progress). Used to set
	// sessions.last_prompt_tokens without the per-iteration over-counting
	// that totalUsage.PromptTokens introduces in tool loops.
	lastPromptTokens int
	iteration        int
	totalToolCalls   int

	// Output accumulators
	finalContent   string
	finalThinking  string
	asyncToolCalls []string // async spawn tool names for fallback
	mediaResults   []MediaResult
	deliverables   []string // tool output content for team task results
	pendingMsgs    []providers.Message

	// Event state
	blockReplies   int
	lastBlockReply string

	// Crash safety
	checkpointFlushedMsgs int

	// Mid-loop compaction and overhead calibration
	midLoopCompacted   bool
	overheadTokens     int // non-history token overhead (system prompt + tools + context files)
	overheadCalibrated bool

	// Bootstrap detection
	bootstrapWriteDetected bool

	// Team task orphan detection
	teamTaskCreates int
	teamTaskSpawns  int

	// Skill evolution nudge state
	skillNudge70Sent    bool
	skillNudge90Sent    bool
	skillPostscriptSent bool

	// Loop detector kill flag — set when any detector triggers critical level.
	// Propagated to RunResult.LoopKilled so the consumer can auto-fail team tasks.
	loopKilled bool

	// Truncation retry counter — caps consecutive truncation/parse-error retries
	// to prevent burning through all iterations when max_tokens is too low.
	truncationRetries int
}
