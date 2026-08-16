package agent

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
)

func newRunState() *pipeline.RunState {
	return &pipeline.RunState{
		RunID: "run-1",
	}
}

// L0: content present and no tool usage → complete.
func TestVerifyCompletionL0ContentOnlyCompletes(t *testing.T) {
	s := newRunState()
	s.Observe.FinalContent = "Here is your answer."
	got := verifyCompletion(&RunResult{}, s)
	if !got.Complete {
		t.Fatalf("Complete = false, want true (content-only run must complete): %+v", got)
	}
	if got.Confidence != 1.0 {
		t.Errorf("Confidence = %v, want 1.0", got.Confidence)
	}
	if len(got.Missing) != 0 {
		t.Errorf("Missing = %v, want empty", got.Missing)
	}
	if got.Reason != "complete" {
		t.Errorf("Reason = %q, want %q", got.Reason, "complete")
	}
}

// L0: no content, no tool calls → incomplete with content missing.
func TestVerifyCompletionL0EmptyOutputIncomplete(t *testing.T) {
	s := newRunState()
	got := verifyCompletion(&RunResult{}, s)
	if got.Complete {
		t.Fatalf("Complete = true, want false for empty run: %+v", got)
	}
	if len(got.Missing) == 0 {
		t.Fatal("Missing empty, want content signal")
	}
	if got.Confidence >= 1.0 {
		t.Errorf("Confidence = %v, want < 1.0 for incomplete run", got.Confidence)
	}
}

// L0: tool calls without a deliverable → incomplete (premature completion).
func TestVerifyCompletionL0ToolCallsMissingDeliverable(t *testing.T) {
	s := newRunState()
	s.Observe.FinalContent = "I called tools but nothing to show."
	s.Tool.TotalToolCalls = 3
	got := verifyCompletion(&RunResult{}, s)
	if got.Complete {
		t.Fatalf("Complete = true, want false when tools used but no deliverable: %+v", got)
	}
	found := false
	for _, m := range got.Missing {
		if m == "deliverable" {
			found = true
		}
	}
	if !found {
		t.Errorf("Missing = %v, want deliverable signal", got.Missing)
	}
	// Content present, only the L1 deliverable signal missing → 0.5.
	if got.Confidence != 0.5 {
		t.Errorf("Confidence = %v, want 0.5 (L1-only gap)", got.Confidence)
	}
}

// L1: tools used + deliverable present → complete.
func TestVerifyCompletionL1ToolCallsWithDeliverableCompletes(t *testing.T) {
	s := newRunState()
	s.Observe.FinalContent = "Done."
	s.Tool.TotalToolCalls = 1
	s.Tool.Deliverables = []string{"path/to/file.md"}
	got := verifyCompletion(&RunResult{}, s)
	if !got.Complete {
		t.Fatalf("Complete = false, want true: %+v", got)
	}
	if got.Confidence != 1.0 {
		t.Errorf("Confidence = %v, want 1.0", got.Confidence)
	}
}

// L0: no content at all → confidence 0.25 (missing content dominates).
func TestVerifyCompletionMultipleMissingSignalsLowConfidence(t *testing.T) {
	s := newRunState()
	got := verifyCompletion(&RunResult{}, s)
	if got.Confidence != 0.25 {
		t.Errorf("Confidence = %v, want 0.25 for missing content", got.Confidence)
	}
	if !hasMissing(got.Missing, "content") || !hasMissing(got.Missing, "tool_calls") {
		t.Errorf("Missing = %v, want content + tool_calls signals", got.Missing)
	}
}

func hasMissing(missing []string, want string) bool {
	for _, m := range missing {
		if m == want {
			return true
		}
	}
	return false
}

// nil state is handled without panic.
func TestVerifyCompletionNilState(t *testing.T) {
	got := verifyCompletion(&RunResult{}, nil)
	if got.Complete {
		t.Fatalf("Complete = true, want false for nil state: %+v", got)
	}
	if len(got.Missing) == 0 {
		t.Fatal("Missing empty, want signals for nil state")
	}
}

// Loop-killed runs (forced stop) are never complete, even with a canned
// apology in FinalContent.
func TestVerifyCompletionLoopKilledIncomplete(t *testing.T) {
	s := newRunState()
	s.Tool.LoopKilled = true
	s.Observe.FinalContent = "I was unable to complete this task."
	s.Tool.TotalToolCalls = 5
	got := verifyCompletion(&RunResult{}, s)
	if got.Complete {
		t.Fatalf("Complete = true, want false for loop-killed run: %+v", got)
	}
	if got.Confidence != 0.1 {
		t.Errorf("Confidence = %v, want 0.1 for loop-killed run", got.Confidence)
	}
	if !hasMissing(got.Missing, "loop_killed") {
		t.Errorf("Missing = %v, want loop_killed signal", got.Missing)
	}
}
