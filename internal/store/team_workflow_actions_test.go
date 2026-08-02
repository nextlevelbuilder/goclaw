package store

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestWorkflowActionGuardValidateExactShape(t *testing.T) {
	teamID, workflowID, taskID, agentID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	stepGuard := WorkflowActionGuard{
		Action: WorkflowActionRetryBlocked, TeamID: teamID, WorkflowID: workflowID,
		ExpectedStatus: TeamWorkflowStatusRunning, ExpectedPlanRevision: 1,
		TaskID: &taskID, ExpectedTaskStatus: TeamTaskStatusBlocked, Reason: "retry",
		Actor: WorkflowActionActor{Kind: WorkflowActorCoordinator, AgentID: &agentID},
	}
	workflowGuard := WorkflowActionGuard{
		Action: WorkflowActionCancelWorkflow, TeamID: teamID, WorkflowID: workflowID,
		ExpectedStatus: TeamWorkflowStatusRunning, ExpectedPlanRevision: 1, Reason: "stop",
		Actor: WorkflowActionActor{Kind: WorkflowActorAdmin, UserID: "admin"},
	}
	if err := stepGuard.Validate(); err != nil {
		t.Fatalf("valid step guard rejected: %v", err)
	}
	if err := workflowGuard.Validate(); err != nil {
		t.Fatalf("valid workflow guard rejected: %v", err)
	}

	tests := []struct {
		name  string
		guard WorkflowActionGuard
	}{
		{"missing expected status", withWorkflowGuard(workflowGuard, func(g *WorkflowActionGuard) { g.ExpectedStatus = "" })},
		{"missing expected revision", withWorkflowGuard(workflowGuard, func(g *WorkflowActionGuard) { g.ExpectedPlanRevision = 0 })},
		{"step missing task", withWorkflowGuard(stepGuard, func(g *WorkflowActionGuard) { g.TaskID = nil })},
		{"step zero task", withWorkflowGuard(stepGuard, func(g *WorkflowActionGuard) { id := uuid.Nil; g.TaskID = &id })},
		{"step missing task status", withWorkflowGuard(stepGuard, func(g *WorkflowActionGuard) { g.ExpectedTaskStatus = " " })},
		{"workflow carries task", withWorkflowGuard(workflowGuard, func(g *WorkflowActionGuard) { g.TaskID = &taskID })},
		{"workflow carries task status", withWorkflowGuard(workflowGuard, func(g *WorkflowActionGuard) { g.ExpectedTaskStatus = TeamTaskStatusBlocked })},
		{"blank reason", withWorkflowGuard(workflowGuard, func(g *WorkflowActionGuard) { g.Reason = " " })},
		{"oversized reason", withWorkflowGuard(workflowGuard, func(g *WorkflowActionGuard) { g.Reason = strings.Repeat("x", MaxWorkflowActionReasonRunes+1) })},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.guard.Validate(); err != ErrWorkflowActionInvalid {
				t.Fatalf("Validate() error = %v, want ErrWorkflowActionInvalid", err)
			}
		})
	}
}

func withWorkflowGuard(guard WorkflowActionGuard, mutate func(*WorkflowActionGuard)) WorkflowActionGuard {
	mutate(&guard)
	return guard
}
