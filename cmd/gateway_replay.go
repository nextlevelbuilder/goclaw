package cmd

import (
	"context"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// makeRunReplayer builds the closure that the WS runs.replay handler invokes.
// It mirrors makeRunResumer: resolve the run's owning agent through the router,
// then drive agent.ReplayRun, which asserts the loop implements loopReplayer and
// resumes from the requested checkpoint snapshot. Returns nil when the store,
// snapshot store, or router is absent — the handlers then report unavailable,
// keeping the surface safe before wiring.
func makeRunReplayer(agents *agent.Router, runs store.RunsStore, snaps store.CheckpointSnapshotStore) func(ctx context.Context, runID string, seq int) (*agent.RunResult, error) {
	if agents == nil || runs == nil || snaps == nil {
		return nil
	}
	return func(ctx context.Context, runID string, seq int) (*agent.RunResult, error) {
		result, err := agent.ReplayRun(ctx, agents, runs, snaps, runID, seq)
		if err == nil {
			return result, nil
		}
		slog.Warn("runs.replay_failed", "run_id", runID, "seq", seq, "error", err)
		return nil, err
	}
}