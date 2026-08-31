package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestWorkflowBackgroundContextOutlivesParentAndKeepsTenant(t *testing.T) {
	tenantID := uuid.New()
	parent, cancel := context.WithCancel(store.WithTenantID(context.Background(), tenantID))
	cancel()

	ctx := workflowBackgroundContext(parent)
	if err := ctx.Err(); err != nil {
		t.Fatalf("durable workflow context inherited cancellation: %v", err)
	}
	if got := store.TenantIDFromContext(ctx); got != tenantID {
		t.Fatalf("tenant = %s, want %s", got, tenantID)
	}
}

func TestWorkflowStepResultIsUsableRejectsNonDeliverables(t *testing.T) {
	// Observed live: khanh-developer's turn trailed off into "..." without calling
	// team_tasks(action="complete") and the step was settled COMPLETED with "..."
	// as its deliverable, so the critic reviewed "..." (workflow 019f9f21).
	unusable := []string{
		"",
		"   ",
		"...",
		". . .",
		"---",
		"**",
		"NO_REPLY",
		"no_reply",
		"_NO_REPLY_",
		"OK",
		"done",
		"Đã xong",
		"Step completed",
		"Agent run ended without explicit result",
	}
	for _, in := range unusable {
		if workflowStepResultIsUsable(in) {
			t.Errorf("expected %q to be rejected as a step deliverable", in)
		}
	}

	usable := []string{
		"Đã hoàn tất và lưu bản research cấu trúc tại goclaw-competitor-positioning-research.md.",
		"Architecture doc written to workspace: tenant isolation via schema-per-tenant, RLS fallback.",
		strings.Repeat("a", minUsableStepResultRunes),
	}
	for _, in := range usable {
		if !workflowStepResultIsUsable(in) {
			t.Errorf("expected %q to be accepted as a step deliverable", in)
		}
	}
}

func TestWorkflowStepResultIsUsableIgnoresGluedNoReplyWord(t *testing.T) {
	// IsSilentReply only matches a standalone token; a real result mentioning the
	// word must not be discarded.
	in := "The handler returns NO_REPLYING for muted channels, documented in handler.go."
	if !workflowStepResultIsUsable(in) {
		t.Fatalf("glued NO_REPLY variant must stay usable: %q", in)
	}
}

// workflowFinalizeRecordingStore drives the REAL finalizeWorkflow end to end. It
// embeds the full TeamWorkflowStore interface (the whole ~36-method set compiles
// via nil-interface promotion) and overrides only the methods finalizeWorkflow
// actually calls on the finalize path:
//   - GetWorkflow returns a not-yet-finalized workflow, forcing the full claim →
//     list → commit path instead of the already-finalized delivery shortcut;
//   - ClaimWorkflowFinalization returns the workflow plus the claim token;
//   - ListWorkflowTasks returns the durable tasks;
//   - CompleteWorkflowFinalization records the committed status and result_summary
//     (the persisted delivery payload);
//   - ClaimWorkflowDelivery errors, short-circuiting the post-commit delivery
//     attempt so this test isolates what is COMMITTED, not how it is delivered.
//
// Any other method (recovery, dispatch, cancel) would panic if reached, which is
// the desired guard: finalizeWorkflow must not touch them.
type workflowFinalizeRecordingStore struct {
	store.TeamWorkflowStore

	workflow   *store.TeamWorkflowData
	tasks      []store.TeamTaskData
	claimToken uuid.UUID

	claimCalls    int
	completeCalls int
	deliveryCalls int
	gotStatus     string
	gotSummary    string
	gotToken      uuid.UUID
}

func (s *workflowFinalizeRecordingStore) GetWorkflow(_ context.Context, _ uuid.UUID) (*store.TeamWorkflowData, error) {
	return s.workflow, nil
}

func (s *workflowFinalizeRecordingStore) ClaimWorkflowFinalization(_ context.Context, _ uuid.UUID, _ time.Time) (*store.TeamWorkflowData, uuid.UUID, error) {
	s.claimCalls++
	return s.workflow, s.claimToken, nil
}

func (s *workflowFinalizeRecordingStore) ListWorkflowTasks(_ context.Context, _ uuid.UUID) ([]store.TeamTaskData, error) {
	return s.tasks, nil
}

func (s *workflowFinalizeRecordingStore) CompleteWorkflowFinalization(_ context.Context, _ uuid.UUID, finalizeToken uuid.UUID, status, resultSummary string) error {
	s.completeCalls++
	s.gotToken = finalizeToken
	s.gotStatus = status
	s.gotSummary = resultSummary
	return nil
}

func (s *workflowFinalizeRecordingStore) ClaimWorkflowDelivery(_ context.Context, _ uuid.UUID, _ time.Time) (*store.TeamWorkflowData, uuid.UUID, error) {
	s.deliveryCalls++
	return nil, uuid.Nil, errors.New("delivery disabled in finalize commit test")
}

// TestFinalizeWorkflowCommitsTerminalResultVerbatim is the Correction E happy
// path driven through the real function: a completed workflow persists the
// terminal integration task's result VERBATIM as its result_summary (the delivery
// payload), with no LLM synthesis. deps.Sched is nil, so if finalizeWorkflow
// still scheduled the retired LLM finalizer, the nil *scheduler.Scheduler would
// panic here — proving delivery is purely deterministic.
func TestFinalizeWorkflowCommitsTerminalResultVerbatim(t *testing.T) {
	workflowID := uuid.New()
	claimToken := uuid.New()
	terminal := "The integrated final answer, ready for the requester."
	rec := &workflowFinalizeRecordingStore{
		workflow: &store.TeamWorkflowData{BaseModel: store.BaseModel{ID: workflowID}, Status: store.TeamWorkflowStatusRunning},
		tasks: []store.TeamTaskData{
			{WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: false, Result: strPtr("partial research that must not be delivered")},
			{WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true, Result: strPtr(terminal)},
		},
		claimToken: claimToken,
	}
	deps := &ConsumerDeps{} // Sched nil → any Schedule call panics the test.

	finalizeWorkflow(context.Background(), deps, rec, workflowID)

	if rec.completeCalls != 1 {
		t.Fatalf("CompleteWorkflowFinalization calls = %d, want 1", rec.completeCalls)
	}
	if rec.gotToken != claimToken {
		t.Errorf("committed token = %s, want claim token %s", rec.gotToken, claimToken)
	}
	if rec.gotStatus != store.TeamWorkflowStatusCompleted {
		t.Errorf("committed status = %q, want completed", rec.gotStatus)
	}
	if rec.gotSummary != terminal {
		t.Errorf("committed result_summary = %q, want terminal result %q verbatim", rec.gotSummary, terminal)
	}
	if strings.Contains(rec.gotSummary, "partial research") {
		t.Errorf("result_summary must not splice in non-terminal results: %q", rec.gotSummary)
	}
}

// TestFinalizeWorkflowCompletedEmptyTerminalFallsBackToSummary: Correction C/E
// leave no room for an empty delivery on a completed workflow. When the terminal
// result is blank, finalizeWorkflow commits the deterministic summary instead of
// an empty string.
func TestFinalizeWorkflowCompletedEmptyTerminalFallsBackToSummary(t *testing.T) {
	workflowID := uuid.New()
	workflow := &store.TeamWorkflowData{BaseModel: store.BaseModel{ID: workflowID}, Status: store.TeamWorkflowStatusRunning}
	tasks := []store.TeamTaskData{
		{WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true, Result: strPtr("   \n\t ")},
	}
	rec := &workflowFinalizeRecordingStore{workflow: workflow, tasks: tasks, claimToken: uuid.New()}
	deps := &ConsumerDeps{}

	finalizeWorkflow(context.Background(), deps, rec, workflowID)

	if rec.gotStatus != store.TeamWorkflowStatusCompleted {
		t.Fatalf("committed status = %q, want completed", rec.gotStatus)
	}
	if strings.TrimSpace(rec.gotSummary) == "" {
		t.Fatal("completed workflow committed an empty result_summary")
	}
	want := deterministicWorkflowSummary(workflow, tasks, workflowLocale(workflow))
	if rec.gotSummary != want {
		t.Fatalf("empty-terminal fallback = %q, want deterministic summary %q", rec.gotSummary, want)
	}
}

// TestFinalizeWorkflowFailedCommitsDeterministicSummary: a failing workflow
// commits failed + the deterministic summary, which redacts any internal error
// text on the terminal task. It must not deliver the terminal result verbatim.
func TestFinalizeWorkflowFailedCommitsDeterministicSummary(t *testing.T) {
	workflowID := uuid.New()
	workflow := &store.TeamWorkflowData{BaseModel: store.BaseModel{ID: workflowID}, Status: store.TeamWorkflowStatusFailing}
	tasks := []store.TeamTaskData{
		{WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true,
			Result: strPtr("FAILED: iter 1 think: llm call: provider returned no content (9router/Huy-Minh)")},
	}
	rec := &workflowFinalizeRecordingStore{workflow: workflow, tasks: tasks, claimToken: uuid.New()}
	deps := &ConsumerDeps{}

	finalizeWorkflow(context.Background(), deps, rec, workflowID)

	if rec.gotStatus != store.TeamWorkflowStatusFailed {
		t.Fatalf("committed status = %q, want failed (failing maps to failed)", rec.gotStatus)
	}
	want := deterministicWorkflowSummary(workflow, tasks, workflowLocale(workflow))
	if rec.gotSummary != want {
		t.Fatalf("failed result_summary = %q, want deterministic summary %q", rec.gotSummary, want)
	}
	for _, leak := range []string{"9router", "Huy-Minh", "FAILED:", "iter 1 think"} {
		if strings.Contains(rec.gotSummary, leak) {
			t.Errorf("failed summary leaks internal detail %q: %q", leak, rec.gotSummary)
		}
	}
}

// TestFinalizeWorkflowCancelledCommitsDeterministicSummary: a cancelling workflow
// commits cancelled + the deterministic summary carrying the cancel reason, not a
// verbatim terminal-result delivery.
func TestFinalizeWorkflowCancelledCommitsDeterministicSummary(t *testing.T) {
	workflowID := uuid.New()
	workflow := &store.TeamWorkflowData{BaseModel: store.BaseModel{ID: workflowID}, Status: store.TeamWorkflowStatusCancelling, CancelReason: "user stopped the run"}
	tasks := []store.TeamTaskData{
		{WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true, Result: strPtr("terminal work in flight")},
	}
	rec := &workflowFinalizeRecordingStore{workflow: workflow, tasks: tasks, claimToken: uuid.New()}
	deps := &ConsumerDeps{}

	finalizeWorkflow(context.Background(), deps, rec, workflowID)

	if rec.gotStatus != store.TeamWorkflowStatusCancelled {
		t.Fatalf("committed status = %q, want cancelled", rec.gotStatus)
	}
	want := deterministicWorkflowSummary(workflow, tasks, workflowLocale(workflow))
	if rec.gotSummary != want {
		t.Fatalf("cancelled result_summary = %q, want deterministic summary %q", rec.gotSummary, want)
	}
	if !strings.Contains(rec.gotSummary, "user stopped the run") {
		t.Errorf("cancelled summary should carry the cancel reason: %q", rec.gotSummary)
	}
}

// TestWorkflowTerminalResultSelectsOnlyTerminalWork is supplemental coverage for
// the read side of Correction B: the delivery payload resolves from the ONE task
// flagged work+terminal in durable state, and never from a non-terminal or
// non-work task.
func TestWorkflowTerminalResultSelectsOnlyTerminalWork(t *testing.T) {
	terminal := "The integrated final answer."
	cases := []struct {
		name  string
		tasks []store.TeamTaskData
		want  string
	}{
		{
			"terminal work selected among many",
			[]store.TeamTaskData{
				{WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: false, Result: strPtr("step one output")},
				{WorkflowKind: "", WorkflowTerminal: false, Result: strPtr("not work kind")},
				{WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true, Result: strPtr(terminal)},
			},
			terminal,
		},
		{"non-terminal work is not terminal", []store.TeamTaskData{{WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: false, Result: strPtr("only a step")}}, ""},
		{"non-work kind ignored even if flagged terminal", []store.TeamTaskData{{WorkflowKind: "", WorkflowTerminal: true, Result: strPtr("review artifact")}}, ""},
		{"no tasks", nil, ""},
		{"terminal work with nil result", []store.TeamTaskData{{WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true}}, ""},
		{"result returned verbatim incl padding", []store.TeamTaskData{{WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true, Result: strPtr("  padded  ")}}, "  padded  "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := workflowTerminalResult(tc.tasks); got != tc.want {
				t.Errorf("workflowTerminalResult = %q, want %q", got, tc.want)
			}
		})
	}
}
