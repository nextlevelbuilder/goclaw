package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

type workflowDispatchAcceptanceStore struct {
	store.TeamStore
	store.TeamWorkflowStore

	transition store.WorkflowTaskTransition
	err        error
	calls      int
	attempt    store.WorkflowTaskAttempt
}

func (s *workflowDispatchAcceptanceStore) AcceptWorkflowTaskAttempt(_ context.Context, attempt store.WorkflowTaskAttempt, _ time.Time) (store.WorkflowTaskTransition, error) {
	s.calls++
	s.attempt = attempt
	return s.transition, s.err
}

func workflowDispatchTestMessage(planRevision string) bus.InboundMessage {
	return bus.InboundMessage{
		TenantID: uuid.New(),
		Metadata: map[string]string{
			tools.MetaWorkflowID:           uuid.NewString(),
			tools.MetaTeamTaskID:           uuid.NewString(),
			tools.MetaTeamID:               uuid.NewString(),
			tools.MetaDispatchToken:        uuid.NewString(),
			tools.MetaWorkflowPlanRevision: planRevision,
			tools.MetaWorkflowStepID:       "draft",
		},
	}
}

func TestAcceptWorkflowDispatchAttemptRejectsInvalidPlanRevision(t *testing.T) {
	for _, revision := range []string{"", "not-a-number", "0", "-1"} {
		t.Run(revision, func(t *testing.T) {
			teamStore := &workflowDispatchAcceptanceStore{
				transition: store.WorkflowTaskTransition{Outcome: store.WorkflowMutationApplied},
			}
			if attempt, ok := acceptWorkflowDispatchAttempt(context.Background(), workflowDispatchTestMessage(revision), teamStore); ok || attempt != nil {
				t.Fatalf("acceptWorkflowDispatchAttempt(%q) = (%+v, %v), want (nil, false)", revision, attempt, ok)
			}
			if teamStore.calls != 0 {
				t.Fatalf("AcceptWorkflowTaskAttempt calls = %d, want 0", teamStore.calls)
			}
		})
	}
}

func TestAcceptWorkflowDispatchAttemptRequiresAppliedTransition(t *testing.T) {
	msg := workflowDispatchTestMessage("2")
	teamStore := &workflowDispatchAcceptanceStore{
		transition: store.WorkflowTaskTransition{Outcome: store.WorkflowMutationAlreadyApplied},
	}
	if attempt, ok := acceptWorkflowDispatchAttempt(context.Background(), msg, teamStore); ok || attempt != nil {
		t.Fatalf("already-applied attempt = (%+v, %v), want (nil, false)", attempt, ok)
	}
	if teamStore.calls != 1 {
		t.Fatalf("AcceptWorkflowTaskAttempt calls = %d, want 1", teamStore.calls)
	}
}

func TestAcceptWorkflowDispatchAttemptReturnsAppliedIdentity(t *testing.T) {
	msg := workflowDispatchTestMessage("2")
	teamStore := &workflowDispatchAcceptanceStore{
		transition: store.WorkflowTaskTransition{Outcome: store.WorkflowMutationApplied},
	}
	attempt, ok := acceptWorkflowDispatchAttempt(context.Background(), msg, teamStore)
	if !ok || attempt == nil {
		t.Fatalf("applied attempt = (%+v, %v), want non-nil true", attempt, ok)
	}
	if attempt.PlanRevision != 2 || attempt.WorkflowStep != "draft" {
		t.Fatalf("attempt identity = %+v, want revision 2 step draft", attempt)
	}
	if teamStore.attempt != *attempt {
		t.Fatalf("store attempt = %+v, returned %+v", teamStore.attempt, *attempt)
	}
}
