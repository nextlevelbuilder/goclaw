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
	httpapi "github.com/nextlevelbuilder/goclaw/internal/http"
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
	router.Register(protocol.MethodTeamsTaskGetLight, m.handleTaskGetLight)
	router.Register(protocol.MethodTeamsTaskApprove, m.handleTaskApprove)
	router.Register(protocol.MethodTeamsTaskReject, m.handleTaskReject)
	router.Register(protocol.MethodTeamsTaskComment, m.handleTaskComment)
	router.Register(protocol.MethodTeamsTaskComments, m.handleTaskComments)
	router.Register(protocol.MethodTeamsTaskEvents, m.handleTaskEvents)
	router.Register(protocol.MethodTeamsTaskCreate, m.handleTaskCreate)
	router.Register(protocol.MethodTeamsTaskDelete, m.handleTaskDelete)
	router.Register(protocol.MethodTeamsTaskDeleteBulk, m.handleTaskDeleteBulk)
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

	// Sign download URLs at delivery time (same pattern as chat file URLs).
	for i := range attachments {
		dlPath := fmt.Sprintf("/v1/teams/%s/attachments/%s/download", teamID, attachments[i].ID)
		ft := httpapi.SignFileToken(dlPath, httpapi.FileSigningKey(), httpapi.FileTokenTTL)
		attachments[i].DownloadURL = dlPath + "?ft=" + ft
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"task":        task,
		"comments":    comments,
		"events":      events,
		"attachments": attachments,
	}))
}

// --- Task Get Light (task only, no comments/events/attachments) ---

func (m *TeamsMethods) handleTaskGetLight(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
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
			slog.Warn("teams.tasks.get-light failed", "task_id", taskID, "error", err)
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		}
		return
	}

	if task.TeamID != teamID {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "task", "")))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"task": task,
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
			ActorType: "human",
			ActorID:   client.UserID(),
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
			ActorType: "human",
			ActorID:   client.UserID(),
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
		commentPreview := params.Content
		if runes := []rune(commentPreview); len(runes) > 500 {
			commentPreview = string(runes[:500]) + "..."
		}
		m.msgBus.Broadcast(taskBusEvent(protocol.EventTeamTaskCommented, protocol.TeamTaskEventPayload{
			TeamID:      teamID.String(),
			TaskID:      taskID.String(),
			TaskNumber:  task.TaskNumber,
			Subject:     task.Subject,
			CommentText: commentPreview,
			UserID:      client.UserID(),
			Channel:     "dashboard",
			Timestamp:   taskNowUTC(),
			ActorType:   "human",
			ActorID:     client.UserID(),
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

