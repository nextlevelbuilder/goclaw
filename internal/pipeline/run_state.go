package pipeline

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/workspace"
)

// RunState is the shared mutable state for a single pipeline run.
// Passed by pointer through all stages.
type RunState struct {
	// Identity (set once at pipeline start, immutable during run)
	Input     *RunInput
	Workspace *workspace.WorkspaceContext
	Model     string
	Provider  providers.Provider

	// Ctx holds enriched context from ContextStage (agent/user/workspace values).
	// Pipeline.Run uses this for all stages after setup completes.
	Ctx context.Context

	// Message buffer (read/write by multiple stages)
	Messages *MessageBuffer

	// Per-stage substates
	Context   ContextState
	Think     ThinkState
	Prune     PruneState
	Tool      ToolState
	Observe   ObserveState
	Compact   CompactState
	Evolution EvolutionState

	// Cross-cutting concerns
	Iteration int
	RunID     string
	ExitCode  StageResult
}

// NewRunState creates a RunState with identity fields set.
func NewRunState(input *RunInput, ws *workspace.WorkspaceContext, model string, provider providers.Provider) *RunState {
	return &RunState{
		Input:     input,
		Workspace: ws,
		Model:     model,
		Provider:  provider,
		RunID:     input.RunID,
		Messages:  NewMessageBuffer(providers.Message{}),
	}
}

// BuildResult converts final RunState into a RunResult.
func (rs *RunState) BuildResult() *RunResult {
	return &RunResult{
		RunID:          rs.RunID,
		Content:        rs.Observe.FinalContent,
		Thinking:       rs.Observe.FinalThinking,
		TotalUsage:     rs.Think.TotalUsage,
		Iterations:     rs.Iteration,
		ToolCalls:      rs.Tool.TotalToolCalls,
		LoopKilled:     rs.Tool.LoopKilled,
		AsyncToolCalls: rs.Tool.AsyncToolCalls,
		MediaResults:   rs.Tool.MediaResults,
		Deliverables:   rs.Tool.Deliverables,
		BlockReplies:   rs.Observe.BlockReplies,
		LastBlockReply: rs.Observe.LastBlockReply,
	}
}

// RunInput is the pipeline's view of a run request.
// Converted from agent.RunRequest by the adapter in Phase 8.
type RunInput struct {
	SessionKey        string
	Message           string
	Media             []bus.MediaFile
	ForwardMedia      []bus.MediaFile
	Channel           string
	ChannelType       string
	ChatTitle         string
	ChatID            string
	PeerKind          string
	RunID             string
	UserID            string
	SenderID          string
	Stream            bool
	ExtraSystemPrompt string
	SkillFilter       []string
	HistoryLimit      int
	ToolAllow         []string
	LightContext      bool
	RunKind           string
	DelegationID      string
	TeamID            string
	TeamTaskID        string
	ParentAgentID     string
	MaxIterations     int
	ModelOverride     string
	HideInput         bool
	ContentSuffix     string
	LeaderAgentID     string
	WorkspaceChannel  string
	WorkspaceChatID   string
	TeamWorkspace     string
}

// MediaResult represents a media file produced during tool execution.
type MediaResult struct {
	Path        string
	ContentType string
	Size        int64
	AsVoice     bool
}
