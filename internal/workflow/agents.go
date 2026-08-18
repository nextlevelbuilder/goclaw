package workflow

import (
	"context"
	"log/slog"
)

// AgentStep wraps a plain step body as a sequential Step, preserving the
// dependency order expected of a single agent action inside a DAG. The id is
// used as both the DAG node id and a human label; deps reference earlier step
// ids the agent action depends on.
func AgentStep(id string, run func(ctx context.Context, rc *RunCtx) error, deps ...string) Step {
	return Step{
		ID:   id,
		Name: id,
		Type: StepSequential,
		Run:  run,
		Deps: append([]string(nil), deps...),
	}
}

// ParallelAgentRound builds a DAG that fans a round out to one StepParallel
// node per agent. Each node's Run body invokes the given per-agent function
// with the agent's ID, so a round can target the same task at every member
// simultaneously. The step ID equals the agent ID, which lets downstream
// steps declare a dependency on the whole round by listing the round's agents.
//
// Duplicate agent IDs in the input are skipped with a warning so the returned
// DAG never fails registration; the round still executes each unique member
// once.
func ParallelAgentRound(name string, agents []string, run func(ctx context.Context, agentID string, rc *RunCtx) error) *DAG {
	d := NewDAG(name)
	if run == nil {
		slog.Warn("workflow.parallel_round.empty", "reason", "nil runner", "dag", name)
		return d
	}
	seen := make(map[string]bool, len(agents))
	for _, agentID := range agents {
		if agentID == "" {
			slog.Warn("workflow.parallel_round.skip", "reason", "empty agent id", "dag", name)
			continue
		}
		if seen[agentID] {
			slog.Warn("workflow.parallel_round.skip", "reason", "duplicate agent", "agent", agentID, "dag", name)
			continue
		}
		seen[agentID] = true
		_ = d.AddStep(&Step{
			ID:   agentID,
			Name: agentID,
			Type: StepParallel,
			Run: func(ctx context.Context, rc *RunCtx) error {
				return run(ctx, agentID, rc)
			},
		})
	}
	return d
}