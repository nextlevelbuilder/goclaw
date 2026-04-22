package pipeline

import (
	"context"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tokencount"
	"github.com/nextlevelbuilder/goclaw/internal/workspace"
)

// PruneStats holds counts from a single pruneContextMessages invocation.
// Populated by the pruning function; read by PruneStage for event emission.
type PruneStats struct {
	ResultsTrimmed int  // Pass 1: soft-trimmed count
	ResultsCleared int  // Pass 2: hard-cleared count
	Compacted      bool // LLM compaction ran this cycle
}

// PipelineDeps bundles all external dependencies stages need.
// Passed to NewDefaultPipeline; individual stages receive what they need via closure or direct field access.
type PipelineDeps struct {
	TokenCounter tokencount.TokenCounter
	EventBus     eventbus.DomainEventBus
	Config       PipelineConfig
	// Hooks is the hook dispatcher. nil = no hooks (zero-overhead fast path).
	Hooks hooks.Dispatcher

	// ResolveContextWindow returns the effective context window (in tokens) for
	// a given provider/model pair. Nil = always use Config.ContextWindow.
	// Invoked ONCE per run by ContextStage and stored in RunState.Context.EffectiveContextWindow.
	ResolveContextWindow func(provider, model string) int

	// Callbacks from agent.Loop — Phase 8 adapter wires these.
	EmitEvent func(event any)

	// Auto-inject memory context (ContextStage, L0 tier).
	// Callback captures agent/tenant context via closure. recentContext carries
	// a short snippet of recent conversation (last 1-2 user turns) so the
	// downstream recall query can resolve pronouns and implicit references.
	// Empty recentContext = legacy single-message search semantics.
	AutoInject func(ctx context.Context, userMessage, userID, recentContext string) (string, error)

	// InjectContext sets up agent/tenant/user/workspace/tool context values.
	// Wraps injectContext() for v3 pipeline. Called once at ContextStage start.
	InjectContext func(ctx context.Context, input *RunInput) (context.Context, error)

	// LoadSessionHistory loads persisted session history + summary from store.
	// Called before BuildMessages in ContextStage.
	LoadSessionHistory func(ctx context.Context, sessionKey string) ([]providers.Message, string)

	// Context callbacks (ContextStage)
	ResolveWorkspace func(ctx context.Context, input *RunInput) (*workspace.WorkspaceContext, error)
	LoadContextFiles func(ctx context.Context, userID string) ([]bootstrap.ContextFile, bool) // files, hadBootstrap
	BuildMessages    func(ctx context.Context, input *RunInput, history []providers.Message, summary string) ([]providers.Message, error)
	EnrichMedia      func(ctx context.Context, state *RunState) error
	InjectReminders  func(ctx context.Context, input *RunInput, msgs []providers.Message) []providers.Message

	// Think callbacks (ThinkStage)
	BuildFilteredTools  func(state *RunState) ([]providers.ToolDefinition, error)
	CallLLM             func(ctx context.Context, state *RunState, req providers.ChatRequest) (*providers.ChatResponse, error)
	UniqueToolCallIDs   func(calls []providers.ToolCall, runID string, iteration int) []providers.ToolCall
	EmitBlockReply      func(content string) // emit block.reply for intermediate assistant content

	// Prune callbacks (PruneStage)
	PruneMessages   func(msgs []providers.Message, budget int) ([]providers.Message, PruneStats)
	SanitizeHistory func(msgs []providers.Message) ([]providers.Message, int)
	CompactMessages func(ctx context.Context, msgs []providers.Message, model string) ([]providers.Message, error)

	// Cache-TTL gate callbacks (Phase 06). All optional (nil = feature disabled).
	GetProviderCaps  func() providers.ProviderCapabilities  // provider capabilities for cache detection
	GetPruningConfig func() *config.ContextPruningConfig    // pruning config (TTL field)
	GetCacheTouch    func(sessionKey string) time.Time      // per-session last prune-mutation timestamp
	MarkCacheTouched func(sessionKey string)                // record mutation timestamp AFTER prune mutates

	// Memory flush callbacks (MemoryFlushStage, invoked by PruneStage)
	RunMemoryFlush func(ctx context.Context, state *RunState) error

	// Tool callbacks (ToolStage)
	// ExecuteToolCall runs a single tool call with full state mutation (sequential only).
	ExecuteToolCall func(ctx context.Context, state *RunState, tc providers.ToolCall) ([]providers.Message, error)
	// ExecuteToolRaw runs tool I/O only (parallel-safe, no state mutation).
	// Returns tool message + opaque raw data passed through to ProcessToolResult.
	// If nil, ToolStage falls back to sequential ExecuteToolCall.
	ExecuteToolRaw func(ctx context.Context, tc providers.ToolCall) (providers.Message, any, error)
	// ProcessToolResult processes a raw tool result with state mutation (sequential only).
	ProcessToolResult func(ctx context.Context, state *RunState, tc providers.ToolCall, rawMsg providers.Message, rawData any) []providers.Message
	// CheckReadOnly checks read-only streak. Returns warning message (if any) and whether to break.
	CheckReadOnly func(state *RunState) (*providers.Message, bool)

	// Observe callbacks (ObserveStage)
	DrainInjectCh func() []providers.Message

	// Checkpoint callbacks (CheckpointStage)
	FlushMessages func(ctx context.Context, sessionKey string, msgs []providers.Message) error

	// Finalize callbacks (FinalizeStage)
	SkillPostscript          func(ctx context.Context, content string, totalToolCalls int) string // skill evolution nudge (nil = disabled)
	SanitizeContent          func(content string) string
	StripMessageDirectives   func(content string) string
	DeduplicateMediaSuffix   func(content, suffix string) string
	IsSilentReply          func(content string) bool
	EmitSessionCompleted   func(ctx context.Context, sessionKey string, msgCount, tokensUsed, compactionCount int)
	UpdateMetadata         func(ctx context.Context, sessionKey string, usage providers.Usage) error
	BootstrapCleanup       func(ctx context.Context, state *RunState) error
	MaybeSummarize         func(ctx context.Context, sessionKey string)
}

// FireHook is nil-safe. Returns FireResult{Decision: DecisionAllow} when no
// dispatcher is wired. Callers on blocking events should abort the stage on
// result.Decision == DecisionBlock and apply result.UpdatedToolInput /
// UpdatedRawInput to their local state when non-nil (builtin-hook mutations).
func (d *PipelineDeps) FireHook(ctx context.Context, ev hooks.Event) (hooks.FireResult, error) {
	if d == nil || d.Hooks == nil {
		return hooks.FireResult{Decision: hooks.DecisionAllow}, nil
	}
	return d.Hooks.Fire(ctx, ev)
}

// PipelineConfig holds pipeline-level settings.
type PipelineConfig struct {
	MaxIterations      int
	MaxToolCalls       int
	CheckpointInterval int // flush every N iterations (default 5)
	ContextWindow      int
	MaxTokens          int
	Compaction         *config.CompactionConfig

	// ReserveTokens is a safety buffer subtracted from the history budget so
	// PruneStage compacts slightly before the hard limit. Prevents edge cases
	// where a provider returns more than MaxTokens output or where the token
	// counter's estimate drifts upward during streaming.
	// Zero (default) preserves legacy behavior: budget = contextWindow - overhead - MaxTokens.
	// Recommended: 5-10% of contextWindow for reasoning-heavy models.
	ReserveTokens int

	// V3 memory/retrieval flags removed — always true at runtime.
	// Memory flush runs if callback != nil; auto-inject runs if AutoInject != nil.
}
