package tools

import (
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Settlement uses this to decide whether a TRANSIENT run failure is worth
// requeueing. Getting the cap wrong either loses work (too strict) or hangs the
// workflow forever against a permanently broken provider (too loose).
func TestWorkflowStepHasDispatchBudget(t *testing.T) {
	workflowID := uuid.New()
	task := func(count int) *store.TeamTaskData {
		return &store.TeamTaskData{
			WorkflowID: &workflowID, WorkflowKind: store.TeamWorkflowTaskKindWork, DispatchCount: count,
		}
	}
	for _, tc := range []struct {
		name string
		task *store.TeamTaskData
		want bool
	}{
		{name: "nil task defers to the dispatcher", task: nil, want: true},
		{name: "fresh task", task: task(0), want: true},
		{name: "one retry left", task: task(maxTaskDispatches - 1), want: true},
		{name: "budget exhausted", task: task(maxTaskDispatches), want: false},
		{name: "over budget", task: task(maxTaskDispatches + 5), want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := WorkflowStepHasDispatchBudget(tc.task); got != tc.want {
				t.Fatalf("WorkflowStepHasDispatchBudget = %v, want %v", got, tc.want)
			}
		})
	}
}
