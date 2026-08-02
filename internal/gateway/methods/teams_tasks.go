package methods

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	httpapi "github.com/nextlevelbuilder/goclaw/internal/http"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// maxCommentLength caps comment/reason content to prevent DB bloat.
const maxCommentLength = 10000

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
	task, err := m.teamStore.GetTask(ctx, taskID)
	if err != nil || task.TeamID != teamID {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "task", "")))
		return
	}

	approvedStatus := store.TeamTaskStatusCompleted
	workflowExpanded := false
	if task.WorkflowID != nil && task.WorkflowKind == store.TeamWorkflowTaskKindAudit {
		workflowStore, ok := m.teamStore.(store.TeamWorkflowStore)
		if !ok {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
			return
		}
		workflow, workflowErr := workflowStore.GetWorkflow(ctx, *task.WorkflowID)
		team, teamErr := m.teamStore.GetTeam(ctx, teamID)
		if workflowErr != nil || teamErr != nil || team == nil || workflow.CoordinatorAgentID != team.LeadAgentID || task.OwnerAgentID == nil || *task.OwnerAgentID != team.LeadAgentID {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "workflow request is not approvable"))
			return
		}
		if m.postTurn == nil || m.postTurn.RevalidateWorkflow(ctx, workflow) != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "workflow plan is no longer executable; re-plan is required"))
			return
		}
		workflowTasks, buildErr := tools.BuildWorkflowTasksFromStoredPlan(workflow)
		if buildErr != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "stored workflow plan is invalid"))
			return
		}
		tools.InheritWorkflowTaskContext(workflowTasks, task)
		members, memberErr := m.teamStore.ListMembers(ctx, teamID)
		memberIDs := make(map[uuid.UUID]struct{}, len(members))
		for _, member := range members {
			memberIDs[member.AgentID] = struct{}{}
		}
		if memberErr != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
			return
		}
		for _, workflowTask := range workflowTasks {
			if workflowTask.OwnerAgentID == nil {
				client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "workflow owner is invalid"))
				return
			}
			if _, exists := memberIDs[*workflowTask.OwnerAgentID]; !exists {
				client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "workflow roster changed; re-plan is required"))
				return
			}
		}
		if err := workflowStore.ApprovePendingWorkflowRequest(ctx, workflow.ID, task.ID, store.WorkflowApprovalActor{UserID: client.UserID(), Role: string(client.Role())}, workflowTasks); err != nil {
			slog.Warn("teams.tasks.approve workflow failed", "workflow_id", workflow.ID, "error", err)
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
			return
		}
		approvedStatus = store.TeamWorkflowStatusRunning
		workflowExpanded = true
	} else if err := m.teamStore.ApproveTask(ctx, taskID, teamID, params.Comment); err != nil {
		slog.Warn("teams.tasks.approve failed", "task_id", taskID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		return
	}
	if workflowExpanded && m.postTurn != nil {
		m.postTurn.DispatchUnblockedTasks(ctx, teamID)
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
		// Identity/status/workflow/owner are re-derived from the committed row
		// inside the publish path (approvedStatus is a local hint only — the
		// authoritative row wins, as it did before this refactor); owner display
		// is resolved via m.agentStore.
		_ = approvedStatus
		tools.PublishTaskEventWithResolver(m.teamStore, m.msgBus, m.agentStore, protocol.EventTeamTaskApproved, tools.BuildTeamTaskEventPayload(task, "", tools.TeamTaskEventOptions{
			UserID: client.UserID(), Channel: "dashboard",
			ActorType: "human", ActorID: client.UserID(),
		}), uuid.Nil)
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
		// Identity/status re-derived from the committed row inside the publish
		// path; Reason is caller-supplied context. Owner display via m.agentStore.
		tools.PublishTaskEventWithResolver(m.teamStore, m.msgBus, m.agentStore, protocol.EventTeamTaskRejected, protocol.TeamTaskEventPayload{
			TaskID:    taskID.String(),
			Reason:    reason,
			UserID:    client.UserID(),
			Channel:   "dashboard",
			Timestamp: taskNowUTC(),
			ActorType: "human",
			ActorID:   client.UserID(),
		}, uuid.Nil)
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
		tools.PublishTaskEventWithResolver(m.teamStore, m.msgBus, m.agentStore, protocol.EventTeamTaskCommented, tools.BuildTeamTaskEventPayload(task, "", tools.TeamTaskEventOptions{
			CommentText: commentPreview, UserID: client.UserID(), Channel: "dashboard",
			ActorType: "human", ActorID: client.UserID(),
		}), uuid.Nil)
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
