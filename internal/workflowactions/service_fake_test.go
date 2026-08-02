package workflowactions

import (
	"context"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// workflowActionTestStore deliberately embeds the broad compatibility
// interface; tests override only the authoritative methods exercised by Service.
type workflowActionTestStore struct {
	store.TeamStore
	store.TeamWorkflowStore
	workflow *store.TeamWorkflowData
	tasks    []store.TeamTaskData
	team     *store.TeamData
	apply    store.WorkflowActionResult
	applyErr error
	getErr   error
}

func (s *workflowActionTestStore) GetWorkflow(context.Context, uuid.UUID) (*store.TeamWorkflowData, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.workflow, nil
}

func (s *workflowActionTestStore) ListWorkflowTasks(context.Context, uuid.UUID) ([]store.TeamTaskData, error) {
	return append([]store.TeamTaskData(nil), s.tasks...), nil
}

func (s *workflowActionTestStore) GetTeam(context.Context, uuid.UUID) (*store.TeamData, error) {
	return s.team, nil
}

func (s *workflowActionTestStore) ApplyWorkflowAction(context.Context, store.WorkflowActionGuard) (store.WorkflowActionResult, error) {
	return s.apply, s.applyErr
}

type workflowActionTestDispatcher struct{ calls int }

func (d *workflowActionTestDispatcher) DispatchUnblockedTasks(context.Context, uuid.UUID) {
	d.calls++
}
