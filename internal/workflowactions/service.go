package workflowactions

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ReplanRequest contains only authoritative state loaded by Service. The
// dashboard and model cannot supply a plan, hash, graph, roster, or token.
type ReplanRequest struct {
	Team          *store.TeamData
	Workflow      *store.TeamWorkflowData
	Blocked       *store.TeamTaskData
	Guard         store.WorkflowActionGuard
	CoordinatorID uuid.UUID
}

type ReplanFunc func(context.Context, ReplanRequest) (store.WorkflowActionResult, error)

type Dispatcher interface {
	DispatchUnblockedTasks(context.Context, uuid.UUID)
}

// Service centralizes workflow recovery state loading, transition dispatch,
// post-commit scheduling, and token-free invalidation events. Ingress-specific
// authorization remains with the coordinator tool and admin RPC handler.
type Service struct {
	teams      store.TeamStore
	workflows  store.TeamWorkflowStore
	bus        *bus.MessageBus
	replan     ReplanFunc
	dispatcher Dispatcher
	now        func() time.Time
}

func New(
	teams store.TeamStore,
	workflows store.TeamWorkflowStore,
	msgBus *bus.MessageBus,
	replan ReplanFunc,
	dispatcher Dispatcher,
) *Service {
	return &Service{
		teams: teams, workflows: workflows, bus: msgBus,
		replan: replan, dispatcher: dispatcher, now: time.Now,
	}
}

func (s *Service) Get(ctx context.Context, teamID, workflowID uuid.UUID) (*store.TeamWorkflowData, []store.TeamTaskData, error) {
	if s == nil || s.workflows == nil {
		return nil, nil, fmt.Errorf("workflow service is unavailable")
	}
	workflow, err := s.workflows.GetWorkflow(ctx, workflowID)
	if err != nil {
		return nil, nil, err
	}
	if workflow == nil || workflow.TeamID != teamID || workflow.TenantID != store.TenantIDFromContext(ctx) {
		return nil, nil, store.ErrTaskNotFound
	}
	tasks, err := s.workflows.ListWorkflowTasks(ctx, workflowID)
	if err != nil {
		return nil, nil, err
	}
	return workflow, tasks, nil
}

func (s *Service) Apply(ctx context.Context, guard store.WorkflowActionGuard) (store.WorkflowActionResult, error) {
	if s == nil || s.teams == nil || s.workflows == nil {
		return store.WorkflowActionResult{}, fmt.Errorf("workflow service is unavailable")
	}
	if err := guard.Validate(); err != nil {
		return store.WorkflowActionResult{}, err
	}
	workflow, tasks, err := s.Get(ctx, guard.TeamID, guard.WorkflowID)
	if err != nil {
		return store.WorkflowActionResult{}, err
	}

	var result store.WorkflowActionResult
	if guard.Action == store.WorkflowActionApplyReplan {
		if s.replan == nil {
			return store.WorkflowActionResult{}, fmt.Errorf("workflow replanner is unavailable")
		}
		blocked := findTask(tasks, guard.TaskID)
		if blocked == nil {
			return store.WorkflowActionResult{Outcome: store.WorkflowActionConflict, Action: guard.Action, Workflow: workflow, Tasks: tasks}, nil
		}
		team, err := s.teams.GetTeam(ctx, guard.TeamID)
		if err != nil {
			return store.WorkflowActionResult{}, err
		}
		result, err = s.replan(ctx, ReplanRequest{
			Team: team, Workflow: workflow, Blocked: blocked,
			Guard: guard, CoordinatorID: workflow.CoordinatorAgentID,
		})
		if err != nil {
			return store.WorkflowActionResult{}, err
		}
		// The backend replanner may detect a stale guard before opening its commit
		// transaction. Reload here so Conflict still carries the complete
		// authoritative state required by dashboard reconciliation.
		if result.Workflow != nil && len(result.Tasks) == 0 {
			currentWorkflow, currentTasks, getErr := s.Get(ctx, guard.TeamID, guard.WorkflowID)
			if getErr != nil {
				return store.WorkflowActionResult{}, getErr
			}
			result.Workflow = currentWorkflow
			result.Tasks = currentTasks
		}
	} else {
		result, err = s.workflows.ApplyWorkflowAction(ctx, guard)
		if err != nil {
			return store.WorkflowActionResult{}, err
		}
	}

	if result.Applied() {
		if (guard.Action == store.WorkflowActionRetryBlocked || guard.Action == store.WorkflowActionApplyReplan) && s.dispatcher != nil {
			s.dispatcher.DispatchUnblockedTasks(ctx, guard.TeamID)
		}
		s.publish(ctx, result)
	}
	return result, nil
}

func (s *Service) publish(ctx context.Context, result store.WorkflowActionResult) {
	if s.bus == nil || result.Workflow == nil {
		return
	}
	s.bus.Broadcast(bus.Event{
		Name:     protocol.EventTeamWorkflowUpdated,
		TenantID: store.TenantIDFromContext(ctx),
		Payload: protocol.TeamWorkflowUpdatedPayload{
			TenantID:     store.TenantIDFromContext(ctx).String(),
			TeamID:       result.Workflow.TeamID.String(),
			WorkflowID:   result.Workflow.ID.String(),
			Action:       string(result.Action),
			Status:       result.Workflow.Status,
			PlanRevision: result.Workflow.PlanRevision,
			Outcome:      result.Outcome.String(),
		},
	})
}

func findTask(tasks []store.TeamTaskData, taskID *uuid.UUID) *store.TeamTaskData {
	if taskID == nil {
		return nil
	}
	for i := range tasks {
		if tasks[i].ID == *taskID {
			return &tasks[i]
		}
	}
	return nil
}

// AllowedActions mirrors the store predicates using authoritative internal
// state. It exposes only action names, never the claims used to derive them.
func (s *Service) AllowedActions(workflow *store.TeamWorkflowData, tasks []store.TeamTaskData) []store.WorkflowAction {
	if workflow == nil {
		return []store.WorkflowAction{}
	}
	now := time.Now()
	if s != nil && s.now != nil {
		now = s.now()
	}
	blocked := false
	for i := range tasks {
		task := &tasks[i]
		if task.WorkflowKind == store.TeamWorkflowTaskKindWork && task.PlanRevision == workflow.PlanRevision && task.Status == store.TeamTaskStatusBlocked {
			blocked = true
			break
		}
	}

	actions := make([]store.WorkflowAction, 0, 4)
	switch workflow.Status {
	case store.TeamWorkflowStatusRunning:
		if blocked {
			actions = append(actions,
				store.WorkflowActionRetryBlocked,
				store.WorkflowActionRequestRevision,
				store.WorkflowActionCancelWorkflow,
				store.WorkflowActionFailWorkflow,
			)
		}
	case store.TeamWorkflowStatusNeedsRevision:
		if blocked {
			actions = append(actions,
				store.WorkflowActionRetryBlocked,
				store.WorkflowActionApplyReplan,
				store.WorkflowActionCancelWorkflow,
				store.WorkflowActionFailWorkflow,
			)
		}
	case store.TeamWorkflowStatusPendingExpansion:
		live := workflow.ExpansionToken != nil && workflow.ExpansionLeaseUntil != nil && workflow.ExpansionLeaseUntil.After(now)
		if !live {
			actions = append(actions, store.WorkflowActionRetryExpansion)
		}
		actions = append(actions, store.WorkflowActionCancelWorkflow)
	case store.TeamWorkflowStatusCompleted, store.TeamWorkflowStatusFailed, store.TeamWorkflowStatusCancelled:
		live := workflow.DeliveryToken != nil && workflow.DeliveryLeaseUntil != nil && workflow.DeliveryLeaseUntil.After(now)
		if workflow.DeliveredAt == nil && !live && workflow.DeliveryStatus == store.TeamWorkflowDeliveryDead {
			actions = append(actions, store.WorkflowActionRetryDelivery)
		}
	}
	return actions
}
