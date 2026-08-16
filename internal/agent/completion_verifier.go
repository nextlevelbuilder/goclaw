package agent

import (
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
)

// CompletionResult is the output of a run's completion verification (plan §11.2).
// It records whether the run produced a complete answer and which signals are
// missing. Phase 5 is record-only: the result is attached to run events and
// trace output, it never changes the terminal decision of the run.
type CompletionResult struct {
	Complete   bool
	Confidence float64
	Missing    []string
	Reason     string
}

// verifyCompletion inspects a finished pipeline run for completion signals:
//
//   - L0 (basic): the run produced final content.
//   - L1 (tool consistency): when the run used tools, at least one deliverable
//     was produced and every executed tool call has a recorded result (the
//     pipeline increments TotalToolCalls only after a result is processed, so
//     a nonzero count implies no pending calls).
//
// It is a deterministic, LLM-free check — no model judge, no DB writes. It
// exists to surface weak-model failure modes (empty output, premature
// completion with no deliverable) in observability, not to gate the run.
func verifyCompletion(r *RunResult, s *pipeline.RunState) CompletionResult {
	if s == nil {
		return CompletionResult{
			Complete:   false,
			Confidence: 0,
			Missing:    []string{"content", "tool_state"},
			Reason:     "no run state available",
		}
	}

	content := strings.TrimSpace(s.Observe.FinalContent)
	toolCalls := s.Tool.TotalToolCalls
	deliverables := len(s.Tool.Deliverables)

	var missing []string
	// L0: final content must exist. A loop-killed run has a canned apology
	// string in FinalContent, so the loop kill is reported as missing content
	// only when nothing was actually produced.
	if content == "" {
		missing = append(missing, "content")
	}
	// L1: a run that used tools should also have produced a deliverable.
	// Chat-only runs (no tools) complete with content alone.
	if toolCalls > 0 && deliverables == 0 {
		missing = append(missing, "deliverable")
	}
	// L1: a run with no tools and no content has no completion signal at all.
	if toolCalls == 0 && content == "" && !s.Tool.LoopKilled {
		missing = append(missing, "tool_calls")
	}
	// A run force-stopped by the loop detector is never complete.
	if s.Tool.LoopKilled {
		missing = append(missing, "loop_killed")
	}

	complete := len(missing) == 0

	// Confidence blends the two levels: full when both pass; 0.5 when only the
	// L1 deliverable signal is missing (content present); 0.25 when L0 content
	// itself is missing; 0.1 when the run was force-stopped by the detector.
	confidence := 1.0
	if !complete {
		switch {
		case s.Tool.LoopKilled:
			confidence = 0.1
		case content == "":
			confidence = 0.25
		default:
			confidence = 0.5
		}
	}

	reason := "complete"
	if !complete {
		reason = "incomplete: missing " + strings.Join(missing, ", ")
	}

	return CompletionResult{
		Complete:   complete,
		Confidence: confidence,
		Missing:    missing,
		Reason:     reason,
	}
}
