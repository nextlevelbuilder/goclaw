package pipeline

import (
	"context"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// CheckpointStage runs per iteration. Flushes pending messages to session store
// every N iterations for crash recovery.
type CheckpointStage struct {
	deps *PipelineDeps
}

// NewCheckpointStage creates a CheckpointStage.
func NewCheckpointStage(deps *PipelineDeps) *CheckpointStage {
	return &CheckpointStage{deps: deps}
}

func (s *CheckpointStage) Name() string { return "checkpoint" }

// Execute flushes pending messages to session store at checkpoint intervals.
func (s *CheckpointStage) Execute(ctx context.Context, state *RunState) error {
	interval := s.deps.Config.CheckpointInterval
	if interval <= 0 {
		interval = 5
	}
	if state.Iteration == 0 || state.Iteration%interval != 0 {
		return nil // skip this iteration
	}

	if s.deps.FlushMessages == nil {
		return nil
	}

	pending := state.Messages.FlushPending()
	if len(pending) == 0 {
		return nil
	}
	persistable := persistableMessages(pending)
	if len(persistable) == 0 {
		return nil
	}

	if err := s.deps.FlushMessages(ctx, state.Input.SessionKey, persistable); err != nil {
		// Non-fatal: messages moved to history by FlushPending, will be flushed by FinalizeStage.
		slog.Warn("checkpoint flush failed", "err", err, "iteration", state.Iteration)
		return nil
	}

	state.Compact.CheckpointFlushedMsgs += len(persistable)
	return nil
}

func persistableMessages(messages []providers.Message) []providers.Message {
	for _, msg := range messages {
		if msg.Transient {
			filtered := make([]providers.Message, 0, len(messages))
			for _, candidate := range messages {
				if !candidate.Transient {
					filtered = append(filtered, candidate)
				}
			}
			return filtered
		}
	}
	return messages
}
