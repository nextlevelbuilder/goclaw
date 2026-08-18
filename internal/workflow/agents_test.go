package workflow

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestAgentStep_BuildsSequentialStep(t *testing.T) {
	ran := false
	s := AgentStep("agent-a", func(context.Context, *RunCtx) error {
		ran = true
		return nil
	}, "dep-1", "dep-2")
	if s.ID != "agent-a" {
		t.Errorf("ID = %q, want agent-a", s.ID)
	}
	if s.Type != StepSequential {
		t.Errorf("Type = %v, want sequential", s.Type)
	}
	if s.Run == nil {
		t.Fatal("Run must not be nil")
	}
	if len(s.Deps) != 2 || s.Deps[0] != "dep-1" || s.Deps[1] != "dep-2" {
		t.Errorf("Deps = %v", s.Deps)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	rc := NewRunCtx()
	if err := s.Run(context.Background(), rc); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !ran {
		t.Error("step body did not run")
	}
}

func TestAgentStep_NoDeps(t *testing.T) {
	s := AgentStep("solo", noop())
	if len(s.Deps) != 0 {
		t.Errorf("Deps = %v, want empty", s.Deps)
	}
}

func TestParallelAgentRound_BuildsParallelDAG(t *testing.T) {
	var ran atomic.Int32
	d := ParallelAgentRound("round-1", []string{"alice", "bob"}, func(ctx context.Context, agentID string, rc *RunCtx) error {
		if ctx == nil {
			t.Error("nil context")
		}
		if rc == nil {
			t.Error("nil RunCtx")
		}
		ran.Add(1)
		return nil
	})
	if d.Name() != "round-1" {
		t.Errorf("Name = %q, want round-1", d.Name())
	}
	order, err := d.TopoOrder()
	if err != nil {
		t.Fatalf("TopoOrder: %v", err)
	}
	if len(order) != 2 {
		t.Fatalf("order = %v, want 2 steps", order)
	}
	for _, id := range []string{"alice", "bob"} {
		s := d.Step(id)
		if s == nil {
			t.Fatalf("step %q not registered", id)
		}
		if s.Type != StepParallel {
			t.Errorf("step %q Type = %v, want parallel", id, s.Type)
		}
	}
	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ran.Load() != 2 {
		t.Errorf("ran = %d, want 2", ran.Load())
	}
}

func TestParallelAgentRound_EmptyAgents(t *testing.T) {
	d := ParallelAgentRound("empty", nil, func(context.Context, string, *RunCtx) error { return nil })
	order, err := d.TopoOrder()
	if err != nil {
		t.Fatalf("TopoOrder: %v", err)
	}
	if len(order) != 0 {
		t.Errorf("order = %v, want empty round", order)
	}
	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("Run empty round: %v", err)
	}
}

func TestParallelAgentRound_DuplicatesAndEmptySkipped(t *testing.T) {
	var ran atomic.Int32
	d := ParallelAgentRound("dedupe", []string{"alice", "alice", "", "bob", "alice"}, func(context.Context, string, *RunCtx) error {
		ran.Add(1)
		return nil
	})
	order, err := d.TopoOrder()
	if err != nil {
		t.Fatalf("TopoOrder: %v", err)
	}
	if len(order) != 2 {
		t.Errorf("order = %v, want 2 unique agents", order)
	}
	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ran.Load() != 2 {
		t.Errorf("ran = %d, want 2 (duplicates + empty skipped)", ran.Load())
	}
}

func TestParallelAgentRound_HooksDownstreamJoin(t *testing.T) {
	// A downstream join step can depend on every round agent and runs after
	// all fan-out branches complete.
	d := ParallelAgentRound("with-join", []string{"alice", "bob"}, func(context.Context, string, *RunCtx) error { return nil })
	mustAdd(t, d, &Step{ID: "join", Type: StepSequential, Run: noop(), Deps: []string{"alice", "bob"}})
	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	order, err := d.TopoOrder()
	if err != nil {
		t.Fatalf("TopoOrder: %v", err)
	}
	if order[len(order)-1] != "join" {
		t.Errorf("last step = %q, want join", order[len(order)-1])
	}
}