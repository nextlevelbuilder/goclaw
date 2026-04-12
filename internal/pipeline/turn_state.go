package pipeline

import (
	"fmt"
	"strings"
)

// TurnPhase is the runtime-owned outcome state for a single turn.
// It distinguishes normal completion from honest partial/blocked closeout.
type TurnPhase string

const (
	TurnPhaseRunning    TurnPhase = "running"
	TurnPhasePartial    TurnPhase = "partial"
	TurnPhaseBlocked    TurnPhase = "blocked"
	TurnPhaseNeedsHuman TurnPhase = "needs_human"
	TurnPhaseCompleted  TurnPhase = "completed"
)

// TurnCloseoutReason explains why the runtime forced the turn to close out
// without allowing more tool calls.
type TurnCloseoutReason string

const (
	TurnCloseoutReasonReadOnlyBudgetExhausted TurnCloseoutReason = "read_only_budget_exhausted"
	TurnCloseoutReasonNoProgressLoop          TurnCloseoutReason = "no_progress_loop"
	TurnCloseoutReasonToolBudgetExhausted     TurnCloseoutReason = "tool_budget_exhausted"
	TurnCloseoutReasonConstraintNeedsHuman    TurnCloseoutReason = "constraint_needs_human"
)

// TurnState tracks runtime-owned completion and partial-outcome state.
// Agentic OS principle: completion must be a machine state, not inferred from wording.
type TurnState struct {
	Phase                TurnPhase
	CloseoutReason       TurnCloseoutReason
	ForceAnswerOnly      bool
	CloseoutHintInjected bool

	LastToolName    string
	LastObservation string
	BlockedByPolicy bool
	MissingPrereq   bool
}

// RecordToolObservation stores the latest tool evidence and derives blocker signals.
func (ts *TurnState) RecordToolObservation(toolName, content string, isError bool) {
	if toolName != "" {
		ts.LastToolName = toolName
	}
	if summary := summarizeTurnObservation(content); summary != "" {
		ts.LastObservation = summary
	}
	if !isError {
		return
	}

	lower := strings.ToLower(strings.TrimSpace(content))
	switch {
	case strings.Contains(lower, "command denied by safety policy"),
		strings.Contains(lower, "blocked by policy"),
		strings.Contains(lower, "tool blocked"),
		strings.Contains(lower, "permission denied"):
		ts.BlockedByPolicy = true
	}
	switch {
	case strings.Contains(lower, "command not found"),
		strings.Contains(lower, "not installed"),
		strings.Contains(lower, "missing"),
		strings.Contains(lower, "unavailable"),
		strings.Contains(lower, "no such file"),
		strings.Contains(lower, "not found"):
		ts.MissingPrereq = true
	}
}

// ArmCloseout marks the turn as partial/blocked and forbids further tool calls.
func (ts *TurnState) ArmCloseout(reason TurnCloseoutReason) {
	if reason == "" {
		return
	}
	if ts.CloseoutReason == "" {
		ts.CloseoutReason = reason
	}
	ts.ForceAnswerOnly = true
	if ts.Phase == TurnPhaseNeedsHuman {
		return
	}
	if ts.BlockedByPolicy || ts.MissingPrereq {
		ts.Phase = TurnPhaseBlocked
		return
	}
	if ts.Phase == "" || ts.Phase == TurnPhaseRunning || ts.Phase == TurnPhaseCompleted {
		ts.Phase = TurnPhasePartial
	}
}

func (ts *TurnState) ArmNeedsHuman(reason TurnCloseoutReason) {
	if reason == "" {
		return
	}
	if ts.CloseoutReason == "" {
		ts.CloseoutReason = reason
	}
	ts.ForceAnswerOnly = true
	ts.Phase = TurnPhaseNeedsHuman
}

// CloseoutInstruction returns the runtime instruction injected into the next LLM call.
func (ts *TurnState) CloseoutInstruction() string {
	if !ts.ForceAnswerOnly {
		return ""
	}

	reason := "More tool use is unlikely to improve the answer."
	switch ts.CloseoutReason {
	case TurnCloseoutReasonReadOnlyBudgetExhausted:
		reason = "This turn exhausted its read-only retrieval budget without producing a decisive answer."
	case TurnCloseoutReasonNoProgressLoop:
		reason = "This turn hit a no-progress loop."
	case TurnCloseoutReasonToolBudgetExhausted:
		reason = "This turn exhausted its tool-call budget."
	case TurnCloseoutReasonConstraintNeedsHuman:
		reason = "This turn hit a blocker that requires human action before it can continue."
	}

	lines := []string{
		"[System] Runtime closeout mode is active.",
		reason,
		"Do not call any more tools in this turn. Respond to the user now using only the evidence already gathered in this conversation.",
		"Be explicit about partial progress. Structure the answer as: established facts, blockers, next viable step, remaining uncertainty.",
	}
	if ts.BlockedByPolicy {
		lines = append(lines, "If a command was blocked by policy, state that plainly.")
	}
	if ts.MissingPrereq {
		lines = append(lines, "If a prerequisite is missing, name the exact missing prerequisite plainly.")
	}
	return strings.Join(lines, " ")
}

// ShouldUseCloseoutFallback decides whether finalize should replace the final content.
func (ts *TurnState) ShouldUseCloseoutFallback(content string) bool {
	if !ts.ForceAnswerOnly {
		return false
	}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" || trimmed == "..." {
		return true
	}
	if strings.HasPrefix(trimmed, "CRITICAL:") {
		return true
	}
	if strings.HasPrefix(trimmed, "I was unable to complete this task") {
		return true
	}
	return false
}

// FallbackContent builds a deterministic closeout response when the model fails
// to produce a usable answer after the runtime forced answer-only mode.
func (ts *TurnState) FallbackContent() string {
	status := "Partial result"
	if ts.Phase == TurnPhaseBlocked {
		status = "Blocked"
	}
	if ts.Phase == TurnPhaseNeedsHuman {
		status = "Needs human action"
	}

	lines := []string{fmt.Sprintf("Status: %s.", status)}

	switch ts.CloseoutReason {
	case TurnCloseoutReasonReadOnlyBudgetExhausted:
		lines = append(lines, "I exhausted the read-only retrieval budget before getting a stronger answer.")
	case TurnCloseoutReasonNoProgressLoop:
		lines = append(lines, "I detected a no-progress tool loop and stopped further tool use.")
	case TurnCloseoutReasonToolBudgetExhausted:
		lines = append(lines, "I exhausted the tool-call budget for this turn.")
	case TurnCloseoutReasonConstraintNeedsHuman:
		lines = append(lines, "I reached a blocker that requires human action before the task can continue.")
	}

	if ts.LastObservation != "" {
		lines = append(lines, "", "Latest evidence:", "```text", ts.LastObservation, "```")
	}
	if ts.BlockedByPolicy {
		lines = append(lines, "", "Blocker: at least one required command was blocked by runtime policy.")
	}
	if ts.MissingPrereq {
		lines = append(lines, "", "Blocker: at least one required prerequisite appears to be missing in the environment.")
	}

	lines = append(lines, "",
		"Next viable step: continue from the evidence above, avoid repeating the same read-only checks, and either take a mutating action or state the exact prerequisite/human input still required.",
		"Remaining uncertainty: the turn ended in forced closeout mode, so this is the best answer available from evidence already gathered.")
	return strings.Join(lines, "\n")
}

func summarizeTurnObservation(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= 600 {
		return trimmed
	}
	return trimmed[:600] + "..."
}
