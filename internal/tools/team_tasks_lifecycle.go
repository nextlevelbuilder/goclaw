package tools

import (
	"context"
	"fmt"
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

	ownerKey := t.manager.agentKeyFromID(ctx, agentID)
	t.manager.broadcastTeamEvent(protocol.EventTeamTaskClaimed, protocol.TeamTaskEventPayload{
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
	// TODO: Enable when reviewer workflow is implemented — teammate agents should
	// not complete tasks directly when a reviewer is required.
	// if ToolChannelFromCtx(ctx) == ChannelTeammate {
	// 	return ErrorResult("teammate agents cannot complete team tasks directly — results are auto-completed when delegation finishes")
	// }

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

	ownerKey := t.manager.agentKeyFromID(ctx, agentID)
	// Fetch task for TaskNumber/Subject needed by notification subscriber.
	completedTask, _ := t.manager.teamStore.GetTask(ctx, taskID)
	var taskNumber int
	var taskSubject string
	if completedTask != nil {
		taskNumber = completedTask.TaskNumber
		taskSubject = completedTask.Subject
	}
	t.manager.broadcastTeamEvent(protocol.EventTeamTaskCompleted, protocol.TeamTaskEventPayload{
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
	// TODO: Enable when reviewer workflow is implemented — teammate agents should
	// not cancel tasks directly when a reviewer is required.
	// if ToolChannelFromCtx(ctx) == ChannelTeammate {
	// 	return ErrorResult("teammate agents cannot cancel team tasks directly")
	// }

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

	t.manager.broadcastTeamEvent(protocol.EventTeamTaskCancelled, protocol.TeamTaskEventPayload{
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

	ownerKey := t.manager.agentKeyFromID(ctx, agentID)
	t.manager.broadcastTeamEvent(protocol.EventTeamTaskReviewed, protocol.TeamTaskEventPayload{
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
	// TODO: Enable when reviewer workflow is implemented — teammate agents should
	// not approve tasks directly when a reviewer is required.
	// if ToolChannelFromCtx(ctx) == ChannelTeammate {
	// 	return ErrorResult("teammate agents cannot approve team tasks")
	// }

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

	t.manager.broadcastTeamEvent(protocol.EventTeamTaskApproved, protocol.TeamTaskEventPayload{
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

	// Inject message to lead agent via mailbox
	msg := fmt.Sprintf("Task '%s' (id=%s) has been approved by the user (status: %s).", task.Subject, task.ID, newStatus)
	_ = t.manager.teamStore.SendMessage(ctx, &store.TeamMessageData{
		TeamID:      team.ID,
		FromAgentID: team.LeadAgentID,
		ToAgentID:   &team.LeadAgentID,
		Content:     msg,
		MessageType: store.TeamMessageTypeChat,
		TaskID:      &taskID,
	})

	return NewResult(fmt.Sprintf("Task %s approved (status: %s).", taskID, newStatus))
}

func (t *TeamTasksTool) executeReject(ctx context.Context, args map[string]any) *Result {
	// TODO: Enable when reviewer workflow is implemented — teammate agents should
	// not reject tasks directly when a reviewer is required.
	// if ToolChannelFromCtx(ctx) == ChannelTeammate {
	// 	return ErrorResult("teammate agents cannot reject team tasks")
	// }

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

	// Reuse CancelTask (handles unblocking dependents, guards against completed)
	if err := t.manager.teamStore.CancelTask(ctx, taskID, team.ID, reason); err != nil {
		return ErrorResult("failed to reject task: " + err.Error())
	}

	t.manager.broadcastTeamEvent(protocol.EventTeamTaskRejected, protocol.TeamTaskEventPayload{
		TeamID:    team.ID.String(),
		TaskID:    taskID.String(),
		Subject:   task.Subject,
		Status:    "cancelled",
		Reason:    reason,
		UserID:    store.UserIDFromContext(ctx),
		Channel:   ToolChannelFromCtx(ctx),
		ChatID:    ToolChatIDFromCtx(ctx),
		Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		ActorType: "agent",
		ActorID:   t.manager.agentKeyFromID(ctx, agentID),
	})

	// Inject message to lead agent via mailbox
	leadMsg := fmt.Sprintf("Task '%s' (id=%s) was rejected by the user. Reason: %s", task.Subject, task.ID, reason)
	_ = t.manager.teamStore.SendMessage(ctx, &store.TeamMessageData{
		TeamID:      team.ID,
		FromAgentID: team.LeadAgentID,
		ToAgentID:   &team.LeadAgentID,
		Content:     leadMsg,
		MessageType: store.TeamMessageTypeChat,
		TaskID:      &taskID,
	})

	return NewResult(fmt.Sprintf("Task %s rejected. Dependent tasks have been unblocked.", taskID))
}
