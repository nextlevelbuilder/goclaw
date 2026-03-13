package methods

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// --- Task Approve ---

type teamsTaskApproveParams struct {
	TaskID string `json:"task_id"`
}

func (m *TeamsMethods) handleTaskApprove(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgTeamsNotConfigured)))
		return
	}

	var params teamsTaskApproveParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}
	if params.TaskID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "task_id")))
		return
	}

	taskID, err := uuid.Parse(params.TaskID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "task_id")))
		return
	}

	// Fetch task to get team_id, subject, and verify status
	task, err := m.teamStore.GetTask(ctx, taskID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}
	if task.Status != store.TeamTaskStatusPendingApproval {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			fmt.Sprintf("task is not pending approval (current status: %s)", task.Status)))
		return
	}

	// Atomic transition: pending_approval -> pending (or blocked if blockers exist)
	if err := m.teamStore.ApproveTask(ctx, taskID, task.TeamID); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	// Re-fetch to get actual post-approval status (pending or blocked)
	updated, err := m.teamStore.GetTask(ctx, taskID)
	if err != nil {
		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"ok": true}))
	} else {
		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"task": updated}))
	}

	newStatus := store.TeamTaskStatusPending
	if updated != nil {
		newStatus = updated.Status
	}

	// Broadcast event
	if m.msgBus != nil {
		m.msgBus.Broadcast(bus.Event{
			Name: protocol.EventTeamTaskApproved,
			Payload: protocol.TeamTaskEventPayload{
				TeamID:    task.TeamID.String(),
				TaskID:    taskID.String(),
				Subject:   task.Subject,
				Status:    newStatus,
				UserID:    client.UserID(),
				Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
			},
		})
	}

	// Inject message to lead agent via mailbox
	team, err := m.teamStore.GetTeam(ctx, task.TeamID)
	if err == nil {
		msg := fmt.Sprintf("Task '%s' (id=%s) has been approved by the user (status: %s).", task.Subject, task.ID, newStatus)
		_ = m.teamStore.SendMessage(ctx, &store.TeamMessageData{
			TeamID:      task.TeamID,
			FromAgentID: team.LeadAgentID,
			ToAgentID:   &team.LeadAgentID,
			Content:     msg,
			MessageType: store.TeamMessageTypeChat,
			TaskID:      &taskID,
		})
	}
}

// --- Task Reject ---

type teamsTaskRejectParams struct {
	TaskID string `json:"task_id"`
	Reason string `json:"reason"`
}

func (m *TeamsMethods) handleTaskReject(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgTeamsNotConfigured)))
		return
	}

	var params teamsTaskRejectParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}
	if params.TaskID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "task_id")))
		return
	}

	taskID, err := uuid.Parse(params.TaskID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "task_id")))
		return
	}

	reason := params.Reason
	if reason == "" {
		reason = "Rejected by user"
	}

	// Fetch task to get team_id and subject for the lead message
	task, err := m.teamStore.GetTask(ctx, taskID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	// Reuse CancelTask (handles unblocking dependents, guards against already-completed)
	if err := m.teamStore.CancelTask(ctx, taskID, task.TeamID, reason); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"ok": true}))

	// Broadcast event
	if m.msgBus != nil {
		m.msgBus.Broadcast(bus.Event{
			Name: protocol.EventTeamTaskRejected,
			Payload: protocol.TeamTaskEventPayload{
				TeamID:    task.TeamID.String(),
				TaskID:    taskID.String(),
				Subject:   task.Subject,
				Status:    "cancelled",
				Reason:    reason,
				UserID:    client.UserID(),
				Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
			},
		})
	}

	// Inject message to lead agent via mailbox
	team, err := m.teamStore.GetTeam(ctx, task.TeamID)
	if err == nil {
		leadMsg := fmt.Sprintf("Task '%s' (id=%s) was rejected by the user. Reason: %s", task.Subject, task.ID, reason)
		_ = m.teamStore.SendMessage(ctx, &store.TeamMessageData{
			TeamID:      task.TeamID,
			FromAgentID: team.LeadAgentID,
			ToAgentID:   &team.LeadAgentID,
			Content:     leadMsg,
			MessageType: store.TeamMessageTypeChat,
			TaskID:      &taskID,
		})
	}
}
