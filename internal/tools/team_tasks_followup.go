package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func (t *TeamTasksTool) executeAskUser(ctx context.Context, args map[string]any) *Result {
	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	taskID, err := resolveTaskID(ctx, args)
	if err != nil {
		return ErrorResult(err.Error())
	}

	followupMessage, _ := args["text"].(string)
	if followupMessage == "" {
		return ErrorResult("text is required for ask_user action (the question for the user)")
	}

	// Verify ownership.
	task, err := t.manager.teamStore.GetTask(ctx, taskID)
	if err != nil {
		return ErrorResult("task not found: " + err.Error())
	}
	if task.TeamID != team.ID {
		return ErrorResult("task does not belong to your team")
	}
	if task.OwnerAgentID == nil || *task.OwnerAgentID != agentID {
		return ErrorResult("only the task owner can set follow-up reminders")
	}

	// Resolve delay and max from team settings.
	delayMinutes := t.manager.followupDelayMinutes(team)
	maxReminders := t.manager.followupMaxReminders(team)

	// Resolve channel: prefer task's channel, fallback to context channel.
	channel := task.Channel
	chatID := task.ChatID
	ctxChannel := ToolChannelFromCtx(ctx)
	if channel == "" || channel == ChannelDelegate || channel == ChannelSystem || channel == ChannelDashboard {
		channel = ctxChannel
		chatID = ToolChatIDFromCtx(ctx)
	}
	if channel == "" || channel == ChannelDelegate || channel == ChannelSystem || channel == ChannelDashboard {
		return ErrorResult("cannot set follow-up: no valid channel found (task has no origin channel and context channel is internal)")
	}

	followupAt := time.Now().Add(time.Duration(delayMinutes) * time.Minute)
	if err := t.manager.teamStore.SetTaskFollowup(ctx, taskID, team.ID, followupAt, maxReminders, followupMessage, channel, chatID); err != nil {
		return ErrorResult("failed to set follow-up: " + err.Error())
	}

	maxDesc := "unlimited"
	if maxReminders > 0 {
		maxDesc = fmt.Sprintf("max %d", maxReminders)
	}
	return NewResult(fmt.Sprintf("Follow-up set for task %s. First reminder in %d minutes via %s (%s).", taskID, delayMinutes, channel, maxDesc))
}

func (t *TeamTasksTool) executeClearAskUser(ctx context.Context, args map[string]any) *Result {
	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	taskID, err := resolveTaskID(ctx, args)
	if err != nil {
		return ErrorResult(err.Error())
	}

	// Verify task belongs to team.
	task, err := t.manager.teamStore.GetTask(ctx, taskID)
	if err != nil {
		return ErrorResult("task not found: " + err.Error())
	}
	if task.TeamID != team.ID {
		return ErrorResult("task does not belong to your team")
	}
	// Allow owner or lead to clear.
	if task.OwnerAgentID == nil || (*task.OwnerAgentID != agentID && agentID != team.LeadAgentID) {
		return ErrorResult("only the task owner or team lead can clear follow-up reminders")
	}

	if err := t.manager.teamStore.ClearTaskFollowup(ctx, taskID); err != nil {
		return ErrorResult("failed to clear follow-up: " + err.Error())
	}

	return NewResult(fmt.Sprintf("Follow-up reminders cleared for task %s.", taskID))
}

func (t *TeamTasksTool) executeRetry(ctx context.Context, args map[string]any) *Result {
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

	task, err := t.manager.teamStore.GetTask(ctx, taskID)
	if err != nil {
		return ErrorResult("task not found: " + err.Error())
	}
	if task.TeamID != team.ID {
		return ErrorResult("task does not belong to your team")
	}
	if task.Status != store.TeamTaskStatusStale && task.Status != store.TeamTaskStatusFailed {
		return ErrorResult(fmt.Sprintf("retry only works on stale or failed tasks (current status: %s)", task.Status))
	}
	if task.OwnerAgentID == nil {
		return ErrorResult("task has no assignee — assign it first via update")
	}

	// Reset status to pending first (AssignTask only transitions from pending).
	if err := t.manager.teamStore.ResetTaskStatus(ctx, taskID, team.ID); err != nil {
		return ErrorResult("failed to reset task: " + err.Error())
	}
	// Assign (pending → in_progress + lock).
	if err := t.manager.teamStore.AssignTask(ctx, taskID, *task.OwnerAgentID, team.ID); err != nil {
		return ErrorResult("failed to retry task: " + err.Error())
	}

	t.manager.broadcastTeamEvent(protocol.EventTeamTaskDispatched, protocol.TeamTaskEventPayload{
		TeamID:        team.ID.String(),
		TaskID:        taskID.String(),
		TaskNumber:    task.TaskNumber,
		Subject:       task.Subject,
		Status:        store.TeamTaskStatusInProgress,
		OwnerAgentKey: t.manager.agentKeyFromID(ctx, *task.OwnerAgentID),
		UserID:        store.UserIDFromContext(ctx),
		Channel:       ToolChannelFromCtx(ctx),
		ChatID:        ToolChatIDFromCtx(ctx),
		Timestamp:     time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		ActorType:     "agent",
		ActorID:       t.manager.agentKeyFromID(ctx, agentID),
	})

	// Dispatch immediately (retry is an explicit action, not during a turn).
	t.manager.dispatchTaskToAgent(ctx, task, team.ID, *task.OwnerAgentID)

	return NewResult(fmt.Sprintf("Task %s retried and dispatched to %s.", taskID, t.manager.agentKeyFromID(ctx, *task.OwnerAgentID)))
}
