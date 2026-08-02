package workflowactions

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func TestServiceApplyPublishesAndDispatchesOnlyApplied(t *testing.T) {
	tenantID, teamID, workflowID, taskID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	workflow := &store.TeamWorkflowData{
		BaseModel: store.BaseModel{ID: workflowID}, TenantID: tenantID, TeamID: teamID,
		Status: store.TeamWorkflowStatusRunning, PlanRevision: 1,
	}
	tasks := []store.TeamTaskData{{
		BaseModel: store.BaseModel{ID: taskID}, TenantID: tenantID, TeamID: teamID,
		Status: store.TeamTaskStatusPending, WorkflowKind: store.TeamWorkflowTaskKindWork,
		WorkflowID: &workflowID, WorkflowStepID: "work", PlanRevision: 1,
	}}
	ctx := store.WithTenantID(context.Background(), tenantID)
	msgBus := bus.New()
	dispatcher := &workflowActionTestDispatcher{}
	storeStub := &workflowActionTestStore{workflow: workflow, tasks: tasks}
	var events []bus.Event
	msgBus.Subscribe("test", func(event bus.Event) { events = append(events, event) })
	service := New(storeStub, msgBus, dispatcher)

	for _, outcome := range []store.WorkflowActionOutcome{store.WorkflowActionConflict, store.WorkflowActionAlreadyApplied} {
		storeStub.apply = store.WorkflowActionResult{Outcome: outcome, Action: store.WorkflowActionRetryBlocked, Workflow: workflow, Tasks: tasks}
		result, err := service.Apply(ctx, workflowActionTestGuard(teamID, workflowID, taskID))
		if err != nil || result.Outcome != outcome {
			t.Fatalf("Apply outcome=%v err=%v, want %v", result.Outcome, err, outcome)
		}
	}
	if len(events) != 0 || dispatcher.calls != 0 {
		t.Fatalf("non-applied outcomes emitted events=%d dispatches=%d", len(events), dispatcher.calls)
	}

	storeStub.apply = store.WorkflowActionResult{Outcome: store.WorkflowActionApplied, Action: store.WorkflowActionRetryBlocked, Workflow: workflow, Tasks: tasks}
	result, err := service.Apply(ctx, workflowActionTestGuard(teamID, workflowID, taskID))
	if err != nil || !result.Applied() {
		t.Fatalf("applied result=%+v err=%v", result, err)
	}
	if dispatcher.calls != 1 || len(events) != 1 {
		t.Fatalf("applied event/dispatch = %d/%d, want 1/1", len(events), dispatcher.calls)
	}
	if events[0].Name != protocol.EventTeamWorkflowUpdated || events[0].TenantID != tenantID {
		t.Fatalf("event identity=%q/%s", events[0].Name, events[0].TenantID)
	}
	payload, ok := events[0].Payload.(protocol.TeamWorkflowUpdatedPayload)
	if !ok || payload.TenantID != tenantID.String() || payload.TeamID != teamID.String() || payload.WorkflowID != workflowID.String() || payload.Outcome != "applied" {
		t.Fatalf("event payload=%+v", events[0].Payload)
	}
}

func TestServiceActionPublicationAndDispatchMatrix(t *testing.T) {
	tenantID, teamID, workflowID, taskID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	ctx := store.WithTenantID(context.Background(), tenantID)
	for _, action := range store.AllWorkflowActions {
		t.Run(string(action), func(t *testing.T) {
			workflow := &store.TeamWorkflowData{BaseModel: store.BaseModel{ID: workflowID}, TenantID: tenantID, TeamID: teamID, Status: store.TeamWorkflowStatusRunning, PlanRevision: 1}
			tasks := []store.TeamTaskData{{BaseModel: store.BaseModel{ID: taskID}, TenantID: tenantID, TeamID: teamID, Status: store.TeamTaskStatusBlocked, WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowID: &workflowID, PlanRevision: 1}}
			msgBus, dispatcher := bus.New(), &workflowActionTestDispatcher{}
			storeStub := &workflowActionTestStore{workflow: workflow, tasks: tasks, team: &store.TeamData{BaseModel: store.BaseModel{ID: teamID}}}
			var events []bus.Event
			msgBus.Subscribe("matrix", func(event bus.Event) { events = append(events, event) })
			guard := workflowActionMatrixGuard(action, teamID, workflowID, taskID)
			service := New(storeStub, msgBus, dispatcher)

			for _, outcome := range []store.WorkflowActionOutcome{store.WorkflowActionConflict, store.WorkflowActionAlreadyApplied} {
				storeStub.apply = store.WorkflowActionResult{Outcome: outcome, Action: action, Workflow: workflow, Tasks: tasks}
				result, err := service.Apply(ctx, guard)
				if err != nil || result.Outcome != outcome {
					t.Fatalf("non-applied result=%+v error=%v", result, err)
				}
			}
			if len(events) != 0 || dispatcher.calls != 0 {
				t.Fatalf("non-applied %s events=%d dispatches=%d", action, len(events), dispatcher.calls)
			}

			storeStub.apply = store.WorkflowActionResult{Outcome: store.WorkflowActionApplied, Action: action, Workflow: workflow, Tasks: tasks}
			result, err := service.Apply(ctx, guard)
			if err != nil || !result.Applied() || len(events) != 1 {
				t.Fatalf("applied %s result=%+v events=%d error=%v", action, result, len(events), err)
			}
			wantDispatch := action == store.WorkflowActionRetryBlocked
			if (dispatcher.calls == 1) != wantDispatch {
				t.Fatalf("applied %s dispatches=%d want dispatch=%t", action, dispatcher.calls, wantDispatch)
			}
		})
	}
}

func workflowActionMatrixGuard(action store.WorkflowAction, teamID, workflowID, taskID uuid.UUID) store.WorkflowActionGuard {
	guard := workflowActionTestGuard(teamID, workflowID, taskID)
	guard.Action = action
	if !action.StepScoped() {
		guard.TaskID = nil
		guard.ExpectedTaskStatus = ""
	}
	return guard
}

func TestServiceWorkflowUpdatedEventSerializesWithoutSecrets(t *testing.T) {
	tenantID, teamID, workflowID := uuid.New(), uuid.New(), uuid.New()
	token := uuid.New()
	workflow := &store.TeamWorkflowData{
		BaseModel: store.BaseModel{ID: workflowID}, TenantID: tenantID, TeamID: teamID,
		Status: store.TeamWorkflowStatusRunning, PlanRevision: 4,
		CanonicalPlan: []byte(`{"secret":"plan"}`), PlanHash: "secret-hash",
		ExpansionToken: &token, FinalizeToken: &token, DeliveryToken: &token,
		OriginChatID: "secret-chat", OriginSessionKey: "secret-session",
	}
	ctx := store.WithTenantID(context.Background(), tenantID)
	msgBus := bus.New()
	var event bus.Event
	msgBus.Subscribe("test", func(got bus.Event) { event = got })
	service := New(nil, msgBus, nil)
	service.publish(ctx, store.WorkflowActionResult{
		Outcome: store.WorkflowActionApplied, Action: store.WorkflowActionRetryBlocked, Workflow: workflow,
	})
	encoded, err := json.Marshal(event.Payload)
	if err != nil {
		t.Fatal(err)
	}
	wire := strings.ToLower(string(encoded))
	for _, forbidden := range []string{"token", "lease", "canonical", "hash", "origin", "reason", "secret"} {
		if strings.Contains(wire, forbidden) {
			t.Fatalf("workflow event leaked %q: %s", forbidden, wire)
		}
	}
	var payload protocol.TeamWorkflowUpdatedPayload
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.TenantID != tenantID.String() || payload.TeamID != teamID.String() || payload.WorkflowID != workflowID.String() || payload.Action != string(store.WorkflowActionRetryBlocked) || payload.Outcome != "applied" {
		t.Fatalf("workflow event payload=%+v", payload)
	}
}

func workflowActionTestGuard(teamID, workflowID, taskID uuid.UUID) store.WorkflowActionGuard {
	agentID := uuid.New()
	return store.WorkflowActionGuard{
		Action: store.WorkflowActionRetryBlocked, TeamID: teamID, WorkflowID: workflowID,
		ExpectedStatus: store.TeamWorkflowStatusRunning, ExpectedPlanRevision: 1,
		TaskID: &taskID, ExpectedTaskStatus: store.TeamTaskStatusBlocked, Reason: "retry",
		Actor: store.WorkflowActionActor{Kind: store.WorkflowActorCoordinator, AgentID: &agentID},
	}
}

func TestAllowedActionsAuthoritativeMatrix(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	service := &Service{now: func() time.Time { return now }}
	blocked := []store.TeamTaskData{{
		Status: store.TeamTaskStatusBlocked, WorkflowKind: store.TeamWorkflowTaskKindWork, PlanRevision: 2,
	}}
	future := now.Add(time.Minute)
	past := now.Add(-time.Minute)
	token := uuid.New()

	tests := []struct {
		name     string
		workflow store.TeamWorkflowData
		tasks    []store.TeamTaskData
		want     []store.WorkflowAction
	}{
		{
			name:     "running blocked",
			workflow: store.TeamWorkflowData{Status: store.TeamWorkflowStatusRunning, PlanRevision: 2},
			tasks:    blocked,
			want:     []store.WorkflowAction{store.WorkflowActionRetryBlocked, store.WorkflowActionCancelWorkflow, store.WorkflowActionFailWorkflow},
		},
		{
			name:     "needs revision blocked",
			workflow: store.TeamWorkflowData{Status: store.TeamWorkflowStatusNeedsRevision, PlanRevision: 2},
			tasks:    blocked,
			want:     []store.WorkflowAction{store.WorkflowActionRetryBlocked, store.WorkflowActionCancelWorkflow, store.WorkflowActionFailWorkflow},
		},
		{
			name:     "running without blocker",
			workflow: store.TeamWorkflowData{Status: store.TeamWorkflowStatusRunning, PlanRevision: 2},
			want: []store.WorkflowAction{
				store.WorkflowActionCancelWorkflow,
				store.WorkflowActionFailWorkflow,
			},
		},
		{
			name:     "needs revision without blocker",
			workflow: store.TeamWorkflowData{Status: store.TeamWorkflowStatusNeedsRevision, PlanRevision: 2},
			want:     []store.WorkflowAction{store.WorkflowActionCancelWorkflow, store.WorkflowActionFailWorkflow},
		},
		{
			name:     "pending expansion unclaimed due",
			workflow: store.TeamWorkflowData{Status: store.TeamWorkflowStatusPendingExpansion, PlanRevision: 2, NextExpansionAt: &past},
			want:     []store.WorkflowAction{store.WorkflowActionRetryExpansion, store.WorkflowActionCancelWorkflow},
		},
		{
			name:     "pending expansion live claim",
			workflow: store.TeamWorkflowData{Status: store.TeamWorkflowStatusPendingExpansion, PlanRevision: 2, ExpansionToken: &token, ExpansionLeaseUntil: &future},
			want:     []store.WorkflowAction{store.WorkflowActionCancelWorkflow},
		},
		{
			name:     "terminal dead delivery",
			workflow: store.TeamWorkflowData{Status: store.TeamWorkflowStatusCompleted, PlanRevision: 2, DeliveryStatus: store.TeamWorkflowDeliveryDead},
			want:     []store.WorkflowAction{store.WorkflowActionRetryDelivery},
		},
		{
			name:     "terminal pending delivery",
			workflow: store.TeamWorkflowData{Status: store.TeamWorkflowStatusCompleted, PlanRevision: 2, DeliveryStatus: store.TeamWorkflowDeliveryPending},
			want:     []store.WorkflowAction{},
		},
		{
			name:     "terminal dead live claim",
			workflow: store.TeamWorkflowData{Status: store.TeamWorkflowStatusCompleted, PlanRevision: 2, DeliveryStatus: store.TeamWorkflowDeliveryDead, DeliveryToken: &token, DeliveryLeaseUntil: &future},
			want:     []store.WorkflowAction{},
		},
		{
			name:     "transition state",
			workflow: store.TeamWorkflowData{Status: store.TeamWorkflowStatusCancelling, PlanRevision: 2},
			want:     []store.WorkflowAction{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := service.AllowedActions(&tt.workflow, tt.tasks)
			if len(got) != len(tt.want) {
				t.Fatalf("AllowedActions() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("AllowedActions() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}
