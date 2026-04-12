package pipeline

import (
	"strings"
	"testing"
)

func TestConstraintStore_Add_SeverityPrecedence(t *testing.T) {
	cs := NewConstraintStore()
	if !cs.Add(Constraint{
		Kind:       ConstraintRepeatedFailure,
		Subject:    "exec:git clone",
		Severity:   SeveritySoft,
		Resolution: ResolutionSelfReroute,
		Message:    "soft failure",
	}) {
		t.Fatal("expected first add to succeed")
	}
	cs.Add(Constraint{
		Kind:       ConstraintRepeatedFailure,
		Subject:    "exec:git clone",
		Severity:   SeverityHard,
		Resolution: ResolutionSelfReroute,
		Message:    "hard failure",
	})

	active := cs.Active()
	if len(active) != 1 {
		t.Fatalf("active len = %d, want 1", len(active))
	}
	if active[0].Severity != SeverityHard {
		t.Fatalf("severity = %q, want %q", active[0].Severity, SeverityHard)
	}
}

func TestConstraintStore_Check_BinaryMissing(t *testing.T) {
	cs := NewConstraintStore()
	cs.Add(Constraint{
		Kind:       ConstraintBinaryMissing,
		Subject:    "git",
		Severity:   SeverityHard,
		Resolution: ResolutionHumanRequired,
		Message:    "git is not installed",
	})

	blocked, constraint := cs.Check("exec", map[string]any{
		"command": "cd /tmp && git clone https://example.com/repo.git",
	})
	if !blocked || constraint == nil {
		t.Fatal("expected git command to be blocked")
	}

	blocked, _ = cs.Check("exec", map[string]any{"command": "ls -la"})
	if blocked {
		t.Fatal("did not expect unrelated exec command to be blocked")
	}
}

func TestConstraintStore_Check_SpawnBlocked(t *testing.T) {
	cs := NewConstraintStore()
	cs.Add(Constraint{
		Kind:       ConstraintCapacityExhausted,
		Subject:    "spawn.children",
		Severity:   SeverityHard,
		Resolution: ResolutionSelfReroute,
		Message:    "child limit reached",
	})

	blocked, _ := cs.Check("spawn", map[string]any{"task": "analyze"})
	if !blocked {
		t.Fatal("expected spawn to be blocked")
	}
}

func TestConstraintStore_ClearNonSticky(t *testing.T) {
	cs := NewConstraintStore()
	cs.Add(Constraint{
		Kind:       ConstraintLowSignal,
		Subject:    "https://example.com",
		Severity:   SeveritySoft,
		Resolution: ResolutionSelfReroute,
		Sticky:     false,
		Message:    "repeated identical fetches",
	})
	cs.Add(Constraint{
		Kind:       ConstraintBinaryMissing,
		Subject:    "git",
		Severity:   SeverityHard,
		Resolution: ResolutionHumanRequired,
		Sticky:     true,
		Message:    "git missing",
	})

	cs.ClearNonSticky()
	active := cs.Active()
	if len(active) != 1 {
		t.Fatalf("active len = %d, want 1", len(active))
	}
	if active[0].Kind != ConstraintBinaryMissing {
		t.Fatalf("remaining kind = %q, want %q", active[0].Kind, ConstraintBinaryMissing)
	}
}

func TestConstraintStore_ForSystemPrompt(t *testing.T) {
	cs := NewConstraintStore()
	cs.Add(Constraint{
		Kind:       ConstraintBinaryMissing,
		Subject:    "git",
		Severity:   SeverityHard,
		Resolution: ResolutionHumanRequired,
		Message:    "git is not installed in this environment",
	})

	prompt := cs.ForSystemPrompt()
	if !strings.Contains(prompt, "Active runtime constraints") {
		t.Fatalf("expected header in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "binary_missing on git") {
		t.Fatalf("expected git constraint in prompt, got %q", prompt)
	}
}
