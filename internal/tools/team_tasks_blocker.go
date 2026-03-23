package tools

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// handleBlockerComment auto-fails the task, cancels the member session via
// EventTeamTaskFailed broadcast, and escalates to the leader agent.
func (t *TeamTasksTool) handleBlockerComment(
	ctx context.Context,
	team *store.TeamData,
	task *store.TeamTaskData,
	taskID, agentID uuid.UUID,
	text string,
) *Result {
	// Only escalate if task is still in progress.
	if task.Status != store.TeamTaskStatusInProgress {
		return NewResult(fmt.Sprintf(
			"Blocker comment saved on task #%d \"%s\", but task is already %s — no escalation needed.",
			task.TaskNumber, task.Subject, task.Status))
	}

	// Auto-fail the task. FailTask guards with AND status='in_progress' so
	// concurrent calls are safe — only one succeeds.
	reason := "Blocked: " + text
	if len([]rune(reason)) > 500 {
		reason = string([]rune(reason)[:500])
	}
	if err := t.manager.teamStore.FailTask(ctx, taskID, team.ID, reason); err != nil {
		slog.Warn("blocker: FailTask error", "task_id", taskID, "error", err)
		// Task may have completed concurrently — not a hard error.
		return NewResult("Blocker comment saved. Task may have already completed — check task status.")
	}

	// Broadcast EventTeamTaskFailed — triggers:
	// 1. Cancel subscriber → sched.CancelSession() → member stops
	// 2. Notify subscriber → "❌ Task failed" → chat channel (direct outbound)
	// 3. WS broadcast → web UI dashboard real-time update
	memberKey := t.manager.agentKeyFromID(ctx, agentID)
	t.manager.broadcastTeamEvent(ctx, protocol.EventTeamTaskFailed, protocol.TeamTaskEventPayload{
		TeamID:        team.ID.String(),
		TaskID:        taskID.String(),
		TaskNumber:    task.TaskNumber,
		Subject:       task.Subject,
		Status:        store.TeamTaskStatusFailed,
		Reason:        reason,
		OwnerAgentKey: memberKey,
		UserID:        store.UserIDFromContext(ctx),
		Channel:       task.Channel,
		ChatID:        task.ChatID,
		Timestamp:     time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		ActorType:     "agent",
		ActorID:       memberKey,
	})

	// Escalate to leader if enabled in team settings.
	escalationCfg := ParseBlockerEscalationConfig(team.Settings)
	if escalationCfg.Enabled && t.manager.msgBus != nil {
		leadAg, err := t.manager.cachedGetAgentByID(ctx, team.LeadAgentID)
		if err == nil {
			escalationMsg := fmt.Sprintf(
				"[Escalation] Member %q is blocked on task #%d \"%s\"\n\n"+
					"Blocker: %s\n\n"+
					"Use team_tasks(action=\"retry\", task_id=\"%s\") to reopen with updated instructions.",
				memberKey, task.TaskNumber, task.Subject, text, taskID)

			if !t.manager.msgBus.TryPublishInbound(bus.InboundMessage{
				Channel:  task.Channel,
				SenderID: "system:escalation",
				ChatID:   task.ChatID,
				Content:  escalationMsg,
				UserID:   store.UserIDFromContext(ctx),
				TenantID: store.TenantIDFromContext(ctx),
				AgentID:  leadAg.AgentKey,
			}) {
				slog.Warn("blocker: inbound buffer full, escalation dropped",
					"task_id", taskID, "leader", leadAg.AgentKey)
			}
		}
	}

	return NewResult(fmt.Sprintf(
		"Task #%d \"%s\" auto-failed due to blocker. Leader has been notified and can retry with updated instructions.",
		task.TaskNumber, task.Subject))
}
