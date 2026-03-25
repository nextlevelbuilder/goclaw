package methods

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func (m *TeamsMethods) handleTaskCreate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params taskCreateParams
	locale, ok := m.parseTaskParams(ctx, client, req, &params)
	if !ok {
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "teamId")))
		return
	}

	if params.Subject == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "subject")))
		return
	}
	if len(params.Subject) > 500 {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "subject too long"))
		return
	}
	if len(params.Description) > maxCommentLength {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "description too long"))
		return
	}

	taskType := params.TaskType
	if taskType == "" {
		taskType = "general"
	}

	ch := params.Channel
	if ch == "" {
		ch = "dashboard"
	}
	cid := params.ChatID
	if cid == "" {
		cid = teamID.String()
	}

	task := &store.TeamTaskData{
		TeamID:      teamID,
		Subject:     params.Subject,
		Description: params.Description,
		Status:      store.TeamTaskStatusPending,
		Priority:    params.Priority,
		TaskType:    taskType,
		UserID:      client.UserID(),
		Channel:     ch,
		ChatID:      cid,
	}

	if err := m.teamStore.CreateTask(ctx, task); err != nil {
		slog.Warn("teams.tasks.create failed", "team_id", teamID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		return
	}

	// Auto-assign only when user explicitly specifies an agent.
	// Unassigned tasks stay pending (backlog) — user assigns via UI when ready.
	var autoAssignedAgentID uuid.UUID
	if params.AssignTo != "" {
		agentID, err := uuid.Parse(params.AssignTo)
		if err == nil {
			if err := m.teamStore.AssignTask(ctx, task.ID, agentID, teamID); err != nil {
				slog.Warn("teams.tasks.create auto-assign failed", "task_id", task.ID, "agent_id", agentID, "error", err)
			} else {
				task.Status = store.TeamTaskStatusInProgress
				task.OwnerAgentID = &agentID
				autoAssignedAgentID = agentID
			}
		}
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"task": task}))

	if m.msgBus != nil {
		m.msgBus.Broadcast(taskBusEvent(protocol.EventTeamTaskCreated, protocol.TeamTaskEventPayload{
			TeamID:     teamID.String(),
			TaskID:     task.ID.String(),
			TaskNumber: task.TaskNumber,
			Subject:    task.Subject,
			Status:     task.Status,
			UserID:     client.UserID(),
			Channel:    ch,
			ChatID:     cid,
			Timestamp:  taskNowUTC(),
			ActorType:  "human",
			ActorID:    client.UserID(),
		}))

		if autoAssignedAgentID != uuid.Nil {
			m.msgBus.Broadcast(taskBusEvent(protocol.EventTeamTaskAssigned, protocol.TeamTaskEventPayload{
				TeamID:        teamID.String(),
				TaskID:        task.ID.String(),
				Status:        store.TeamTaskStatusInProgress,
				OwnerAgentKey: autoAssignedAgentID.String(),
				UserID:        client.UserID(),
				Channel:       ch,
				ChatID:        cid,
				Timestamp:     taskNowUTC(),
				ActorType:     "human",
				ActorID:       client.UserID(),
			}))

			// Dispatch to assigned agent.
			m.dispatchTaskToAgent(ctx, task, task.ID, teamID, autoAssignedAgentID, client.UserID())
		}
	}
}

// --- Task Assign ---

type taskAssignParams struct {
	TeamID  string `json:"teamId"`
	TaskID  string `json:"taskId"`
	AgentID string `json:"agentId"`
}

func (m *TeamsMethods) handleTaskAssign(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params taskAssignParams
	locale, ok := m.parseTaskParams(ctx, client, req, &params)
	if !ok {
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "teamId")))
		return
	}
	taskID, err := uuid.Parse(params.TaskID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "taskId")))
		return
	}
	agentID, err := uuid.Parse(params.AgentID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "agentId")))
		return
	}

	// Validate task belongs to team (prevent IDOR).
	task, err := m.teamStore.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, store.ErrTaskNotFound) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "task", "")))
		} else {
			slog.Warn("teams.tasks.assign get failed", "task_id", taskID, "error", err)
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		}
		return
	}
	if task.TeamID != teamID {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "task", "")))
		return
	}

	if err := m.teamStore.AssignTask(ctx, taskID, agentID, teamID); err != nil {
		slog.Warn("teams.tasks.assign failed", "task_id", taskID, "agent_id", agentID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"ok": true}))

	if m.msgBus != nil {
		m.msgBus.Broadcast(taskBusEvent(protocol.EventTeamTaskAssigned, protocol.TeamTaskEventPayload{
			TeamID:    teamID.String(),
			TaskID:    taskID.String(),
			Status:    store.TeamTaskStatusInProgress,
			UserID:    client.UserID(),
			Channel:   task.Channel,
			ChatID:    task.ChatID,
			Timestamp: taskNowUTC(),
			ActorType: "human",
			ActorID:   client.UserID(),
		}))

		// Dispatch task to the assigned agent via message bus so the consumer
		// routes it through the agent loop.
		m.dispatchTaskToAgent(ctx, task, taskID, teamID, agentID, client.UserID())
	}
}

// --- Task Delete (hard-delete terminal-status tasks) ---

type taskDeleteParams struct {
	TeamID string `json:"teamId"`
	TaskID string `json:"taskId"`
}

func (m *TeamsMethods) handleTaskDelete(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params taskDeleteParams
	locale, ok := m.parseTaskParams(ctx, client, req, &params)
	if !ok {
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "teamId")))
		return
	}
	taskID, err := uuid.Parse(params.TaskID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "taskId")))
		return
	}

	// Validate task belongs to team (prevent IDOR).
	task, err := m.teamStore.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, store.ErrTaskNotFound) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "task", "")))
		} else {
			slog.Warn("teams.tasks.delete get failed", "task_id", taskID, "error", err)
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		}
		return
	}
	if task.TeamID != teamID {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "task", "")))
		return
	}

	if err := m.teamStore.DeleteTask(ctx, taskID, teamID); err != nil {
		if errors.Is(err, store.ErrTaskNotFound) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "task is not in a deletable state"))
		} else {
			slog.Warn("teams.tasks.delete failed", "task_id", taskID, "error", err)
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		}
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"ok": true}))

	if m.msgBus != nil {
		m.msgBus.Broadcast(taskBusEvent(protocol.EventTeamTaskDeleted, protocol.TeamTaskEventPayload{
			TeamID:    teamID.String(),
			TaskID:    taskID.String(),
			Status:    task.Status,
			UserID:    client.UserID(),
			Channel:   "dashboard",
			Timestamp: taskNowUTC(),
			ActorType: "human",
			ActorID:   client.UserID(),
		}))
	}
}

// --- Task Delete Bulk (hard-delete multiple terminal-status tasks) ---

type taskDeleteBulkParams struct {
	TeamID  string   `json:"teamId"`
	TaskIDs []string `json:"taskIds"`
}

func (m *TeamsMethods) handleTaskDeleteBulk(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params taskDeleteBulkParams
	locale, ok := m.parseTaskParams(ctx, client, req, &params)
	if !ok {
		return
	}

	teamID, err := uuid.Parse(params.TeamID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "teamId")))
		return
	}
	if len(params.TaskIDs) == 0 {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "taskIds is required"))
		return
	}

	taskUUIDs := make([]uuid.UUID, 0, len(params.TaskIDs))
	for _, raw := range params.TaskIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			continue // skip invalid IDs
		}
		taskUUIDs = append(taskUUIDs, id)
	}
	if len(taskUUIDs) == 0 {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "no valid taskIds"))
		return
	}

	deleted, err := m.teamStore.DeleteTasks(ctx, taskUUIDs, teamID)
	if err != nil {
		slog.Warn("teams.tasks.delete-bulk failed", "team_id", teamID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"deleted": len(deleted),
	}))

	// Broadcast delete event per task for real-time UI sync.
	if m.msgBus != nil {
		for _, id := range deleted {
			m.msgBus.Broadcast(taskBusEvent(protocol.EventTeamTaskDeleted, protocol.TeamTaskEventPayload{
				TeamID:    teamID.String(),
				TaskID:    id.String(),
				UserID:    client.UserID(),
				Channel:   "dashboard",
				Timestamp: taskNowUTC(),
				ActorType: "human",
				ActorID:   client.UserID(),
			}))
		}
	}
}

// dispatchTaskToAgent publishes a teammate-style inbound message so the
// gateway consumer picks it up and runs the assigned agent, then auto-completes
// the task on success or auto-fails on error.
func (m *TeamsMethods) dispatchTaskToAgent(ctx context.Context, task *store.TeamTaskData, taskID, teamID, agentID uuid.UUID, userID string) {
	// Block dispatch to the lead agent — causes dual-session loop.
	if team, err := m.teamStore.GetTeam(ctx, teamID); err == nil && team != nil && agentID == team.LeadAgentID {
		slog.Warn("teams.tasks.dispatch: blocked dispatch to lead agent",
			"task_id", taskID, "agent_id", agentID, "team_id", teamID)
		_ = m.teamStore.UpdateTask(ctx, taskID, map[string]any{
			"status": store.TeamTaskStatusFailed,
			"result": "Cannot dispatch task to the team lead — reassign to a team member",
		})
		return
	}

	ag, err := m.agentStore.GetByID(ctx, agentID)
	if err != nil {
		slog.Warn("teams.tasks.dispatch: cannot resolve agent", "agent_id", agentID, "error", err)
		return
	}

	// Build task prompt for the agent.
	content := fmt.Sprintf("[Assigned task #%d (id: %s)]: %s", task.TaskNumber, task.ID, task.Subject)
	if task.Description != "" {
		content += "\n\n" + task.Description
	}

	// Use the task's original channel/chat so completion announcements route
	// back to the user's real channel (e.g. Telegram) instead of void "dashboard".
	originChannel := task.Channel
	if originChannel == "" {
		originChannel = "dashboard"
	}
	fromAgent := "dashboard"
	if team, err := m.teamStore.GetTeam(ctx, teamID); err == nil && team != nil {
		if leadAg, err := m.agentStore.GetByID(ctx, team.LeadAgentID); err == nil {
			fromAgent = leadAg.AgentKey
		}
	}

	// Resolve peer kind from task metadata; fallback to "direct" for old tasks.
	originPeerKind := "direct"
	if task.Metadata != nil {
		if pk, ok := task.Metadata["peer_kind"].(string); ok && pk != "" {
			originPeerKind = pk
		}
	}

	meta := map[string]string{
		"origin_channel":   originChannel,
		"origin_peer_kind": originPeerKind,
		"origin_chat_id":   task.ChatID,
		"from_agent":       fromAgent,
		"to_agent":         ag.AgentKey,
		"team_task_id":     taskID.String(),
		"team_id":          teamID.String(),
	}
	// Pass team workspace and local key from task metadata.
	if task.Metadata != nil {
		if ws, _ := task.Metadata["team_workspace"].(string); ws != "" {
			meta["team_workspace"] = ws
		}
		if lk, _ := task.Metadata["local_key"].(string); lk != "" {
			meta["origin_local_key"] = lk
		}
	}

	m.msgBus.PublishInbound(bus.InboundMessage{
		Channel:  "system",
		SenderID: "teammate:dashboard",
		ChatID:   teamID.String(),
		Content:  content,
		UserID:   userID,
		AgentID:  ag.AgentKey,
		Metadata: meta,
	})
	slog.Info("teams.tasks.dispatch: sent task to agent",
		"task_id", taskID,
		"agent_key", ag.AgentKey,
		"team_id", teamID,
	)
}
