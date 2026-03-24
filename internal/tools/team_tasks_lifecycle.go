package tools

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func (t *TeamTasksTool) executeClaim(ctx context.Context, args map[string]any) *Result {
	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	taskID, err := resolveTaskID(ctx, args)
	if err != nil {
		return ErrorResult(err.Error())
	}

	if err := t.manager.teamStore.ClaimTask(ctx, taskID, agentID, team.ID); err != nil {
		return ErrorResult("failed to claim task: " + err.Error())
	}
	// Record action flag after successful store operation.
	recordTaskAction(ctx, func(f *TaskActionFlags) { f.Claimed = true })

	ownerKey := t.manager.agentKeyFromID(ctx, agentID)
	t.manager.broadcastTeamEvent(ctx, protocol.EventTeamTaskClaimed, protocol.TeamTaskEventPayload{
		TeamID:           team.ID.String(),
		TaskID:           taskID.String(),
		Status:           store.TeamTaskStatusInProgress,
		OwnerAgentKey:    ownerKey,
		OwnerDisplayName: t.manager.agentDisplayName(ctx, ownerKey),
		UserID:           store.UserIDFromContext(ctx),
		Channel:          ToolChannelFromCtx(ctx),
		ChatID:           ToolChatIDFromCtx(ctx),
		Timestamp:        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		ActorType:        "agent",
		ActorID:          ownerKey,
	})

	return NewResult(fmt.Sprintf("Task %s claimed successfully. It is now in progress.", taskID))
}

func (t *TeamTasksTool) executeComplete(ctx context.Context, args map[string]any) *Result {
	// Note: reviewer role exists but is not yet active in UI.
	// All approval flows through leader. When reviewer role is enabled,
	// restrict teammate agents from completing tasks directly.

	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	taskID, err := resolveTaskID(ctx, args)
	if err != nil {
		return ErrorResult(err.Error())
	}

	result, _ := args["result"].(string)
	if result == "" {
		return ErrorResult("result is required for complete action")
	}

	// Auto-claim if the task is still pending (saves an extra tool call).
	// ClaimTask is atomic — only one agent can succeed, others get an error.
	// Ignore claim error: task may already be in_progress (claimed by us or someone else).
	_ = t.manager.teamStore.ClaimTask(ctx, taskID, agentID, team.ID)

	if err := t.manager.teamStore.CompleteTask(ctx, taskID, team.ID, result); err != nil {
		return ErrorResult("failed to complete task: " + err.Error())
	}
	// Record action flag after successful store operation.
	recordTaskAction(ctx, func(f *TaskActionFlags) { f.Completed = true })

	ownerKey := t.manager.agentKeyFromID(ctx, agentID)
	// Fetch task for TaskNumber/Subject needed by notification subscriber.
	completedTask, _ := t.manager.teamStore.GetTask(ctx, taskID)
	var taskNumber int
	var taskSubject string
	if completedTask != nil {
		taskNumber = completedTask.TaskNumber
		taskSubject = completedTask.Subject
	}
	t.manager.broadcastTeamEvent(ctx, protocol.EventTeamTaskCompleted, protocol.TeamTaskEventPayload{
		TeamID:           team.ID.String(),
		TaskID:           taskID.String(),
		TaskNumber:       taskNumber,
		Subject:          taskSubject,
		Status:           store.TeamTaskStatusCompleted,
		OwnerAgentKey:    ownerKey,
		OwnerDisplayName: t.manager.agentDisplayName(ctx, ownerKey),
		UserID:           store.UserIDFromContext(ctx),
		Channel:          ToolChannelFromCtx(ctx),
		ChatID:           ToolChatIDFromCtx(ctx),
		Timestamp:        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		ActorType:        "agent",
		ActorID:          ownerKey,
	})

	// Dependent tasks are dispatched by the consumer after this agent's turn ends
	// (post-turn), not mid-turn. This prevents dependent tasks from completing and
	// announcing to the leader before this agent's own run finishes.

	return NewResult(fmt.Sprintf("Task %s completed. Dependent tasks will be dispatched after this turn ends.", taskID))
}

func (t *TeamTasksTool) executeCancel(ctx context.Context, args map[string]any) *Result {
	// Note: reviewer role not yet active. Cancellation goes through leader only.

	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}
	if err := t.manager.requireLead(ctx, team, agentID); err != nil {
		return ErrorResult(err.Error())
	}

	taskID, err := resolveTaskID(ctx, args)
	if err != nil {
		return ErrorResult(err.Error())
	}

	reason, _ := args["text"].(string)
	if reason == "" {
		reason = "Cancelled by agent"
	}

	// CancelTask: guards against completed tasks, unblocks dependents, transitions blocked→pending.
	if err := t.manager.teamStore.CancelTask(ctx, taskID, team.ID, reason); err != nil {
		return ErrorResult("failed to cancel task: " + err.Error())
	}

	t.manager.broadcastTeamEvent(ctx, protocol.EventTeamTaskCancelled, protocol.TeamTaskEventPayload{
		TeamID:    team.ID.String(),
		TaskID:    taskID.String(),
		Status:    store.TeamTaskStatusCancelled,
		Reason:    reason,
		UserID:    store.UserIDFromContext(ctx),
		Channel:   ToolChannelFromCtx(ctx),
		ChatID:    ToolChatIDFromCtx(ctx),
		Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		ActorType: "agent",
		ActorID:   t.manager.agentKeyFromID(ctx, agentID),
	})

	// Dependent tasks are dispatched by the consumer after this agent's turn ends (post-turn).

	return NewResult(fmt.Sprintf("Task %s cancelled. Dependent tasks will be unblocked after this turn ends.", taskID))
}

func (t *TeamTasksTool) executeReview(ctx context.Context, args map[string]any) *Result {
	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	taskID, err := resolveTaskID(ctx, args)
	if err != nil {
		return ErrorResult(err.Error())
	}

	// Verify the agent owns this task.
	task, err := t.manager.teamStore.GetTask(ctx, taskID)
	if err != nil {
		return ErrorResult("task not found: " + err.Error())
	}
	if task.TeamID != team.ID {
		return ErrorResult("task does not belong to your team")
	}
	if task.OwnerAgentID == nil || *task.OwnerAgentID != agentID {
		return ErrorResult("only the task owner can submit for review")
	}

	if err := t.manager.teamStore.ReviewTask(ctx, taskID, team.ID); err != nil {
		return ErrorResult("failed to submit for review: " + err.Error())
	}
	// Record action flag after successful store operation.
	recordTaskAction(ctx, func(f *TaskActionFlags) { f.Reviewed = true })

	ownerKey := t.manager.agentKeyFromID(ctx, agentID)
	t.manager.broadcastTeamEvent(ctx, protocol.EventTeamTaskReviewed, protocol.TeamTaskEventPayload{
		TeamID:           team.ID.String(),
		TaskID:           taskID.String(),
		Status:           store.TeamTaskStatusInReview,
		OwnerAgentKey:    ownerKey,
		OwnerDisplayName: t.manager.agentDisplayName(ctx, ownerKey),
		UserID:           store.UserIDFromContext(ctx),
		Channel:          ToolChannelFromCtx(ctx),
		ChatID:           ToolChatIDFromCtx(ctx),
		Timestamp:        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		ActorType:        "agent",
		ActorID:          ownerKey,
	})

	return NewResult(fmt.Sprintf("Task %s submitted for review.", taskID))
}

func (t *TeamTasksTool) executeApprove(ctx context.Context, args map[string]any) *Result {
	// Note: reviewer role not yet active. All approvals flow through leader or dashboard.

	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	// Only lead can approve tasks via tool (non-lead agents should not approve).
	// System/dashboard channels bypass this check (human UI approval).
	ch := ToolChannelFromCtx(ctx)
	if ch != ChannelSystem && ch != ChannelDashboard {
		if err := t.manager.requireLead(ctx, team, agentID); err != nil {
			return ErrorResult(err.Error())
		}
	}

	taskID, err := resolveTaskID(ctx, args)
	if err != nil {
		return ErrorResult(err.Error())
	}

	// Fetch task for subject (used in lead message) and team ownership check
	task, err := t.manager.teamStore.GetTask(ctx, taskID)
	if err != nil {
		return ErrorResult("task not found: " + err.Error())
	}
	if task.TeamID != team.ID {
		return ErrorResult("task does not belong to your team")
	}

	// Atomic transition: in_review -> completed
	if err := t.manager.teamStore.ApproveTask(ctx, taskID, team.ID, ""); err != nil {
		return ErrorResult("failed to approve task: " + err.Error())
	}

	// Re-fetch to get the actual post-approval status (pending or blocked)
	approved, _ := t.manager.teamStore.GetTask(ctx, taskID)
	newStatus := store.TeamTaskStatusPending
	if approved != nil {
		newStatus = approved.Status
	}

	t.manager.broadcastTeamEvent(ctx, protocol.EventTeamTaskApproved, protocol.TeamTaskEventPayload{
		TeamID:    team.ID.String(),
		TaskID:    taskID.String(),
		Subject:   task.Subject,
		Status:    newStatus,
		UserID:    store.UserIDFromContext(ctx),
		Channel:   ToolChannelFromCtx(ctx),
		ChatID:    ToolChatIDFromCtx(ctx),
		Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		ActorType: "agent",
		ActorID:   t.manager.agentKeyFromID(ctx, agentID),
	})

	// Record approval as a task comment for audit trail.
	approveMsg := fmt.Sprintf("Task approved (status: %s).", newStatus)
	_ = t.manager.teamStore.AddTaskComment(ctx, &store.TeamTaskCommentData{
		TaskID:  taskID,
		AgentID: &agentID,
		Content: approveMsg,
	})

	return NewResult(fmt.Sprintf("Task %s approved (status: %s).", taskID, newStatus))
}

func (t *TeamTasksTool) executeReject(ctx context.Context, args map[string]any) *Result {
	// Note: reviewer role not yet active. Rejections flow through leader or dashboard.

	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	// Only lead can reject tasks via tool.
	ch := ToolChannelFromCtx(ctx)
	if ch != ChannelSystem && ch != ChannelDashboard {
		if err := t.manager.requireLead(ctx, team, agentID); err != nil {
			return ErrorResult(err.Error())
		}
	}

	taskID, err := resolveTaskID(ctx, args)
	if err != nil {
		return ErrorResult(err.Error())
	}

	reason, _ := args["text"].(string)
	if reason == "" {
		reason = "Rejected by user"
	}

	// Fetch task to get subject for the lead message
	task, err := t.manager.teamStore.GetTask(ctx, taskID)
	if err != nil {
		return ErrorResult("task not found: " + err.Error())
	}
	if task.TeamID != team.ID {
		return ErrorResult("task does not belong to your team")
	}

	// Record rejection as a task comment for audit trail (before status change).
	rejectMsg := fmt.Sprintf("Task rejected. Reason: %s", reason)
	_ = t.manager.teamStore.AddTaskComment(ctx, &store.TeamTaskCommentData{
		TaskID:  taskID,
		AgentID: &agentID,
		Content: rejectMsg,
	})

	// Auto re-dispatch if task has an owner: skip RejectTask (which unblocks dependents)
	// and instead reset in_review → pending → in_progress → dispatch.
	// Dependents stay blocked until this task actually completes or circuit breaker fails it.
	if task.OwnerAgentID != nil {
		// Reset in_review → pending (ResetTaskStatus accepts in_review via store guard).
		if err := t.manager.teamStore.ResetTaskStatus(ctx, taskID, team.ID); err != nil {
			// Fallback: use RejectTask (cancels + unblocks) if reset fails.
			slog.Warn("reject: reset failed, falling back to RejectTask", "task_id", taskID, "error", err)
			if err := t.manager.teamStore.RejectTask(ctx, taskID, team.ID, reason); err != nil {
				return ErrorResult("failed to reject task: " + err.Error())
			}
			t.manager.broadcastTeamEvent(ctx, protocol.EventTeamTaskRejected, protocol.TeamTaskEventPayload{
				TeamID:    team.ID.String(),
				TaskID:    taskID.String(),
				Subject:   task.Subject,
				Status:    store.TeamTaskStatusCancelled,
				Reason:    reason,
				UserID:    store.UserIDFromContext(ctx),
				Channel:   ToolChannelFromCtx(ctx),
				ChatID:    ToolChatIDFromCtx(ctx),
				Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
				ActorType: "agent",
				ActorID:   t.manager.agentKeyFromID(ctx, agentID),
			})
			return NewResult(fmt.Sprintf("Task %s rejected (cancelled). Use retry to re-dispatch manually.", taskID))
		}
		if err := t.manager.teamStore.AssignTask(ctx, taskID, *task.OwnerAgentID, team.ID); err != nil {
			slog.Warn("reject: assign task failed", "task_id", taskID, "error", err)
			return NewResult(fmt.Sprintf("Task %s rejected but could not assign. Use retry to re-dispatch manually.", taskID))
		}
		t.manager.broadcastTeamEvent(ctx, protocol.EventTeamTaskRejected, protocol.TeamTaskEventPayload{
			TeamID:    team.ID.String(),
			TaskID:    taskID.String(),
			Subject:   task.Subject,
			Status:    store.TeamTaskStatusInProgress,
			Reason:    reason,
			UserID:    store.UserIDFromContext(ctx),
			Channel:   ToolChannelFromCtx(ctx),
			ChatID:    ToolChatIDFromCtx(ctx),
			Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
			ActorType: "agent",
			ActorID:   t.manager.agentKeyFromID(ctx, agentID),
		})
		t.manager.dispatchTaskToAgent(ctx, task, team, *task.OwnerAgentID)
		return NewResult(fmt.Sprintf("Task %s rejected and re-dispatched to %s with feedback.",
			taskID, t.manager.agentKeyFromID(ctx, *task.OwnerAgentID)))
	}

	// No owner — use RejectTask to cancel + unblock dependents.
	if err := t.manager.teamStore.RejectTask(ctx, taskID, team.ID, reason); err != nil {
		return ErrorResult("failed to reject task: " + err.Error())
	}
	t.manager.broadcastTeamEvent(ctx, protocol.EventTeamTaskRejected, protocol.TeamTaskEventPayload{
		TeamID:    team.ID.String(),
		TaskID:    taskID.String(),
		Subject:   task.Subject,
		Status:    store.TeamTaskStatusCancelled,
		Reason:    reason,
		UserID:    store.UserIDFromContext(ctx),
		Channel:   ToolChannelFromCtx(ctx),
		ChatID:    ToolChatIDFromCtx(ctx),
		Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		ActorType: "agent",
		ActorID:   t.manager.agentKeyFromID(ctx, agentID),
	})
	return NewResult(fmt.Sprintf("Task %s rejected.", taskID))
}
