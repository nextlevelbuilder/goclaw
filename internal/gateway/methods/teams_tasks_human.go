package methods

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// Human-in-the-loop task actions from the dashboard.
//
// Until now a human could only delete a task once it was terminal and
// approve/reject one sitting in in_review. A task that ended up blocked,
// stale, failed or cancelled could be neither cancelled nor restarted from the
// UI: CancelTask/ResetTaskStatus existed in the store but were reachable only
// through the lead agent's team_tasks tool, which requires the full task UUID
// the dashboard never showed. These two handlers expose the same transitions
// to the human directly.

// --- Task Cancel ---

type taskCancelParams struct {
	TeamID string `json:"teamId"`
	TaskID string `json:"taskId"`
	Reason string `json:"reason"` // optional; stored as result and posted as a comment
}

func (m *TeamsMethods) handleTaskCancel(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params taskCancelParams
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

	reason := strings.TrimSpace(params.Reason)
	if len(reason) > maxCommentLength {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "reason too long"))
		return
	}

	task, ok := m.loadTeamTask(ctx, client, req, locale, "teams.tasks.cancel", taskID, teamID)
	if !ok {
		return
	}
	switch task.Status {
	case store.TeamTaskStatusCompleted, store.TeamTaskStatusCancelled:
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "task is already "+task.Status))
		return
	}

	storedReason := reason
	if storedReason == "" {
		storedReason = "Cancelled by human"
	}
	// CancelTask also unblocks dependents (blocked → pending). They are not
	// dispatched here: the dashboard has no agent turn to hang a post-turn
	// dispatch on, so the lead's next turn (or teams.tasks.retry) picks them up.
	if err := m.teamStore.CancelTask(ctx, taskID, teamID, storedReason); err != nil {
		slog.Warn("teams.tasks.cancel failed", "task_id", taskID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		return
	}

	// A human-supplied reason is worth keeping in the thread, not only in the
	// result column: the lead reads comments when it reviews the board.
	if reason != "" {
		m.addHumanComment(ctx, client, taskID, reason)
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"ok": true, "status": store.TeamTaskStatusCancelled}))

	if m.msgBus != nil {
		m.msgBus.Broadcast(taskBusEvent(protocol.EventTeamTaskCancelled, protocol.TeamTaskEventPayload{
			TeamID:     teamID.String(),
			TaskID:     taskID.String(),
			TaskNumber: task.TaskNumber,
			Subject:    task.Subject,
			Status:     store.TeamTaskStatusCancelled,
			Reason:     storedReason,
			UserID:     client.UserID(),
			Channel:    taskOriginChannel(task),
			ChatID:     task.ChatID,
			Timestamp:  taskNowUTC(),
			ActorType:  "human",
			ActorID:    client.UserID(),
		}))
	}
}

// --- Task Retry ---

type taskRetryParams struct {
	TeamID  string `json:"teamId"`
	TaskID  string `json:"taskId"`
	Comment string `json:"comment"` // required: the human's answer or instructions for the assignee
	AgentID string `json:"agentId"` // optional: reassign to another member (agent key or UUID)
}

// retryableTaskStatuses mirrors the ResetTaskStatus predicate in the store.
// blocked is allowed only when nothing blocks the task any more — a task that
// still waits on other tasks must be unblocked by finishing or cancelling
// those, not by restarting it.
var retryableTaskStatuses = map[string]bool{
	store.TeamTaskStatusStale:     true,
	store.TeamTaskStatusFailed:    true,
	store.TeamTaskStatusCancelled: true,
	store.TeamTaskStatusInReview:  true,
	store.TeamTaskStatusBlocked:   true,
}

func (m *TeamsMethods) handleTaskRetry(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params taskRetryParams
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

	// The comment is the whole point of a human retry: it is the answer the
	// assignee was missing. Without it the agent would just hit the same wall.
	comment := strings.TrimSpace(params.Comment)
	if comment == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "comment")))
		return
	}
	if len(comment) > maxCommentLength {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "comment too long"))
		return
	}

	task, ok := m.loadTeamTask(ctx, client, req, locale, "teams.tasks.retry", taskID, teamID)
	if !ok {
		return
	}
	if !retryableTaskStatuses[task.Status] {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			"task cannot be retried from status "+task.Status+" (retry works on stale, failed, cancelled, in_review and unblocked blocked tasks)"))
		return
	}
	if task.Status == store.TeamTaskStatusBlocked && len(task.BlockedBy) > 0 {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			"task is still blocked by other tasks — complete or cancel them first"))
		return
	}

	// Assignee: explicit reassignment wins, otherwise keep the current owner.
	var agentID uuid.UUID
	if params.AgentID != "" {
		agentID, err = resolveAgentUUIDCached(ctx, m.agentRouter, m.agentStore, params.AgentID)
		if err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "agentId")))
			return
		}
	} else if task.OwnerAgentID != nil {
		agentID = *task.OwnerAgentID
	} else {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "task has no assignee — pass agentId"))
		return
	}

	// Same guard as teams.tasks.assign / dispatchTaskToAgent: a task owned by
	// the lead would be dispatched into the lead's own session and loop.
	team, err := m.teamStore.GetTeam(ctx, teamID)
	if err != nil || team == nil {
		slog.Warn("teams.tasks.retry get team failed", "team_id", teamID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		return
	}
	if agentID == team.LeadAgentID {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest,
			"cannot retry a task assigned to the team lead — pass a team member as agentId"))
		return
	}

	// Comment first, so it is already on the task when the assignee reads it.
	if err := m.teamStore.AddTaskComment(ctx, &store.TeamTaskCommentData{
		TaskID:  taskID,
		UserID:  client.UserID(),
		Content: comment,
	}); err != nil {
		slog.Warn("teams.tasks.retry comment failed", "task_id", taskID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		return
	}

	// pending first (AssignTask only transitions from pending), then assign.
	if err := m.teamStore.ResetTaskStatus(ctx, taskID, teamID); err != nil {
		slog.Warn("teams.tasks.retry reset failed", "task_id", taskID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		return
	}
	if err := m.teamStore.AssignTask(ctx, taskID, agentID, teamID); err != nil {
		slog.Warn("teams.tasks.retry assign failed", "task_id", taskID, "agent_id", agentID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"ok": true, "status": store.TeamTaskStatusInProgress}))

	if m.msgBus == nil {
		return
	}
	m.msgBus.Broadcast(taskBusEvent(protocol.EventTeamTaskDispatched, protocol.TeamTaskEventPayload{
		TeamID:     teamID.String(),
		TaskID:     taskID.String(),
		TaskNumber: task.TaskNumber,
		Subject:    task.Subject,
		Status:     store.TeamTaskStatusInProgress,
		Reason:     comment,
		UserID:     client.UserID(),
		Channel:    taskOriginChannel(task),
		ChatID:     task.ChatID,
		Timestamp:  taskNowUTC(),
		ActorType:  "human",
		ActorID:    client.UserID(),
	}))

	// The assignee gets the task prompt with the human's comment appended, so
	// the answer arrives in the same message as the assignment instead of
	// relying on the agent to go and read the comment thread.
	dispatched := *task
	dispatched.Description = strings.TrimSpace(task.Description + "\n\n[Retry requested by human]\n" + comment)
	m.dispatchTaskToAgent(ctx, &dispatched, taskID, teamID, agentID, client.UserID())
}

// --- helpers ---

// loadTeamTask fetches a task and checks it belongs to the team (IDOR guard).
// Sends the error response itself and returns ok=false when the caller must stop.
func (m *TeamsMethods) loadTeamTask(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame, locale, op string, taskID, teamID uuid.UUID) (*store.TeamTaskData, bool) {
	task, err := m.teamStore.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, store.ErrTaskNotFound) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "task", "")))
		} else {
			slog.Warn(op+" get failed", "task_id", taskID, "error", err)
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		}
		return nil, false
	}
	if task.TeamID != teamID {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "task", "")))
		return nil, false
	}
	return task, true
}

func (m *TeamsMethods) addHumanComment(ctx context.Context, client *gateway.Client, taskID uuid.UUID, content string) {
	if err := m.teamStore.AddTaskComment(ctx, &store.TeamTaskCommentData{
		TaskID:  taskID,
		UserID:  client.UserID(),
		Content: content,
	}); err != nil {
		slog.Warn("audit.comment_failed", "task_id", taskID, "error", err)
	}
}

// taskOriginChannel is the channel the task was created from; "dashboard" for
// tasks that never had one, matching dispatchTaskToAgent.
func taskOriginChannel(task *store.TeamTaskData) string {
	if task.Channel == "" {
		return "dashboard"
	}
	return task.Channel
}
