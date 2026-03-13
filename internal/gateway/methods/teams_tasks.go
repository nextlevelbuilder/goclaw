package methods

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// maxCommentLength caps comment/reason content to prevent DB bloat.
const maxCommentLength = 10000

func taskBusEvent(name string, payload any) bus.Event {
	return bus.Event{Name: name, Payload: payload}
}

func taskNowUTC() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}

// parseTaskParams unmarshals params and checks teamStore availability.
// Returns locale and false if an error response was already sent.
func (m *TeamsMethods) parseTaskParams(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame, dst any) (string, bool) {
	locale := store.LocaleFromContext(ctx)
	if m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgTeamsNotConfigured)))
		return locale, false
	}
	if err := json.Unmarshal(req.Params, dst); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return locale, false
	}
	return locale, true
}

// RegisterTasks registers teams.tasks.* RPC handlers.
func (m *TeamsMethods) RegisterTasks(router *gateway.MethodRouter) {
	router.Register(protocol.MethodTeamsTaskGet, m.handleTaskGet)
	router.Register(protocol.MethodTeamsTaskApprove, m.handleTaskApprove)
	router.Register(protocol.MethodTeamsTaskReject, m.handleTaskReject)
	router.Register(protocol.MethodTeamsTaskComment, m.handleTaskComment)
	router.Register(protocol.MethodTeamsTaskComments, m.handleTaskComments)
	router.Register(protocol.MethodTeamsTaskEvents, m.handleTaskEvents)
	router.Register(protocol.MethodTeamsTaskCreate, m.handleTaskCreate)
	router.Register(protocol.MethodTeamsTaskAssign, m.handleTaskAssign)
}

// --- Task Get (with comments + events + attachments) ---

type taskGetParams struct {
	TeamID string `json:"teamId"`
	TaskID string `json:"taskId"`
}

func (m *TeamsMethods) handleTaskGet(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params taskGetParams
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

	task, err := m.teamStore.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, store.ErrTaskNotFound) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "task", "")))
		} else {
			slog.Warn("teams.tasks.get failed", "task_id", taskID, "error", err)
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		}
		return
	}

	// Validate task belongs to the requested team (prevent IDOR).
	if task.TeamID != teamID {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "task", "")))
		return
	}

	comments, _ := m.teamStore.ListTaskComments(ctx, taskID)
	events, _ := m.teamStore.ListTaskEvents(ctx, taskID)
	attachments, _ := m.teamStore.ListTaskAttachments(ctx, taskID)

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"task":        task,
		"comments":    comments,
		"events":      events,
		"attachments": attachments,
	}))
}

// --- Task Approve ---

type taskApproveParams struct {
	TeamID  string `json:"teamId"`
	TaskID  string `json:"taskId"`
	Comment string `json:"comment"`
}

func (m *TeamsMethods) handleTaskApprove(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params taskApproveParams
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

	if len(params.Comment) > maxCommentLength {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "comment too long"))
		return
	}

	if err := m.teamStore.ApproveTask(ctx, taskID, teamID, params.Comment); err != nil {
		slog.Warn("teams.tasks.approve failed", "task_id", taskID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		return
	}

	// Record audit event.
	if err := m.teamStore.RecordTaskEvent(ctx, &store.TeamTaskEventData{
		TaskID:    taskID,
		EventType: "approved",
		ActorType: "human",
		ActorID:   client.UserID(),
	}); err != nil {
		slog.Warn("audit.record_failed", "task_id", taskID, "event", "approved", "error", err)
	}

	// Add optional comment.
	if params.Comment != "" {
		if err := m.teamStore.AddTaskComment(ctx, &store.TeamTaskCommentData{
			TaskID:  taskID,
			UserID:  client.UserID(),
			Content: params.Comment,
		}); err != nil {
			slog.Warn("audit.comment_failed", "task_id", taskID, "error", err)
		}
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"ok": true}))

	if m.msgBus != nil {
		m.msgBus.Broadcast(taskBusEvent(protocol.EventTeamTaskApproved, protocol.TeamTaskEventPayload{
			TeamID:    teamID.String(),
			TaskID:    taskID.String(),
			Status:    store.TeamTaskStatusCompleted,
			UserID:    client.UserID(),
			Channel:   "dashboard",
			Timestamp: taskNowUTC(),
		}))
	}
}

// --- Task Reject ---

type taskRejectParams struct {
	TeamID string `json:"teamId"`
	TaskID string `json:"taskId"`
	Reason string `json:"reason"`
}

func (m *TeamsMethods) handleTaskReject(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params taskRejectParams
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

	reason := params.Reason
	if reason == "" {
		reason = "Rejected by human"
	}
	if len(reason) > maxCommentLength {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "reason too long"))
		return
	}

	if err := m.teamStore.RejectTask(ctx, taskID, teamID, reason); err != nil {
		slog.Warn("teams.tasks.reject failed", "task_id", taskID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		return
	}

	// Record audit event.
	if err := m.teamStore.RecordTaskEvent(ctx, &store.TeamTaskEventData{
		TaskID:    taskID,
		EventType: "rejected",
		ActorType: "human",
		ActorID:   client.UserID(),
	}); err != nil {
		slog.Warn("audit.record_failed", "task_id", taskID, "event", "rejected", "error", err)
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"ok": true}))

	if m.msgBus != nil {
		m.msgBus.Broadcast(taskBusEvent(protocol.EventTeamTaskRejected, protocol.TeamTaskEventPayload{
			TeamID:    teamID.String(),
			TaskID:    taskID.String(),
			Status:    store.TeamTaskStatusCancelled,
			Reason:    reason,
			UserID:    client.UserID(),
			Channel:   "dashboard",
			Timestamp: taskNowUTC(),
		}))
	}
}

// --- Task Comment (human adds comment) ---

type taskCommentParams struct {
	TeamID  string `json:"teamId"`
	TaskID  string `json:"taskId"`
	Content string `json:"content"`
}

func (m *TeamsMethods) handleTaskComment(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params taskCommentParams
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

	if params.Content == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "content")))
		return
	}
	if len(params.Content) > maxCommentLength {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "comment too long"))
		return
	}

	// Validate task belongs to team (prevent IDOR).
	task, err := m.teamStore.GetTask(ctx, taskID)
	if err != nil || task.TeamID != teamID {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "task", "")))
		return
	}

	if err := m.teamStore.AddTaskComment(ctx, &store.TeamTaskCommentData{
		TaskID:  taskID,
		UserID:  client.UserID(),
		Content: params.Content,
	}); err != nil {
		slog.Warn("teams.tasks.comment failed", "task_id", taskID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"ok": true}))

	if m.msgBus != nil {
		m.msgBus.Broadcast(taskBusEvent(protocol.EventTeamTaskCommented, protocol.TeamTaskEventPayload{
			TeamID:    teamID.String(),
			TaskID:    taskID.String(),
			UserID:    client.UserID(),
			Channel:   "dashboard",
			Timestamp: taskNowUTC(),
		}))
	}
}

// --- Task Comments list ---

type taskCommentsParams struct {
	TeamID string `json:"teamId"`
	TaskID string `json:"taskId"`
}

func (m *TeamsMethods) handleTaskComments(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params taskCommentsParams
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
	if err != nil || task.TeamID != teamID {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "task", "")))
		return
	}

	comments, err := m.teamStore.ListTaskComments(ctx, taskID)
	if err != nil {
		slog.Warn("teams.tasks.comments failed", "task_id", taskID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"comments": comments,
	}))
}

// --- Task Events list ---

type taskEventsParams struct {
	TeamID string `json:"teamId"`
	TaskID string `json:"taskId"`
}

func (m *TeamsMethods) handleTaskEvents(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params taskEventsParams
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
	if err != nil || task.TeamID != teamID {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "task", "")))
		return
	}

	events, err := m.teamStore.ListTaskEvents(ctx, taskID)
	if err != nil {
		slog.Warn("teams.tasks.events failed", "task_id", taskID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"events": events,
	}))
}

// --- Task Create ---

type taskCreateParams struct {
	TeamID      string `json:"teamId"`
	Subject     string `json:"subject"`
	Description string `json:"description"`
	Priority    int    `json:"priority"`
	TaskType    string `json:"taskType"`
	AssignTo    string `json:"assignTo"` // optional agent UUID — assign immediately after creation
	Channel     string `json:"channel"`  // optional scope — defaults to "dashboard"
	ChatID      string `json:"chatId"`   // optional scope — defaults to teamID
}

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

	// Record audit event.
	if err := m.teamStore.RecordTaskEvent(ctx, &store.TeamTaskEventData{
		TaskID:    task.ID,
		EventType: "created",
		ActorType: "human",
		ActorID:   client.UserID(),
	}); err != nil {
		slog.Warn("audit.record_failed", "task_id", task.ID, "event", "created", "error", err)
	}

	// Auto-assign: use explicit assignTo, otherwise fall back to team lead.
	assignTo := params.AssignTo
	if assignTo == "" {
		team, err := m.teamStore.GetTeam(ctx, teamID)
		if err == nil && team != nil && team.LeadAgentID != uuid.Nil {
			assignTo = team.LeadAgentID.String()
		}
	}
	var autoAssignedAgentID uuid.UUID
	if assignTo != "" {
		agentID, err := uuid.Parse(assignTo)
		if err == nil {
			if err := m.teamStore.AssignTask(ctx, task.ID, agentID, teamID); err != nil {
				slog.Warn("teams.tasks.create auto-assign failed", "task_id", task.ID, "agent_id", agentID, "error", err)
			} else {
				task.Status = store.TeamTaskStatusInProgress
				task.OwnerAgentID = &agentID
				autoAssignedAgentID = agentID
				if err := m.teamStore.RecordTaskEvent(ctx, &store.TeamTaskEventData{
					TaskID:    task.ID,
					EventType: "assigned",
					ActorType: "human",
					ActorID:   client.UserID(),
					Data:      json.RawMessage(`{"agent_id":"` + agentID.String() + `"}`),
				}); err != nil {
					slog.Warn("audit.record_failed", "task_id", task.ID, "event", "assigned", "error", err)
				}
			}
		}
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"task": task}))

	if m.msgBus != nil {
		m.msgBus.Broadcast(taskBusEvent(protocol.EventTeamTaskCreated, protocol.TeamTaskEventPayload{
			TeamID:    teamID.String(),
			TaskID:    task.ID.String(),
			Status:    store.TeamTaskStatusPending,
			UserID:    client.UserID(),
			Channel:   "dashboard",
			Timestamp: taskNowUTC(),
		}))

		// Dispatch to assigned agent if auto-assigned during creation.
		if autoAssignedAgentID != uuid.Nil {
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

	// Record audit event.
	if err := m.teamStore.RecordTaskEvent(ctx, &store.TeamTaskEventData{
		TaskID:    taskID,
		EventType: "assigned",
		ActorType: "human",
		ActorID:   client.UserID(),
		Data:      json.RawMessage(`{"agent_id":"` + agentID.String() + `"}`),
	}); err != nil {
		slog.Warn("audit.record_failed", "task_id", taskID, "event", "assigned", "error", err)
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"ok": true}))

	if m.msgBus != nil {
		m.msgBus.Broadcast(taskBusEvent(protocol.EventTeamTaskAssigned, protocol.TeamTaskEventPayload{
			TeamID:    teamID.String(),
			TaskID:    taskID.String(),
			Status:    store.TeamTaskStatusInProgress,
			UserID:    client.UserID(),
			Channel:   "dashboard",
			Timestamp: taskNowUTC(),
		}))

		// Dispatch task to the assigned agent via message bus so the consumer
		// routes it through the agent loop (same pattern as team_message).
		m.dispatchTaskToAgent(ctx, task, taskID, teamID, agentID, client.UserID())
	}
}

// dispatchTaskToAgent publishes a teammate-style inbound message so the
// gateway consumer picks it up and runs the assigned agent, then auto-completes
// the task on success or auto-fails on error.
func (m *TeamsMethods) dispatchTaskToAgent(ctx context.Context, task *store.TeamTaskData, taskID, teamID, agentID uuid.UUID, userID string) {
	ag, err := m.agentStore.GetByID(ctx, agentID)
	if err != nil {
		slog.Warn("teams.tasks.dispatch: cannot resolve agent", "agent_id", agentID, "error", err)
		return
	}

	// Build task prompt for the agent.
	content := fmt.Sprintf("[Assigned task #%d]: %s", task.TaskNumber, task.Subject)
	if task.Description != "" {
		content += "\n\n" + task.Description
	}

	m.msgBus.PublishInbound(bus.InboundMessage{
		Channel:  "system",
		SenderID: fmt.Sprintf("teammate:dashboard"),
		ChatID:   teamID.String(),
		Content:  content,
		UserID:   userID,
		AgentID:  ag.AgentKey,
		Metadata: map[string]string{
			"origin_channel":   "dashboard",
			"origin_peer_kind": "direct",
			"from_agent":       "dashboard",
			"to_agent":         ag.AgentKey,
			"team_task_id":     taskID.String(),
			"team_id":          teamID.String(),
		},
	})
	slog.Info("teams.tasks.dispatch: sent task to agent",
		"task_id", taskID,
		"agent_key", ag.AgentKey,
		"team_id", teamID,
	)
}
