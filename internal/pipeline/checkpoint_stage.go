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

// Execute flushes pending messages to session store at checkpoint intervals and
// persists a durable pipeline checkpoint when WriteCheckpoint is wired.
func (s *CheckpointStage) Execute(ctx context.Context, state *RunState) error {
	interval := s.deps.Config.CheckpointInterval
	if interval <= 0 {
		interval = 5
	}
	if state.Iteration == 0 || state.Iteration%interval != 0 {
		// Durable checkpoint interval is independent of the message-flush
		// cadence; it drives WriteCheckpoint (resume capability) only.
		s.maybeWriteDurable(ctx, state)
		return nil // skip this iteration's message flush
	}

	if s.deps.FlushMessages == nil {
		s.maybeWriteDurable(ctx, state)
		return nil
	}

	pending := state.Messages.FlushPending()
	if len(pending) == 0 {
		s.maybeWriteDurable(ctx, state)
		return nil
	}
	persistable := persistableMessages(pending)
	if len(persistable) == 0 {
		s.maybeWriteDurable(ctx, state)
		return nil
	}

	if err := s.deps.FlushMessages(ctx, state.Input.SessionKey, persistable); err != nil {
		// Non-fatal: messages moved to history by FlushPending, will be flushed by FinalizeStage.
		slog.Warn("checkpoint flush failed", "err", err, "iteration", state.Iteration)
		s.maybeWriteDurable(ctx, state)
		return nil
	}

	state.Compact.CheckpointFlushedMsgs += len(persistable)
	s.maybeWriteDurable(ctx, state)
	return nil
}

// maybeWriteDurable persists a durable pipeline checkpoint when the callback is
// wired and the durable interval has elapsed. Errors are non-fatal: a failed
// checkpoint only forfeits resume capability, it never aborts the run.
func (s *CheckpointStage) maybeWriteDurable(ctx context.Context, state *RunState) {
	if s.deps.WriteCheckpoint == nil {
		return
	}
	interval := s.deps.Config.DurableCheckpointInterval
	if interval <= 0 {
		return // durable checkpointing disabled (0 = disable)
	}
	if state.Iteration == 0 || state.Iteration%interval != 0 {
		return
	}
	if err := s.deps.WriteCheckpoint(ctx, state); err != nil {
		slog.Warn("durable checkpoint write failed", "err", err, "iteration", state.Iteration)
	}
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
