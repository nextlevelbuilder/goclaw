package cmd

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// makeRunSuspendResumer builds the closures behind the WS runs.pause / runs.wake
// handlers. The suspend side delegates to agent.SuspendRun, which resolves the
// run's owning agent through the router, asserts it implements the narrow
// suspend capability, and drives the agent's Loop.SuspendRun (write checkpoint +
// transition to paused). The wake side REUSES the exact resume closure built for
// runs.resume (makeRunResumer → Loop.ResumeRun) — no resume logic is duplicated
// or reinvented here, so a wake is indistinguishable from a resume at the loop
// boundary. Returns nils when the store or router is absent, which the handlers
// already treat as "suspend/wake unavailable".
func makeRunSuspendResumer(agents *agent.Router, runs store.RunsStore) (func(ctx context.Context, runID string) error, func(ctx context.Context, runID string) (*agent.RunResult, error)) {
	if agents == nil || runs == nil {
		return nil, nil
	}
	return func(ctx context.Context, runID string) error {
		return agent.SuspendRun(ctx, agents, runs, runID)
	}, makeRunResumer(agents, runs)
}