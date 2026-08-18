package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// loopReplayer is the time-travel capability exposed by agent.Loop but not by
// the broad agent.Agent interface. The concrete loop type is resolved via the
// agent router (which builds *agent.Loop instances through the managed
// resolver), so wiring replay only needs this narrow, compile-checked assertion
// surface — the agent-side mirror of cmd's loopResumer.
type loopReplayer interface {
	// ResumeRunFrom resumes a durable run from an explicit checkpoint blob
	// instead of the run record's latest checkpoint (time-travel rewind).
	ResumeRunFrom(ctx context.Context, runID string, checkpoint json.RawMessage) (*RunResult, error)
}

// ErrRunReplayUnavailable is returned by ReplayRun when durable run records,
// checkpoint snapshots, or the resolver are not wired. Distinct from any
// transient store error so callers can surface "replay not supported" cleanly.
var ErrRunReplayUnavailable = errors.New("run replay unavailable: durable run records or snapshots not wired")

// ErrRunReplayNotFound is returned when the run record or the requested
// checkpoint snapshot does not exist (or has no snapshot body).
var ErrRunReplayNotFound = errors.New("run replay failed: run or snapshot not found")

// ReplayRun rewinds a durable run to an earlier checkpoint snapshot (time
// travel) and drives the owning agent's loop to resume from that snapshot. It
// mirrors cmd's makeRunResumer: resolve the run record, resolve the owning
// agent via the router, assert it implements loopReplayer, then drive the
// resume. The loop reads the snapshot body and rebuilds the pipeline state from
// it, so nothing here re-creates the run row. Returns nil-ish unavailable error
// when any required capability is absent.
func ReplayRun(ctx context.Context, agents *Router, runs store.RunsStore, snaps store.CheckpointSnapshotStore, runID string, seq int) (*RunResult, error) {
	if agents == nil || runs == nil || snaps == nil {
		return nil, ErrRunReplayUnavailable
	}
	if runID == "" {
		return nil, fmt.Errorf("replay run: run_id required")
	}
	run, err := runs.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run == nil || run.AgentID == nil {
		return nil, ErrRunReplayNotFound
	}
	snap, err := snaps.GetCheckpointSnapshot(ctx, runID, seq)
	if err != nil {
		slog.Warn("runs.replay_snapshot_get_failed", "run_id", runID, "seq", seq, "error", err)
		return nil, ErrRunReplayNotFound
	}
	if snap == nil || len(snap.Snapshot) == 0 {
		return nil, ErrRunReplayNotFound
	}

	ag, err := agents.Get(ctx, run.AgentID.String())
	if err != nil {
		slog.Warn("runs.replay_agent_resolve_failed", "run_id", runID, "agent_id", run.AgentID.String(), "error", err)
		return nil, err
	}
	l, ok := ag.(loopReplayer)
	if !ok {
		slog.Warn("runs.replay_not_supported_by_agent", "run_id", runID, "agent_id", run.AgentID.String())
		return nil, ErrRunReplayUnavailable
	}
	return l.ResumeRunFrom(ctx, runID, snap.Snapshot)
}

// ResumeRunFrom restores a durable run from an explicit checkpoint blob and
// drives it through the pipeline — the "replay/rewind" half of time travel. It
// mirrors Loop.ResumeRun (loop_run.go:329) except the pipeline state comes from
// the given checkpoint (a historical snapshot) instead of the run record's
// latest checkpoint: read the run record, restore the pipeline state, rebuild
// the RunRequest from the record + checkpoint input, then run the pipeline from
// the checkpoint's iteration. A corrupt/unparseable checkpoint falls back to
// starting the run from scratch so replay never hard-fails on old data. The run
// row is NOT re-created; the existing record keeps its identity and the replay
// finalizes the same row on completion.
func (l *Loop) ResumeRunFrom(ctx context.Context, runID string, checkpoint json.RawMessage) (*RunResult, error) {
	if l.runsStore == nil {
		return nil, ErrRunResumeUnavailable
	}
	if runID == "" {
		return nil, errors.New("resume run: run_id required")
	}
	run, err := l.runsStore.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, ErrRunResumeNotFound
	}

	// Restore the pipeline state. Failing here (corrupt/empty checkpoint) is not
	// fatal: the run starts fresh, losing only its in-flight progress.
	var state *pipeline.RunState
	var savedInput *pipeline.RunInput
	if len(checkpoint) > 0 {
		// RestoreCheckpoint intentionally does NOT restore Input (the caller
		// resolves it); the checkpoint JSON still carries it, so extract it to
		// rebuild the RunRequest's message/channel identity for callbacks.
		savedInput = checkpointRunInput(checkpoint)
		state, err = pipeline.RestoreCheckpoint(checkpoint)
		if err != nil {
			slog.Warn("runs.replay_restore_failed", "run_id", runID, "error", err)
			state = nil // fall through to fresh start
		}
	}

	req := runRequestFromRunRecord(run, savedInput)
	// Resume keeps the existing run row alive with a heartbeat WITHOUT recreating
	// it: CreateRun's ON CONFLICT upsert would clobber the stored checkpoint with
	// NULL. newRunRecordUpdater only starts the heartbeat goroutine. nil when the
	// store is not wired (then checkpoints are also disabled).
	resumeRecord := newRunRecordUpdater(ctx, l, runID)
	defer resumeRecord.terminal(ctx, store.AgentRunStatusFailed, "replay finalized by safety net (likely panic or goroutine leak)")

	// Durable checkpoint writer for the resumed execution: continue updating the
	// same run checkpoint so a re-failure stays resumable. checkpointWritten
	// tracks whether a checkpoint landed during this resume so the error path can
	// decide between compacting (still resumable) and terminal-failed.
	var checkpointWriter func(ctx context.Context, s *pipeline.RunState) error
	var checkpointWritten bool
	if l.runsStore != nil {
		runsStoreSnapshot := l.runsStore
		checkpointWriter = func(ctx context.Context, s *pipeline.RunState) error {
			raw, err := s.MarshalCheckpoint()
			if err != nil {
				slog.Warn("runs.replay_checkpoint_marshal_failed", "run_id", runID, "error", err)
				return err
			}
			if err := runsStoreSnapshot.UpdateRunCheckpoint(ctx, runID, store.AgentRunStatusRunning, raw); err != nil {
				return err
			}
			checkpointWritten = true
			return nil
		}
	}
	result, err := l.runViaPipeline(ctx, req, state, checkpointWriter)
	if err != nil {
		if checkpointWritten {
			slog.Warn("replayed run compacted, resumable", "run_id", runID, "error", err)
			resumeRecord.terminal(ctx, store.AgentRunStatusCompacting, err.Error())
		} else {
			resumeRecord.terminal(ctx, store.AgentRunStatusFailed, err.Error())
		}
		return nil, err
	}
	resumeRecord.terminal(ctx, store.AgentRunStatusCompleted, "")
	return result, nil
}