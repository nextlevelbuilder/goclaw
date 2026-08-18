package workflow

import (
	"context"
	"strings"
	"testing"
)

func mustAdd(t *testing.T, d *DAG, s *Step) {
	t.Helper()
	if err := d.AddStep(s); err != nil {
		t.Fatalf("AddStep(%q): %v", s.ID, err)
	}
}

func noop() func(ctx context.Context, rc *RunCtx) error {
	return func(context.Context, *RunCtx) error { return nil }
}

func TestTopoOrder_LinearChain(t *testing.T) {
	d := NewDAG("chain")
	mustAdd(t, d, &Step{ID: "a", Run: noop()})
	mustAdd(t, d, &Step{ID: "b", Run: noop(), Deps: []string{"a"}})
	mustAdd(t, d, &Step{ID: "c", Run: noop(), Deps: []string{"b"}})

	order, err := d.TopoOrder()
	if err != nil {
		t.Fatalf("TopoOrder: %v", err)
	}
	want := []string{"a", "b", "c"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", order, want)
	}
}

func TestTopoOrder_FanInOut(t *testing.T) {
	d := NewDAG("fan")
	mustAdd(t, d, &Step{ID: "root", Run: noop()})
	mustAdd(t, d, &Step{ID: "p1", Run: noop(), Deps: []string{"root"}})
	mustAdd(t, d, &Step{ID: "p2", Run: noop(), Deps: []string{"root"}})
	mustAdd(t, d, &Step{ID: "join", Run: noop(), Deps: []string{"p1", "p2"}})

	order, err := d.TopoOrder()
	if err != nil {
		t.Fatalf("TopoOrder: %v", err)
	}
	// root first, join last; p1/p2 in registration order.
	if order[0] != "root" || order[len(order)-1] != "join" {
		t.Errorf("order = %v, want root first and join last", order)
	}
	// Every dependency must appear strictly before its dependent (topological
	// subsequence property), regardless of how ties are broken.
	idx := make(map[string]int, len(order))
	for i, id := range order {
		idx[id] = i
	}
	for _, want := range []struct {
		before, after string
	}{{"root", "p1"}, {"root", "p2"}, {"p1", "join"}, {"p2", "join"}} {
		if idx[want.before] >= idx[want.after] {
			t.Errorf("order %v: %q must precede %q", order, want.before, want.after)
		}
	}
}

func TestTopoOrder_CycleDetected(t *testing.T) {
	d := NewDAG("cycle")
	mustAdd(t, d, &Step{ID: "a", Run: noop(), Deps: []string{"b"}})
	mustAdd(t, d, &Step{ID: "b", Run: noop(), Deps: []string{"c"}})
	mustAdd(t, d, &Step{ID: "c", Run: noop(), Deps: []string{"a"}})

	_, err := d.TopoOrder()
	if err == nil {
		t.Fatal("TopoOrder: expected cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error = %q, want cycle mention", err)
	}
}

func TestTopoOrder_SelfDependency(t *testing.T) {
	d := NewDAG("self")
	mustAdd(t, d, &Step{ID: "a", Run: noop(), Deps: []string{"a"}})
	if _, err := d.TopoOrder(); err == nil {
		t.Fatal("expected self-dependency error")
	}
}

func TestTopoOrder_UnknownDependency(t *testing.T) {
	d := NewDAG("unknown")
	mustAdd(t, d, &Step{ID: "a", Run: noop(), Deps: []string{"ghost"}})
	if _, err := d.TopoOrder(); err == nil {
		t.Fatal("expected unknown-dependency error")
	}
}

func TestAddStep_Validation(t *testing.T) {
	d := NewDAG("val")
	// Empty ID.
	if err := d.AddStep(&Step{ID: "", Run: noop()}); err == nil {
		t.Error("expected error for empty ID")
	}
	// Nil Run.
	if err := d.AddStep(&Step{ID: "x", Run: nil}); err == nil {
		t.Error("expected error for nil Run")
	}
	// Duplicate ID.
	mustAdd(t, d, &Step{ID: "a", Run: noop()})
	if err := d.AddStep(&Step{ID: "a", Run: noop()}); err == nil {
		t.Error("expected duplicate ID error")
	}
}

func TestDAG_StepsAndStep(t *testing.T) {
	d := NewDAG("s")
	mustAdd(t, d, &Step{ID: "a", Name: "A", Run: noop()})
	mustAdd(t, d, &Step{ID: "b", Name: "B", Run: noop()})
	steps := d.Steps()
	if len(steps) != 2 || steps[0].ID != "a" || steps[1].ID != "b" {
		t.Errorf("Steps() = %+v", steps)
	}
	if s := d.Step("b"); s == nil || s.Name != "B" {
		t.Errorf("Step(b) = %+v", s)
	}
	if d.Step("nope") != nil {
		t.Error("Step(nope) should be nil")
	}
	if d.Name() != "s" {
		t.Errorf("Name() = %q", d.Name())
	}
}

func TestDAG_Empty(t *testing.T) {
	d := NewDAG("empty")
	if _, err := d.TopoOrder(); err != nil {
		t.Fatalf("empty TopoOrder: %v", err)
	}
	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("empty Run: %v", err)
	}
}
