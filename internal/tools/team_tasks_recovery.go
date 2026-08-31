package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/workflowactions"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const maxWorkflowRecoveryTextRunes = 10000

type workflowRecoveryTarget struct {
	team          *store.TeamData
	agentID       uuid.UUID
	workflowStore store.TeamWorkflowStore
	workflow      *store.TeamWorkflowData
	blocked       *store.TeamTaskData
}

// resolveWorkflowRecoveryTarget validates the backend-derived recovery identity
// before any coordinator action mutates workflow state. Recovery UUIDs never come
// from model/tool args: the scheduler injects the exact blocked step into context,
// and this resolver verifies tenant, team, workflow, task, and coordinator ownership.
func (t *TeamTasksTool) resolveWorkflowRecoveryTarget(ctx context.Context) (*workflowRecoveryTarget, error) {
	recovery, ok := store.WorkflowRecoveryContextFromContext(ctx)
	if !ok || recovery.TenantID == uuid.Nil || recovery.TeamID == uuid.Nil || recovery.WorkflowID == uuid.Nil {
		return nil, fmt.Errorf("workflow recovery identity missing from run context")
	}
	if tenantID := store.TenantIDFromContext(ctx); tenantID == uuid.Nil || tenantID != recovery.TenantID {
		return nil, fmt.Errorf("workflow recovery tenant mismatch")
	}

	team, agentID, err := t.manager.ResolveTeam(ctx)
	if err != nil {
		return nil, err
	}
	if team.ID != recovery.TeamID {
		return nil, fmt.Errorf("workflow recovery team mismatch")
	}
	if err := t.manager.RequireLead(ctx, team, agentID); err != nil {
		return nil, err
	}
	workflowStore, err := t.workflowStore()
	if err != nil {
		return nil, err
	}
	workflow, err := workflowStore.GetWorkflow(ctx, recovery.WorkflowID)
	if err != nil {
		return nil, fmt.Errorf("workflow recovery target is unavailable: %w", err)
	}
	if workflow.TenantID != recovery.TenantID || workflow.TeamID != recovery.TeamID {
		return nil, fmt.Errorf("workflow recovery scope mismatch")
	}
	if workflow.CoordinatorAgentID != agentID {
		return nil, fmt.Errorf("only the canonical workflow coordinator can resolve this blocker")
	}

	blocked, err := t.manager.Store().GetTask(ctx, recovery.BlockedTaskID)
	if err != nil {
		return nil, fmt.Errorf("blocked workflow step is unavailable: %w", err)
	}
	if blocked.TenantID != recovery.TenantID || blocked.TeamID != recovery.TeamID ||
		blocked.WorkflowID == nil || *blocked.WorkflowID != recovery.WorkflowID ||
		blocked.WorkflowKind != store.TeamWorkflowTaskKindWork {
		return nil, fmt.Errorf("blocked workflow step does not match the recovery target")
	}
	if blocked.Status != store.TeamTaskStatusBlocked {
		return nil, fmt.Errorf("blocked workflow step was already resolved")
	}

	return &workflowRecoveryTarget{
		team:          team,
		agentID:       agentID,
		workflowStore: workflowStore,
		workflow:      workflow,
		blocked:       blocked,
	}, nil
}

func workflowRecoveryText(args map[string]any, fieldDescription string) (string, error) {
	text, _ := args["text"].(string)
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("text is required for %s", fieldDescription)
	}
	if len([]rune(text)) > maxWorkflowRecoveryTextRunes {
		return "", fmt.Errorf("text too long (max %d characters)", maxWorkflowRecoveryTextRunes)
	}
	return text, nil
}

func (t *TeamTasksTool) workflowRecoveryEventOptions(ctx context.Context, task *store.TeamTaskData, reason string) []TaskEventOption {
	peerKind := ""
	localKey := ""
	if task.Metadata != nil {
		peerKind, _ = task.Metadata[TaskMetaPeerKind].(string)
		localKey, _ = task.Metadata[TaskMetaLocalKey].(string)
	}
	opts := []TaskEventOption{
		WithTaskInfo(task.TaskNumber, task.Subject),
		WithReason(reason),
		WithUserID(store.ActorIDFromContext(ctx)),
		WithChannel(task.Channel),
		WithChatID(task.ChatID),
		WithPeerKind(peerKind),
		WithLocalKey(localKey),
	}
	if task.OwnerAgentID != nil {
		ownerKey := t.manager.AgentKeyFromID(ctx, *task.OwnerAgentID)
		opts = append(opts, WithOwner(ownerKey, t.manager.AgentDisplayName(ctx, ownerKey)))
	}
	return opts
}

func (t *TeamTasksTool) workflowRecoveryGuard(target *workflowRecoveryTarget, action store.WorkflowAction, reason string) store.WorkflowActionGuard {
	agentID := target.agentID
	guard := store.WorkflowActionGuard{
		Action:               action,
		TeamID:               target.team.ID,
		WorkflowID:           target.workflow.ID,
		ExpectedStatus:       target.workflow.Status,
		ExpectedPlanRevision: target.workflow.PlanRevision,
		Reason:               reason,
		Actor: store.WorkflowActionActor{
			Kind:    store.WorkflowActorCoordinator,
			AgentID: &agentID,
		},
	}
	if action.StepScoped() {
		taskID := target.blocked.ID
		guard.TaskID = &taskID
		guard.ExpectedTaskStatus = target.blocked.Status
	}
	return guard
}

func workflowActionConflictResult(action store.WorkflowAction) *Result {
	return ErrorResult(fmt.Sprintf("Workflow changed before %s could be applied. Review the current workflow state and choose one recovery action.", action))
}

func (t *TeamTasksTool) workflowActionService() *workflowactions.Service {
	provider, ok := t.manager.(interface {
		WorkflowActionService() *workflowactions.Service
	})
	if !ok {
		return nil
	}
	return provider.WorkflowActionService()
}

func (t *TeamTasksTool) applyWorkflowRecoveryAction(
	ctx context.Context,
	target *workflowRecoveryTarget,
	action store.WorkflowAction,
	reason string,
) (store.WorkflowActionResult, error) {
	guard := t.workflowRecoveryGuard(target, action, reason)
	if service := t.workflowActionService(); service != nil {
		return service.Apply(ctx, guard)
	}
	return target.workflowStore.ApplyWorkflowAction(ctx, guard)
}

func (t *TeamTasksTool) executeRetryBlocked(ctx context.Context, args map[string]any) *Result {
	instruction, err := workflowRecoveryText(args, "retry_blocked")
	if err != nil {
		return ErrorResult(err.Error())
	}
	target, err := t.resolveWorkflowRecoveryTarget(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}
	result, err := t.applyWorkflowRecoveryAction(ctx, target, store.WorkflowActionRetryBlocked, instruction)
	if err != nil {
		return ErrorResult("failed to retry blocked workflow step: " + err.Error())
	}
	if result.Conflict() {
		return workflowActionConflictResult(store.WorkflowActionRetryBlocked)
	}
	if !result.Applied() {
		return NewResult(fmt.Sprintf("Workflow step %s was already queued for retry.", target.blocked.WorkflowStepID))
	}

	actorKey := t.manager.AgentKeyFromID(ctx, target.agentID)
	t.manager.BroadcastTeamEvent(ctx, protocol.EventTeamTaskUpdated, BuildTaskEventPayload(
		target.team.ID.String(), target.blocked.ID.String(),
		store.TeamTaskStatusPending,
		"agent", actorKey,
		t.workflowRecoveryEventOptions(ctx, target.blocked, instruction)...,
	))
	if t.workflowActionService() == nil {
		if dispatcher, ok := t.manager.(PostTurnProcessor); ok {
			dispatcher.DispatchUnblockedTasks(ctx, target.team.ID)
		}
	}
	return NewResult(fmt.Sprintf("Workflow step %s queued for retry with the revised instruction.", target.blocked.WorkflowStepID))
}

func (t *TeamTasksTool) executeCancelWorkflow(ctx context.Context, args map[string]any) *Result {
	reason, err := workflowRecoveryText(args, "cancel_workflow")
	if err != nil {
		return ErrorResult(err.Error())
	}
	target, err := t.resolveWorkflowRecoveryTarget(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}
	before, err := target.workflowStore.ListWorkflowTasks(ctx, target.workflow.ID)
	if err != nil {
		return ErrorResult("failed to read workflow steps before cancellation: " + err.Error())
	}
	result, err := t.applyWorkflowRecoveryAction(ctx, target, store.WorkflowActionCancelWorkflow, reason)
	if err != nil {
		return ErrorResult("failed to cancel workflow: " + err.Error())
	}
	if result.Conflict() {
		return workflowActionConflictResult(store.WorkflowActionCancelWorkflow)
	}
	if !result.Applied() {
		return NewResult("Workflow cancellation was already accepted.")
	}
	t.publishWorkflowCancelledTasks(ctx, target, before, reason)
	return NewResult(fmt.Sprintf("Workflow cancellation accepted. Final status: %s.", result.Workflow.Status))
}

func (t *TeamTasksTool) executeFailWorkflow(ctx context.Context, args map[string]any) *Result {
	reason, err := workflowRecoveryText(args, "fail_workflow")
	if err != nil {
		return ErrorResult(err.Error())
	}
	target, err := t.resolveWorkflowRecoveryTarget(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}
	before, err := target.workflowStore.ListWorkflowTasks(ctx, target.workflow.ID)
	if err != nil {
		return ErrorResult("failed to read workflow steps before failure: " + err.Error())
	}
	result, err := t.applyWorkflowRecoveryAction(ctx, target, store.WorkflowActionFailWorkflow, reason)
	if err != nil {
		return ErrorResult("failed to fail workflow: " + err.Error())
	}
	if result.Conflict() {
		return workflowActionConflictResult(store.WorkflowActionFailWorkflow)
	}
	if !result.Applied() {
		return NewResult("Workflow failure was already accepted.")
	}
	t.publishWorkflowCancelledTasks(ctx, target, before, reason)
	return NewResult(fmt.Sprintf("Workflow failure accepted. Final status: %s.", result.Workflow.Status))
}

// publishWorkflowCancelledTasks emits authoritative refetch hints only for work
// tasks that the workflow-level transition actually moved from non-terminal to
// cancelled. The store remains the authority; a concurrent completion is omitted.
func (t *TeamTasksTool) publishWorkflowCancelledTasks(ctx context.Context, target *workflowRecoveryTarget, before []store.TeamTaskData, reason string) {
	previous := make(map[uuid.UUID]string, len(before))
	for i := range before {
		previous[before[i].ID] = before[i].Status
	}
	after, err := target.workflowStore.ListWorkflowTasks(ctx, target.workflow.ID)
	if err != nil {
		return
	}
	actorKey := t.manager.AgentKeyFromID(ctx, target.agentID)
	for i := range after {
		task := &after[i]
		if task.WorkflowKind != store.TeamWorkflowTaskKindWork || task.Status != store.TeamTaskStatusCancelled {
			continue
		}
		switch previous[task.ID] {
		case store.TeamTaskStatusCompleted, store.TeamTaskStatusFailed, store.TeamTaskStatusCancelled, store.TeamTaskStatusStale:
			continue
		}
		t.manager.BroadcastTeamEvent(ctx, protocol.EventTeamTaskCancelled, BuildTaskEventPayload(
			target.team.ID.String(), task.ID.String(),
			store.TeamTaskStatusCancelled,
			"agent", actorKey,
			t.workflowRecoveryEventOptions(ctx, task, reason)...,
		))
	}
}
