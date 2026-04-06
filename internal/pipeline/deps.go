package pipeline

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tokencount"
	"github.com/nextlevelbuilder/goclaw/internal/workspace"
)

// PipelineDeps bundles all external dependencies stages need.
// Passed to NewDefaultPipeline; individual stages receive what they need via closure or direct field access.
type PipelineDeps struct {
	TokenCounter tokencount.TokenCounter
	EventBus     eventbus.DomainEventBus
	Config       PipelineConfig

	// Callbacks from agent.Loop — Phase 8 adapter wires these.
	EmitEvent func(event any)

	// Context callbacks (ContextStage)
	ResolveWorkspace func(ctx context.Context, input *RunInput) (*workspace.WorkspaceContext, error)
	LoadContextFiles func(ctx context.Context, userID string) ([]bootstrap.ContextFile, bool) // files, hadBootstrap
	BuildMessages    func(ctx context.Context, input *RunInput, history []providers.Message) ([]providers.Message, error)
	EnrichMedia      func(ctx context.Context, input *RunInput) error
	InjectReminders  func(ctx context.Context, input *RunInput, msgs []providers.Message) []providers.Message

	// Think callbacks (ThinkStage)
	BuildFilteredTools func(state *RunState) ([]providers.ToolDefinition, error)
	CallLLM            func(ctx context.Context, state *RunState, req providers.ChatRequest) (*providers.ChatResponse, error)

	// Prune callbacks (PruneStage)
	PruneMessages   func(msgs []providers.Message, budget int) []providers.Message
	CompactMessages func(ctx context.Context, msgs []providers.Message, model string) ([]providers.Message, error)

	// Memory flush callbacks (MemoryFlushStage, invoked by PruneStage)
	RunMemoryFlush func(ctx context.Context, state *RunState) error

	// Tool callbacks (ToolStage)
	// ExecuteToolCall runs a single tool call. Returns result messages to append.
	ExecuteToolCall func(ctx context.Context, state *RunState, tc providers.ToolCall) ([]providers.Message, error)
	// CheckReadOnly checks read-only streak. Returns warning message (if any) and whether to break.
	CheckReadOnly func(state *RunState) (*providers.Message, bool)

	// Observe callbacks (ObserveStage)
	DrainInjectCh func() []providers.Message

	// Checkpoint callbacks (CheckpointStage)
	FlushMessages func(ctx context.Context, sessionKey string, msgs []providers.Message) error

	// Finalize callbacks (FinalizeStage)
	SanitizeContent  func(content string) string
	UpdateMetadata   func(ctx context.Context, sessionKey string, usage providers.Usage) error
	BootstrapCleanup func(ctx context.Context, state *RunState) error
	MaybeSummarize   func(ctx context.Context, sessionKey string)
}

// PipelineConfig holds pipeline-level settings.
type PipelineConfig struct {
	MaxIterations      int
	MaxToolCalls       int
	CheckpointInterval int // flush every N iterations (default 5)
	ContextWindow      int
	MaxTokens          int
	Compaction         *config.CompactionConfig
}
