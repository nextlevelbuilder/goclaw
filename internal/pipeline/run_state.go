package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/workspace"
)

// resuming is set when this RunState was restored from a durable checkpoint.
// Pipeline.Run and ContextStage consult it to skip setup stages that would
// otherwise rebuild messages/workspace/context and wipe restored state.
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

	// resuming marks a checkpoint-restored state. It disables setup-stage
	// rebuilds that would clobber restored messages/workspace/substates.
	resuming bool

	// CurrentLLMSpanID is the most recent LLM-call span in this run; tool spans parent to it.
	CurrentLLMSpanID *uuid.UUID
	// CurrentToolSpanID is the most recent tool-call span; post-tool-use hook spans parent to it.
	CurrentToolSpanID *uuid.UUID

	// Calls is the per-call usage breakdown (LLM calls + tool-internal LLM calls),
	// appended during the run. Guarded by callsMu for the parallel tool path.
	Calls   []providers.CallUsage
	callsMu sync.Mutex
}

// Resuming reports whether this state was restored from a durable checkpoint.
// True disables setup-stage rebuilds (ContextStage skips message/workspace
// reconstruction) and Pipeline.Run starts the iteration loop at state.Iteration.
func (rs *RunState) Resuming() bool {
	if rs == nil {
		return false
	}
	return rs.resuming
}

// maxCheckpointMessages caps the number of messages persisted in a checkpoint.
// Checkpoints carry substate + conversation so a resume can continue the loop;
// the session store remains the long-term history authority. History beyond this
// cap is dropped (oldest first); the system prompt (index 0) is always kept.
const maxCheckpointMessages = 200

// checkpointMessage is the serializable projection of providers.Message kept in
// a durable checkpoint. providers.Message tags Images/RawAssistantContent with
// `json:"-"` (runtime-only), so an explicit struct preserves them here while
// dropping Videos and Transient (runtime-only; MediaRefs carry the persistent
// video references).
type checkpointMessage struct {
	Role                string                     `json:"role"`
	Content             string                     `json:"content"`
	Thinking            string                     `json:"thinking,omitempty"`
	Images              []providers.ImageContent   `json:"images,omitempty"`
	MediaRefs           []providers.MediaRef       `json:"media_refs,omitempty"`
	ToolCalls           []providers.ToolCall       `json:"tool_calls,omitempty"`
	ToolCallID          string                     `json:"tool_call_id,omitempty"`
	IsError             bool                       `json:"is_error,omitempty"`
	ToolName            string                     `json:"tool_name,omitempty"`
	Phase               string                     `json:"phase,omitempty"`
	RawAssistantContent json.RawMessage            `json:"raw_assistant_content,omitempty"`
	CreatedAt           *time.Time                 `json:"created_at,omitempty"`
}

// runStateCheckpoint is the on-disk shape of a durable checkpoint.
type runStateCheckpoint struct {
	Version   int                             `json:"version"`
	RunID     string                          `json:"run_id"`
	Model     string                          `json:"model,omitempty"`
	Iteration int                             `json:"iteration"`
	Input     *RunInput                       `json:"input,omitempty"`
	Workspace *workspace.WorkspaceContext     `json:"workspace,omitempty"`
	Messages  []checkpointMessage             `json:"messages,omitempty"`
	Context   ContextState                    `json:"context,omitempty"`
	Think     ThinkState                      `json:"think,omitempty"`
	Prune     PruneState                      `json:"prune,omitempty"`
	Tool      ToolState                       `json:"tool,omitempty"`
	Observe   ObserveState                    `json:"observe,omitempty"`
	Compact   CompactState                    `json:"compact,omitempty"`
	Evolution EvolutionState                  `json:"evolution,omitempty"`
	Calls     []providers.CallUsage           `json:"calls,omitempty"`
}

const checkpointVersion = 1

// MarshalCheckpoint serializes the run state for durable storage. It captures
// identity, substates, and messages (keeping Images/RawAssistantContent via
// checkpointMessage) but omits runtime-only values: the Provider, Ctx, exit
// code, span IDs, and the agent-owned LoopDetector. Message history is capped at
// maxCheckpointMessages (oldest dropped) to bound payload size.
func (rs *RunState) MarshalCheckpoint() (json.RawMessage, error) {
	if rs == nil {
		return nil, fmt.Errorf("marshal checkpoint: nil state")
	}
	cp := runStateCheckpoint{
		Version:   checkpointVersion,
		RunID:     rs.RunID,
		Model:     rs.Model,
		Iteration: rs.Iteration,
		Input:     rs.Input,
		Workspace: rs.Workspace,
		Context:   rs.Context,
		Think:     rs.Think,
		Prune:     rs.Prune,
		Tool:      rs.Tool,
		Observe:   rs.Observe,
		Compact:   rs.Compact,
		Evolution: rs.Evolution,
		Calls:     rs.Calls,
	}
	if rs.Messages != nil {
		cp.Messages = toCheckpointMessages(rs.Messages.All())
	}
	return json.Marshal(cp)
}

// toCheckpointMessages converts messages to their serializable projection,
// keeping system + the most recent maxCheckpointMessages-1 entries.
func toCheckpointMessages(msgs []providers.Message) []checkpointMessage {
	if len(msgs) == 0 {
		return nil
	}
	if len(msgs) > maxCheckpointMessages {
		// Always keep the system prompt; drop the oldest history tail.
		tail := msgs[len(msgs)-(maxCheckpointMessages-1):]
		msgs = append([]providers.Message{msgs[0]}, tail...)
	}
	out := make([]checkpointMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, checkpointMessage{
			Role:                m.Role,
			Content:             m.Content,
			Thinking:            m.Thinking,
			Images:              m.Images,
			MediaRefs:           m.MediaRefs,
			ToolCalls:           m.ToolCalls,
			ToolCallID:          m.ToolCallID,
			IsError:             m.IsError,
			ToolName:            m.ToolName,
			Phase:               m.Phase,
			RawAssistantContent: m.RawAssistantContent,
			CreatedAt:           m.CreatedAt,
		})
	}
	return out
}

func (cm checkpointMessage) toProvider() providers.Message {
	return providers.Message{
		Role:                cm.Role,
		Content:             cm.Content,
		Thinking:            cm.Thinking,
		Images:              cm.Images,
		MediaRefs:           cm.MediaRefs,
		ToolCalls:           cm.ToolCalls,
		ToolCallID:          cm.ToolCallID,
		IsError:             cm.IsError,
		ToolName:            cm.ToolName,
		Phase:               cm.Phase,
		RawAssistantContent: cm.RawAssistantContent,
		CreatedAt:           cm.CreatedAt,
	}
}

// RestoreCheckpoint rebuilds a RunState from a durable checkpoint. It restores
// substates, messages, iteration, and run identity fields, marks the state
// resuming (so setup stages are skipped), and initializes a fresh MessageBuffer.
// Input/Model/Provider are NOT restored — the caller resolves them from the run
// record / original request after this call. Unparseable checkpoints return an
// error so the caller can fall back to starting the run from scratch.
func RestoreCheckpoint(raw json.RawMessage) (*RunState, error) {
	var cp runStateCheckpoint
	if err := json.Unmarshal(raw, &cp); err != nil {
		return nil, fmt.Errorf("restore checkpoint: %w", err)
	}
	rs := &RunState{
		RunID:     cp.RunID,
		Iteration: cp.Iteration,
		Workspace: cp.Workspace,
		Context:   cp.Context,
		Think:     cp.Think,
		Prune:     cp.Prune,
		Tool:      cp.Tool,
		Observe:   cp.Observe,
		Compact:   cp.Compact,
		Evolution: cp.Evolution,
		Calls:     cp.Calls,
		resuming:  true,
	}
	rs.Messages = NewMessageBuffer(providers.Message{})
	if len(cp.Messages) > 0 {
		restored := make([]providers.Message, 0, len(cp.Messages))
		for _, cm := range cp.Messages {
			restored = append(restored, cm.toProvider())
		}
		rs.Messages.Restore(restored)
	}
	return rs, nil
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

// AppendCall records one call's usage in the run breakdown (thread-safe).
func (rs *RunState) AppendCall(c providers.CallUsage) {
	rs.callsMu.Lock()
	rs.Calls = append(rs.Calls, c)
	rs.callsMu.Unlock()
}

// BuildResult converts final RunState into a RunResult.
func (rs *RunState) BuildResult() *RunResult {
	return &RunResult{
		RunID:          rs.RunID,
		Content:        rs.Observe.FinalContent,
		Thinking:       rs.Observe.FinalThinking,
		TotalUsage:     rs.Think.TotalUsage,
		LastUsage:      rs.Think.LastUsage,
		Iterations:     rs.Iteration,
		ToolCalls:      rs.Tool.TotalToolCalls,
		LoopKilled:     rs.Tool.LoopKilled,
		AsyncToolCalls: rs.Tool.AsyncToolCalls,
		MediaResults:   rs.Tool.MediaResults,
		Deliverables:   rs.Tool.Deliverables,
		BlockReplies:   rs.Observe.BlockReplies,
		LastBlockReply: rs.Observe.LastBlockReply,
		Calls:          rs.Calls,
	}
}

// RunInput is the pipeline's view of a run request.
// Converted from agent.RunRequest by the adapter in Phase 8.
type RunInput struct {
	SessionKey                 string
	Message                    string
	Media                      []bus.MediaFile
	ForwardMedia               []bus.MediaFile
	Channel                    string
	ChannelType                string
	BitrixPortalDomain         string // bitrix24-only: portal domain for entity URL construction
	ChatTitle                  string
	ChatID                     string
	PeerKind                   string
	RunID                      string
	UserID                     string
	SenderID                   string
	SenderName                 string
	Stream                     bool
	ExtraSystemPrompt          string
	SkillFilter                []string
	HistoryLimit               int
	ToolAllow                  []string
	TelegramManagerPermissions []string
	LightContext               bool
	RunKind                    string
	DelegationID               string
	TeamID                     string
	TeamTaskID                 string
	ParentAgentID              string
	MaxIterations              int
	ModelOverride              string
	HideInput                  bool
	ContentSuffix              string
	LeaderAgentID              string
	WorkspaceChannel           string
	WorkspaceChatID            string
	TeamWorkspace              string
}

// MediaResult represents a media file produced during tool execution.
type MediaResult struct {
	Path        string
	ContentType string
	Size        int64
	AsVoice     bool
	// Prompt is the generation prompt for AI-generated media (e.g. create_image).
	// Empty for user-uploaded or non-generated files.
	Prompt string
}
