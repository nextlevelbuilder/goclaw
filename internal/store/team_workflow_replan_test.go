package store

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func validWorkflowReplanForValidation() WorkflowReplan {
	ownerID := uuid.New()
	rootID := uuid.New()
	terminalID := uuid.New()
	plan := []byte(`{"schema_version":1,"goal":"recovery"}`)
	return WorkflowReplan{
		CanonicalPlan: plan,
		PlanHash:      fmt.Sprintf("%x", sha256.Sum256(plan)),
		Tasks: []TeamTaskData{
			{
				BaseModel:      BaseModel{ID: rootID},
				Status:         TeamTaskStatusPending,
				OwnerAgentID:   &ownerID,
				WorkflowStepID: "root",
			},
			{
				BaseModel:        BaseModel{ID: terminalID},
				Status:           TeamTaskStatusBlocked,
				OwnerAgentID:     &ownerID,
				WorkflowStepID:   "terminal",
				WorkflowTerminal: true,
				BlockedBy:        []uuid.UUID{rootID},
			},
		},
	}
}

func TestValidateWorkflowReplan(t *testing.T) {
	tests := []struct {
		name string
		edit func(*WorkflowReplan)
		want string
	}{
		{name: "invalid JSON", edit: func(r *WorkflowReplan) { r.CanonicalPlan = []byte(`{`) }, want: "not valid JSON"},
		{name: "hash mismatch", edit: func(r *WorkflowReplan) { r.PlanHash = strings.Repeat("0", 64) }, want: "hash mismatch"},
		{name: "empty tasks", edit: func(r *WorkflowReplan) { r.Tasks = nil }, want: "at least one task"},
		{name: "nil task ID", edit: func(r *WorkflowReplan) { r.Tasks[0].ID = uuid.Nil }, want: "nil ID"},
		{name: "duplicate task ID", edit: func(r *WorkflowReplan) { r.Tasks[1].ID = r.Tasks[0].ID }, want: "duplicate replacement task ID"},
		{name: "empty step ID", edit: func(r *WorkflowReplan) { r.Tasks[0].WorkflowStepID = " " }, want: "empty workflow step ID"},
		{name: "duplicate step ID", edit: func(r *WorkflowReplan) { r.Tasks[1].WorkflowStepID = r.Tasks[0].WorkflowStepID }, want: "duplicate replacement workflow step ID"},
		{name: "nil owner", edit: func(r *WorkflowReplan) { r.Tasks[0].OwnerAgentID = nil }, want: "no canonical owner"},
		{name: "zero owner", edit: func(r *WorkflowReplan) { zero := uuid.Nil; r.Tasks[0].OwnerAgentID = &zero }, want: "no canonical owner"},
		{name: "nil dependency", edit: func(r *WorkflowReplan) { r.Tasks[1].BlockedBy = []uuid.UUID{uuid.Nil} }, want: "nil dependency"},
		{name: "self dependency", edit: func(r *WorkflowReplan) { r.Tasks[1].BlockedBy = []uuid.UUID{r.Tasks[1].ID} }, want: "depends on itself"},
		{name: "unknown dependency", edit: func(r *WorkflowReplan) { r.Tasks[1].BlockedBy = []uuid.UUID{uuid.New()} }, want: "depends on unknown task"},
		{name: "duplicate dependency", edit: func(r *WorkflowReplan) { id := r.Tasks[0].ID; r.Tasks[1].BlockedBy = []uuid.UUID{id, id} }, want: "repeats dependency"},
		{name: "root blocked", edit: func(r *WorkflowReplan) { r.Tasks[0].Status = TeamTaskStatusBlocked }, want: "does not match dependencies"},
		{name: "dependent pending", edit: func(r *WorkflowReplan) { r.Tasks[1].Status = TeamTaskStatusPending }, want: "does not match dependencies"},
		{name: "no terminal", edit: func(r *WorkflowReplan) { r.Tasks[1].WorkflowTerminal = false }, want: "exactly one terminal replacement"},
		{name: "multiple terminals", edit: func(r *WorkflowReplan) { r.Tasks[0].WorkflowTerminal = true }, want: "exactly one terminal replacement"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			replan := validWorkflowReplanForValidation()
			tt.edit(&replan)
			err := ValidateWorkflowReplan(replan)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateWorkflowReplan() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestValidateWorkflowReplanAcceptsValidGraph(t *testing.T) {
	if err := ValidateWorkflowReplan(validWorkflowReplanForValidation()); err != nil {
		t.Fatalf("ValidateWorkflowReplan() error = %v", err)
	}
}
