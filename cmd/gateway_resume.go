package cmd

import (
	"context"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// loopResumer is the resume capability exposed by agent.Loop but not by the
// broad agent.Agent interface. The concrete loop type is resolved via the agent
// router (which builds *agent.Loop instances through the managed resolver), so
// wiring resume only needs this narrow, compile-checked assertion surface.
type loopResumer interface {
	ResumeRun(ctx context.Context, runID string) (*agent.RunResult, error)
}

// makeRunResumer builds the closure that the WS runs.resume and HTTP
// POST /v1/runs/{runID}/resume handlers invoke. It resolves the run's owning
// agent through the router, asserts it implements loopResumer, and drives the
// agent's Loop.ResumeRun. The loop already carries RunsStore + heartbeat
// interval from the resolver, so the resumed execution finalizes its own run
// record. Returns nil when the store or router is absent, which the handlers
// already treat as "resume unavailable".
func makeRunResumer(agents *agent.Router, runs store.RunsStore) func(ctx context.Context, runID string) (*agent.RunResult, error) {
	if agents == nil || runs == nil {
		return nil
	}
	return func(ctx context.Context, runID string) (*agent.RunResult, error) {
		run, err := runs.GetRun(ctx, runID)
		if err != nil {
			return nil, err
		}
		if run == nil || run.AgentID == nil {
			return nil, agent.ErrRunResumeNotFound
		}
		ag, err := agents.Get(ctx, run.AgentID.String())
		if err != nil {
			slog.Warn("runs.resume_agent_resolve_failed", "run_id", runID, "agent_id", run.AgentID.String(), "error", err)
			return nil, err
		}
		l, ok := ag.(loopResumer)
		if !ok {
			slog.Warn("runs.resume_not_supported_by_agent", "run_id", runID, "agent_id", run.AgentID.String())
			return nil, agent.ErrRunResumeUnavailable
		}
		return l.ResumeRun(ctx, runID)
	}
}
