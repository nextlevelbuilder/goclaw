package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// Intentional suspend/wake (hibernation): a running agent run can be paused
// on-demand, writing a durable checkpoint and transitioning the run record to
// "paused", then woken through the existing resume path. This complements the
// crash-recovery pause in RecoverStaleRuns — it makes suspension a deliberate,
// user-driven lifecycle transition rather than only an outcome of a stalled
// heartbeat.

// ErrRunSuspendUnavailable is returned when intentional suspension cannot be
// performed because the durable run record store, router, or owning loop does
// not support it. Distinct from transient store errors so callers can surface
// "suspend not supported" cleanly.
var ErrRunSuspendUnavailable = errors.New("run suspend unavailable: durable run records or suspend capability not wired")

// ErrRunSuspendNotFound is returned when the run record does not exist or has no
// owning agent to suspend.
var ErrRunSuspendNotFound = errors.New("run suspend failed: run not found or has no owning agent")

// loopSuspender is the narrow suspend capability exposed by agent.Loop but not
// by the broad agent.Agent interface. The concrete loop type is resolved via the
// agent router (which builds *agent.Loop instances through the managed
// resolver); asserting this interface keeps the wiring to a single method
// surface, mirroring loopResumer in cmd/gateway_resume.go.
type loopSuspender interface {
	SuspendRun(ctx context.Context, runID string) error
}

// SuspendRun resolves the run's owning agent through the router, asserts it
// implements loopSuspender, and transitions the run to paused (writing the
// latest durable checkpoint). It blocks until the record transition completes —
// it never spawns a goroutine. Wake is performed through the shared resume
// path (see WakeRun / cmd.makeRunResumer); no resume logic lives here.
//
// runID must be the durable run record ID (agent_runs.run_id). The router
// resolves the owning agent by the run's stored AgentID.
func SuspendRun(ctx context.Context, router *Router, runs store.RunsStore, runID string) error {
	if router == nil || runs == nil {
		return ErrRunSuspendUnavailable
	}
	if runID == "" {
		return errors.New("suspend run: run_id required")
	}
	run, err := runs.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if run == nil || run.AgentID == nil {
		return ErrRunSuspendNotFound
	}
	ag, err := router.Get(ctx, run.AgentID.String())
	if err != nil {
		slog.Warn("runs.pause_agent_resolve_failed", "run_id", runID, "agent_id", run.AgentID.String(), "error", err)
		return err
	}
	l, ok := ag.(loopSuspender)
	if !ok {
		slog.Warn("runs.pause_not_supported_by_agent", "run_id", runID, "agent_id", run.AgentID.String())
		return ErrRunSuspendUnavailable
	}
	return l.SuspendRun(ctx, runID)
}

// WakeRun resumes a paused run by reusing the existing resume capability. The
// resume closure is built once at gateway wiring time (cmd.makeRunResumer,
// which drives Loop.ResumeRun); this wrapper exists so the wake entrypoint is
// named distinctly from resume while carrying zero resume logic of its own.
func WakeRun(ctx context.Context, resume func(ctx context.Context, runID string) (*RunResult, error), runID string) (*RunResult, error) {
	if resume == nil {
		return nil, ErrRunResumeUnavailable
	}
	return resume(ctx, runID)
}

// SuspendRun transitions an existing run record to paused, preserving its latest
// durable checkpoint so ResumeRun can pick up where it left off. When no
// checkpoint has landed yet (run is below the durable checkpoint interval),
// UpdateRunCheckpoint stores nil and ResumeRun falls back to a fresh start —
// the same non-fatal recovery ResumeRun applies to a corrupt checkpoint.
//
// The write is the ONLY direct interaction with the run record here: it reuses
// the store's UpdateRunCheckpoint (the exact path runRecordUpdater.checkpoint
// drives during execution), so the pause does not alter the append-only
// checkpoint-snapshot timeline owned elsewhere. Idempotent: pausing an already
// paused run is a no-op, and a terminal run is left untouched (it cannot be
// resurrected).
//
// SuspendRun emits a broadcast AgentEvent (subtype AgentEventRunPaused) so WS
// clients observe the transition through the same channel as run.started /
// run.completed. The emit is best-effort — a nil onEvent callback drops it.
func (l *Loop) SuspendRun(ctx context.Context, runID string) error {
	if l.runsStore == nil {
		return ErrRunSuspendUnavailable
	}
	if runID == "" {
		return errors.New("suspend run: run_id required")
	}
	run, err := l.runsStore.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if run == nil {
		return ErrRunSuspendNotFound
	}
	// Paused is idempotent; terminal runs cannot be suspended (resurrected).
	if run.Status == store.RunTimelineStatusPaused || store.AgentRunTerminal(run.Status) {
		return nil
	}

	// Write the latest durable checkpoint back with status "paused". Using the
	// stored checkpoint (rather than re-marshalling the in-memory state) keeps
	// this path consistent with what the pipeline's CheckpointStage already
	// persisted at the durable interval — the newest recoverable snapshot.
	if err := l.runsStore.UpdateRunCheckpoint(ctx, runID, store.RunTimelineStatusPaused, run.Checkpoint); err != nil {
		slog.Warn("runs.pause_checkpoint_failed", "run_id", runID, "error", err)
		return err
	}

	// Emit the pause transition through the loop's emit path (same channel as
	// run.started/run.completed): it stamps the per-run WS seq, and since
	// "run.paused" is not a terminal timeline event the counter is left intact for
	// the resumed execution. A nil onEvent callback is handled inside emit.
	l.emit(AgentEvent{
		Type:    protocol.AgentEventRunPaused,
		AgentID: l.id,
		RunID:   runID,
		Payload: &protocol.RunPausedPayload{
			RunID:         runID,
			Iteration:     checkpointIteration(run.Checkpoint),
			CheckpointSeq: 0, // agent_runs.checkpoint has no snapshot seq (owned by run_checkpoint_snapshots)
		},
	})
	return nil
}

// checkpointIteration extracts the saved pipeline iteration from a durable
// checkpoint JSON blob. Returns 0 when absent/unparseable (run never checkpointed).
func checkpointIteration(raw json.RawMessage) int {
	var wrapper struct {
		Iteration int `json:"iteration"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return 0
	}
	return wrapper.Iteration
}