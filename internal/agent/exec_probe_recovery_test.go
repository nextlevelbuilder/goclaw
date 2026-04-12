package agent

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func TestMaybeArmReadOnlyExecProbeRecovery_EmitsConstraintOnFirstDecisiveMiss(t *testing.T) {
	loop := &Loop{id: "tester"}
	state := pipeline.NewRunState(&pipeline.RunInput{SessionKey: "session-1", RunID: "run-1"}, nil, "", nil)
	result := &tools.Result{ForLLM: "exit status 1", IsError: true}
	args := map[string]any{"command": "which git && git --version"}

	recovery := loop.maybeArmReadOnlyExecProbeRecovery(state, "exec", args, result)
	if recovery == nil {
		t.Fatal("expected recovery message on decisive missing-binary probe")
	}
	if len(result.Constraints) != 1 {
		t.Fatalf("constraint count = %d, want 1", len(result.Constraints))
	}
	if result.Constraints[0].Kind != pipeline.ConstraintBinaryMissing {
		t.Fatalf("constraint kind = %q, want %q", result.Constraints[0].Kind, pipeline.ConstraintBinaryMissing)
	}
	if result.Constraints[0].Subject != "git" {
		t.Fatalf("constraint subject = %q, want git", result.Constraints[0].Subject)
	}
	blocked, _ := state.EnsureConstraintStore().Check("exec", map[string]any{
		"command": "git clone https://example.com/repo.git",
	})
	if !blocked {
		t.Fatal("expected subsequent git exec call to be blocked")
	}
	if state.Turn.Phase != pipeline.TurnPhaseNeedsHuman {
		t.Fatalf("phase = %q, want %q", state.Turn.Phase, pipeline.TurnPhaseNeedsHuman)
	}
}

func TestMaybeArmReadOnlyExecProbeRecovery_IgnoresNonEnvExecs(t *testing.T) {
	loop := &Loop{id: "tester"}
	state := pipeline.NewRunState(&pipeline.RunInput{SessionKey: "session-1", RunID: "run-1"}, nil, "", nil)

	args := map[string]any{"command": "rg -n 'TODO' internal"}
	result := &tools.Result{ForLLM: "internal/foo.go:10:TODO"}

	if recovery := loop.maybeArmReadOnlyExecProbeRecovery(state, "exec", args, result); recovery != nil {
		t.Fatal("did not expect recovery for repo-search exec")
	}
	if len(result.Constraints) != 0 {
		t.Fatalf("constraint count = %d, want 0", len(result.Constraints))
	}
}
