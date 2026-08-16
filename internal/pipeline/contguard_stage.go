package pipeline

import (
	"context"
	"log/slog"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/reliability"
)

// ContinuationGate is an opt-in stage that guards against premature completion
// by weak models: when ThinkStage breaks the loop with a final answer before
// any tool iteration ran, and the run carries no deliverable output, the gate
// requests one more iteration (via the existing ContinueAfterFinal mechanism in
// pipeline.Run) so the model gets a chance to actually do the work.
//
// The gate never fires when:
//   - the feature is disabled (default; opt-in via reliability config),
//   - the final answer carries content (a real reply exists),
//   - the run has deliverable output (media, forwarded media, content suffix),
//   - the final iteration is in progress (no iteration left to continue with),
//   - ContinueAfterFinal was just used (the previous continuation did not help),
//   - ThinkStage's bounded empty-reply nudge is still in flight.
//
// Implemented on top of the existing ContinueAfterFinal flow
// (observe_stage.go + pipeline.go exit handling) — no new control mechanism.
type ContinuationGate struct {
	deps   *PipelineDeps
	result StageResult
}

// NewContinuationGate creates a ContinuationGate.
func NewContinuationGate(deps *PipelineDeps) *ContinuationGate {
	return &ContinuationGate{deps: deps, result: Continue}
}

func (s *ContinuationGate) Name() string        { return "continuation-gate" }
func (s *ContinuationGate) Result() StageResult { return s.result }

// Enabled reports whether the premature-completion gate is active. Default
// disabled (opt-in): reads reliability.premature_completion.enabled through the
// reliability runtime. A zero/nil config (disabled = 0) keeps the gate off in
// every build where the config field is not yet wired.
func (s *ContinuationGate) Enabled() bool {
	if r := reliability.Default(); r != nil {
		return r.PrematureCompletion.Enabled
	}
	return false
}

// Execute inspects the iteration after ThinkStage. When the model produced a
// final answer (no tool calls) with no tool iterations used and no deliverable
// output, it flips ContinueAfterFinal so the pipeline runs one more iteration.
func (s *ContinuationGate) Execute(_ context.Context, state *RunState) error {
	s.result = Continue

	if !s.Enabled() {
		return nil
	}

	// Only consider the final-answer path: ThinkStage must have broken the loop
	// (no tool calls). Tool iterations end with Continue from ThinkStage.
	if state.Think.LastResponse == nil || len(state.Think.LastResponse.ToolCalls) > 0 {
		return nil
	}

	// The model already wrote a real reply — nothing to continue for.
	if strings.TrimSpace(state.Think.LastResponse.Content) != "" {
		return nil
	}

	// Deliverable output (media / forwarded media / content suffix) can carry
	// the reply without a text message — mirrors hasDeliverableOutput semantics.
	if s.hasDeliverableOutput(state) {
		return nil
	}

	// Bound: do not continue on the final iteration (a nudge would never be
	// answered) and never re-fire when the gate already asked once this run —
	// the continuation did not help, keep the run finite.
	maxIter := s.deps.Config.MaxIterations
	if state.Iteration+1 >= maxIter || state.Observe.ContinuationGateFired {
		return nil
	}

	// Bounded empty-reply nudges from ThinkStage still have budget: the model
	// is already being asked to answer; leave the nudge flow alone.
	if state.Think.EmptyReplyRetries < maxEmptyReplyRetries {
		return nil
	}

	// Zero tool iterations = the run did nothing. Ask for one more turn.
	if state.Tool.TotalToolCalls > 0 {
		return nil
	}

	state.Observe.ContinueAfterFinal = true
	state.Observe.ContinuationGateFired = true
	slog.Info("continuation_gate.fired",
		"run_id", state.RunID,
		"iteration", state.Iteration,
		"empty_reply_retries", state.Think.EmptyReplyRetries,
	)
	return nil
}

// hasDeliverableOutput mirrors ThinkStage's deliverable check for the gate.
func (s *ContinuationGate) hasDeliverableOutput(state *RunState) bool {
	if state.Input == nil {
		return false
	}
	return len(state.Tool.MediaResults) > 0 ||
		len(state.Input.ForwardMedia) > 0 ||
		state.Input.ContentSuffix != ""
}
