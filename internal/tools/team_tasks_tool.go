package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// TeamTasksTool exposes the shared team task list to agents.
// Actions: list, get, create, claim, complete, cancel, search, review, comment, progress, attach, update.
type TeamTasksTool struct {
	manager *TeamToolManager
}

func NewTeamTasksTool(manager *TeamToolManager) *TeamTasksTool {
	return &TeamTasksTool{manager: manager}
}

func (t *TeamTasksTool) Name() string { return "team_tasks" }

func (t *TeamTasksTool) Description() string {
	return "Manage the shared team task list (create, claim, complete, track progress). See TEAM.md for available actions and team context."
}

func (t *TeamTasksTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "'list', 'get', 'create', 'claim', 'complete', 'cancel', 'approve', 'reject', 'search', 'review', 'comment', 'progress', 'attach', 'update', 'await_reply', or 'clear_followup'",
			},
			"task_id": map[string]any{
				"type":        "string",
				"description": "Task ID (required for most actions except list, create, search)",
			},
			"subject": map[string]any{
				"type":        "string",
				"description": "Task subject (required for create, optional for update)",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Task description (for create or update)",
			},
			"result": map[string]any{
				"type":        "string",
				"description": "Result summary (required for complete)",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "Text content: comment text, cancel/reject reason, progress step, or followup reminder message",
			},
			"status": map[string]any{
				"type":        "string",
				"description": "Filter for list: '' (active, default), 'completed', 'all'",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "Search query for action=search",
			},
			"priority": map[string]any{
				"type":        "number",
				"description": "Priority, higher = more important (for create, default 0)",
			},
			"blocked_by": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Task IDs that must complete first (for create)",
			},
			"require_approval": map[string]any{
				"type":        "boolean",
				"description": "Require user approval before claim (for create, default false)",
			},
			"percent": map[string]any{
				"type":        "number",
				"description": "Progress 0-100 (for progress)",
			},
			"file_id": map[string]any{
				"type":        "string",
				"description": "Workspace file ID (for attach)",
			},
		},
		"required": []string{"action"},
	}
}

// v2Actions lists team_tasks actions that require team version >= 2.
var v2Actions = map[string]bool{
	"approve": true, "reject": true, "review": true, "comment": true,
	"progress": true, "attach": true, "update": true,
	"await_reply": true, "clear_followup": true,
}

func (t *TeamTasksTool) Execute(ctx context.Context, args map[string]any) *Result {
	action, _ := args["action"].(string)

	// Gate v2-only actions: resolve team once and check version.
	if v2Actions[action] {
		team, _, err := t.manager.resolveTeam(ctx)
		if err != nil {
			return ErrorResult(err.Error())
		}
		if !IsTeamV2(team) {
			return ErrorResult(fmt.Sprintf("action '%s' requires team version 2 — upgrade in team settings", action))
		}
	}

	switch action {
	case "list":
		return t.executeList(ctx, args)
	case "get":
		return t.executeGet(ctx, args)
	case "create":
		return t.executeCreate(ctx, args)
	case "claim":
		return t.executeClaim(ctx, args)
	case "complete":
		return t.executeComplete(ctx, args)
	case "cancel":
		return t.executeCancel(ctx, args)
	case "approve":
		return t.executeApprove(ctx, args)
	case "reject":
		return t.executeReject(ctx, args)
	case "search":
		return t.executeSearch(ctx, args)
	case "review":
		return t.executeReview(ctx, args)
	case "comment":
		return t.executeComment(ctx, args)
	case "progress":
		return t.executeProgress(ctx, args)
	case "attach":
		return t.executeAttach(ctx, args)
	case "update":
		return t.executeUpdate(ctx, args)
	case "await_reply":
		return t.executeAwaitReply(ctx, args)
	case "clear_followup":
		return t.executeClearFollowup(ctx, args)
	default:
		return ErrorResult(fmt.Sprintf("unknown action: %s (use list, get, create, claim, complete, cancel, search, review, comment, progress, attach, update, await_reply, or clear_followup)", action))
	}
}

const listTasksLimit = 20

func (t *TeamTasksTool) executeList(ctx context.Context, args map[string]any) *Result {
	team, _, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	statusFilter, _ := args["status"].(string)

	// Delegate/system channels see all tasks; end users only see their own.
	filterUserID := ""
	channel := ToolChannelFromCtx(ctx)
	if channel != ChannelDelegate && channel != ChannelSystem {
		filterUserID = store.UserIDFromContext(ctx)
	}

	tasks, err := t.manager.teamStore.ListTasks(ctx, team.ID, "priority", statusFilter, filterUserID, "", "")
	if err != nil {
		return ErrorResult("failed to list tasks: " + err.Error())
	}

	// Strip results from list view — use action=get for full detail
	for i := range tasks {
		tasks[i].Result = nil
	}

	hasMore := len(tasks) > listTasksLimit
	if hasMore {
		tasks = tasks[:listTasksLimit]
	}

	resp := map[string]any{
		"tasks": tasks,
		"count": len(tasks),
	}
	if hasMore {
		resp["note"] = fmt.Sprintf("Showing first %d tasks. Use action=search with a query to find older tasks.", listTasksLimit)
		resp["has_more"] = true
	}

	out, _ := json.Marshal(resp)
	return SilentResult(string(out))
}

func (t *TeamTasksTool) executeGet(ctx context.Context, args map[string]any) *Result {
	team, _, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	taskIDStr, _ := args["task_id"].(string)
	if taskIDStr == "" {
		return ErrorResult("task_id is required for get action")
	}
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		return ErrorResult("invalid task_id")
	}

	task, err := t.manager.teamStore.GetTask(ctx, taskID)
	if err != nil {
		return ErrorResult("failed to get task: " + err.Error())
	}
	if task.TeamID != team.ID {
		return ErrorResult("task does not belong to your team")
	}

	// Truncate result for context protection (full result in DB)
	const maxResultRunes = 8000
	if task.Result != nil {
		r := []rune(*task.Result)
		if len(r) > maxResultRunes {
			s := string(r[:maxResultRunes]) + "..."
			task.Result = &s
		}
	}

	// Load comments, events, and attachments for full detail view.
	comments, _ := t.manager.teamStore.ListTaskComments(ctx, taskID)
	events, _ := t.manager.teamStore.ListTaskEvents(ctx, taskID)
	attachments, _ := t.manager.teamStore.ListTaskAttachments(ctx, taskID)

	resp := map[string]any{
		"task": task,
	}
	if len(comments) > 0 {
		resp["comments"] = comments
	}
	if len(events) > 0 {
		resp["events"] = events
	}
	if len(attachments) > 0 {
		resp["attachments"] = attachments
	}

	out, _ := json.Marshal(resp)
	return SilentResult(string(out))
}

func (t *TeamTasksTool) executeSearch(ctx context.Context, args map[string]any) *Result {
	team, _, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	query, _ := args["query"].(string)
	if query == "" {
		return ErrorResult("query is required for search action")
	}

	// Delegate/system channels see all tasks; end users only see their own.
	filterUserID := ""
	channel := ToolChannelFromCtx(ctx)
	if channel != ChannelDelegate && channel != ChannelSystem {
		filterUserID = store.UserIDFromContext(ctx)
	}

	tasks, err := t.manager.teamStore.SearchTasks(ctx, team.ID, query, 20, filterUserID)
	if err != nil {
		return ErrorResult("failed to search tasks: " + err.Error())
	}

	// Show result snippets in search results
	const maxSnippetRunes = 500
	for i := range tasks {
		if tasks[i].Result != nil {
			r := []rune(*tasks[i].Result)
			if len(r) > maxSnippetRunes {
				s := string(r[:maxSnippetRunes]) + "..."
				tasks[i].Result = &s
			}
		}
	}

	out, _ := json.Marshal(map[string]any{
		"tasks": tasks,
		"count": len(tasks),
	})
	return SilentResult(string(out))
}

func (t *TeamTasksTool) executeCreate(ctx context.Context, args map[string]any) *Result {
	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}
	if err := t.manager.requireLead(ctx, team, agentID); err != nil {
		return ErrorResult(err.Error())
	}

	subject, _ := args["subject"].(string)
	if subject == "" {
		return ErrorResult("subject is required for create action")
	}

	description, _ := args["description"].(string)
	priority := 0
	if p, ok := args["priority"].(float64); ok {
		priority = int(p)
	}

	var blockedBy []uuid.UUID
	if raw, ok := args["blocked_by"].([]any); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				if id, err := uuid.Parse(s); err == nil {
					blockedBy = append(blockedBy, id)
				}
			}
		}
	}

	// Validate that all blocked_by tasks belong to the same team.
	for _, depID := range blockedBy {
		depTask, err := t.manager.teamStore.GetTask(ctx, depID)
		if err != nil {
			return ErrorResult(fmt.Sprintf("blocked_by task %s not found: %v", depID, err))
		}
		if depTask.TeamID != team.ID {
			return ErrorResult(fmt.Sprintf("blocked_by task %s belongs to a different team", depID))
		}
	}

	requireApproval, _ := args["require_approval"].(bool)
	status := store.TeamTaskStatusPending
	if requireApproval {
		status = store.TeamTaskStatusInReview
	} else if len(blockedBy) > 0 {
		status = store.TeamTaskStatusBlocked
	}

	chatID := ToolChatIDFromCtx(ctx)

	task := &store.TeamTaskData{
		TeamID:           team.ID,
		Subject:          subject,
		Description:      description,
		Status:           status,
		BlockedBy:        blockedBy,
		Priority:         priority,
		UserID:           store.UserIDFromContext(ctx),
		Channel:          ToolChannelFromCtx(ctx),
		TaskType:         "general",
		CreatedByAgentID: &agentID,
		ChatID:           chatID,
	}

	if err := t.manager.teamStore.CreateTask(ctx, task); err != nil {
		return ErrorResult("failed to create task: " + err.Error())
	}

	t.manager.broadcastTeamEvent(protocol.EventTeamTaskCreated, protocol.TeamTaskEventPayload{
		TeamID:    team.ID.String(),
		TaskID:    task.ID.String(),
		Subject:   subject,
		Status:    status,
		UserID:    store.UserIDFromContext(ctx),
		Channel:   ToolChannelFromCtx(ctx),
		ChatID:    chatID,
		Timestamp: task.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	})

	return NewResult(fmt.Sprintf("Task created: %s (id=%s, identifier=%s, status=%s)", subject, task.ID, task.Identifier, status))
}

func (t *TeamTasksTool) executeClaim(ctx context.Context, args map[string]any) *Result {
	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	taskIDStr, _ := args["task_id"].(string)
	if taskIDStr == "" {
		return ErrorResult("task_id is required for claim action")
	}
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		return ErrorResult("invalid task_id")
	}

	if err := t.manager.teamStore.ClaimTask(ctx, taskID, agentID, team.ID); err != nil {
		return ErrorResult("failed to claim task: " + err.Error())
	}

	ownerKey := t.manager.agentKeyFromID(ctx, agentID)
	t.manager.broadcastTeamEvent(protocol.EventTeamTaskClaimed, protocol.TeamTaskEventPayload{
		TeamID:           team.ID.String(),
		TaskID:           taskIDStr,
		Status:           store.TeamTaskStatusInProgress,
		OwnerAgentKey:    ownerKey,
		OwnerDisplayName: t.manager.agentDisplayName(ctx, ownerKey),
		UserID:           store.UserIDFromContext(ctx),
		Channel:          ToolChannelFromCtx(ctx),
		ChatID:           ToolChatIDFromCtx(ctx),
		Timestamp:        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})

	return NewResult(fmt.Sprintf("Task %s claimed successfully. It is now in progress.", taskIDStr))
}

func (t *TeamTasksTool) executeComplete(ctx context.Context, args map[string]any) *Result {
	// Delegate agents cannot complete tasks — autoCompleteTeamTask handles it.
	if ToolChannelFromCtx(ctx) == ChannelDelegate {
		return ErrorResult("delegate agents cannot complete team tasks directly — results are auto-completed when delegation finishes")
	}

	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	taskIDStr, _ := args["task_id"].(string)
	if taskIDStr == "" {
		return ErrorResult("task_id is required for complete action")
	}
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		return ErrorResult("invalid task_id")
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
	t.manager.broadcastTeamEvent(protocol.EventTeamTaskCompleted, protocol.TeamTaskEventPayload{
		TeamID:           team.ID.String(),
		TaskID:           taskIDStr,
		Status:           store.TeamTaskStatusCompleted,
		OwnerAgentKey:    ownerKey,
		OwnerDisplayName: t.manager.agentDisplayName(ctx, ownerKey),
		UserID:           store.UserIDFromContext(ctx),
		Channel:          ToolChannelFromCtx(ctx),
		ChatID:           ToolChatIDFromCtx(ctx),
		Timestamp:        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})

	return NewResult(fmt.Sprintf("Task %s completed. Dependent tasks have been unblocked.", taskIDStr))
}

func (t *TeamTasksTool) executeCancel(ctx context.Context, args map[string]any) *Result {
	// Delegate agents cannot cancel tasks — only lead/user-facing agents can.
	if ToolChannelFromCtx(ctx) == ChannelDelegate {
		return ErrorResult("delegate agents cannot cancel team tasks directly")
	}

	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}
	if err := t.manager.requireLead(ctx, team, agentID); err != nil {
		return ErrorResult(err.Error())
	}

	taskIDStr, _ := args["task_id"].(string)
	if taskIDStr == "" {
		return ErrorResult("task_id is required for cancel action")
	}
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		return ErrorResult("invalid task_id")
	}

	reason, _ := args["text"].(string)
	if reason == "" {
		reason = "Cancelled by agent"
	}

	// CancelTask: guards against completed tasks, unblocks dependents, transitions blocked→pending.
	if err := t.manager.teamStore.CancelTask(ctx, taskID, team.ID, reason); err != nil {
		return ErrorResult("failed to cancel task: " + err.Error())
	}

	// Cancel any running delegation for this task.
	if t.manager.delegateMgr != nil {
		t.manager.delegateMgr.CancelByTeamTaskID(taskID)
	}

	// Record audit event.
	_ = t.manager.teamStore.RecordTaskEvent(ctx, &store.TeamTaskEventData{
		TaskID:    taskID,
		EventType: "cancelled",
		ActorType: "agent",
		ActorID:   agentID.String(),
	})

	t.manager.broadcastTeamEvent(protocol.EventTeamTaskCancelled, protocol.TeamTaskEventPayload{
		TeamID:    team.ID.String(),
		TaskID:    taskIDStr,
		Status:    store.TeamTaskStatusCancelled,
		Reason:    reason,
		UserID:    store.UserIDFromContext(ctx),
		Channel:   ToolChannelFromCtx(ctx),
		ChatID:    ToolChatIDFromCtx(ctx),
		Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})

	return NewResult(fmt.Sprintf("Task %s cancelled. Any running delegation has been stopped and dependent tasks unblocked.", taskIDStr))
}

func (t *TeamTasksTool) executeReview(ctx context.Context, args map[string]any) *Result {
	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	taskIDStr, _ := args["task_id"].(string)
	if taskIDStr == "" {
		return ErrorResult("task_id is required for review action")
	}
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		return ErrorResult("invalid task_id")
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

	_ = t.manager.teamStore.RecordTaskEvent(ctx, &store.TeamTaskEventData{
		TaskID:    taskID,
		EventType: "reviewed",
		ActorType: "agent",
		ActorID:   agentID.String(),
	})

	ownerKey := t.manager.agentKeyFromID(ctx, agentID)
	t.manager.broadcastTeamEvent(protocol.EventTeamTaskReviewed, protocol.TeamTaskEventPayload{
		TeamID:           team.ID.String(),
		TaskID:           taskIDStr,
		Status:           store.TeamTaskStatusInReview,
		OwnerAgentKey:    ownerKey,
		OwnerDisplayName: t.manager.agentDisplayName(ctx, ownerKey),
		UserID:           store.UserIDFromContext(ctx),
		Channel:          ToolChannelFromCtx(ctx),
		ChatID:           ToolChatIDFromCtx(ctx),
		Timestamp:        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})

	return NewResult(fmt.Sprintf("Task %s submitted for review.", taskIDStr))
}

func (t *TeamTasksTool) executeApprove(ctx context.Context, args map[string]any) *Result {
	// Delegate agents cannot approve tasks — approval requires user authority.
	if ToolChannelFromCtx(ctx) == ChannelDelegate {
		return ErrorResult("delegate agents cannot approve team tasks")
	}

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

	taskIDStr, _ := args["task_id"].(string)
	if taskIDStr == "" {
		return ErrorResult("task_id is required for approve action")
	}
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		return ErrorResult("invalid task_id")
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
		TaskID:    taskIDStr,
		Subject:   task.Subject,
		Status:    newStatus,
		UserID:    store.UserIDFromContext(ctx),
		Channel:   ToolChannelFromCtx(ctx),
		ChatID:    ToolChatIDFromCtx(ctx),
		Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
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

	return NewResult(fmt.Sprintf("Task %s approved (status: %s).", taskIDStr, newStatus))
}

func (t *TeamTasksTool) executeReject(ctx context.Context, args map[string]any) *Result {
	// Delegate agents cannot reject tasks.
	if ToolChannelFromCtx(ctx) == ChannelDelegate {
		return ErrorResult("delegate agents cannot reject team tasks")
	}

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

	taskIDStr, _ := args["task_id"].(string)
	if taskIDStr == "" {
		return ErrorResult("task_id is required for reject action")
	}
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		return ErrorResult("invalid task_id")
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
		TaskID:    taskIDStr,
		Subject:   task.Subject,
		Status:    "cancelled",
		Reason:    reason,
		UserID:    store.UserIDFromContext(ctx),
		Channel:   ToolChannelFromCtx(ctx),
		ChatID:    ToolChatIDFromCtx(ctx),
		Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
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

	return NewResult(fmt.Sprintf("Task %s rejected. Dependent tasks have been unblocked.", taskIDStr))
}

func (t *TeamTasksTool) executeComment(ctx context.Context, args map[string]any) *Result {
	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	taskIDStr, _ := args["task_id"].(string)
	if taskIDStr == "" {
		return ErrorResult("task_id is required for comment action")
	}
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		return ErrorResult("invalid task_id")
	}

	text, _ := args["text"].(string)
	if text == "" {
		return ErrorResult("text is required for comment action")
	}
	if len(text) > 10000 {
		return ErrorResult("comment text too long (max 10000 chars)")
	}

	// Verify task belongs to team.
	task, err := t.manager.teamStore.GetTask(ctx, taskID)
	if err != nil {
		return ErrorResult("task not found: " + err.Error())
	}
	if task.TeamID != team.ID {
		return ErrorResult("task does not belong to your team")
	}

	if err := t.manager.teamStore.AddTaskComment(ctx, &store.TeamTaskCommentData{
		TaskID:  taskID,
		AgentID: &agentID,
		Content: text,
	}); err != nil {
		return ErrorResult("failed to add comment: " + err.Error())
	}

	t.manager.broadcastTeamEvent(protocol.EventTeamTaskCommented, protocol.TeamTaskEventPayload{
		TeamID:    team.ID.String(),
		TaskID:    taskIDStr,
		UserID:    store.UserIDFromContext(ctx),
		Channel:   ToolChannelFromCtx(ctx),
		ChatID:    ToolChatIDFromCtx(ctx),
		Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})

	return NewResult(fmt.Sprintf("Comment added to task %s.", taskIDStr))
}

func (t *TeamTasksTool) executeProgress(ctx context.Context, args map[string]any) *Result {
	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	taskIDStr, _ := args["task_id"].(string)
	if taskIDStr == "" {
		return ErrorResult("task_id is required for progress action")
	}
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		return ErrorResult("invalid task_id")
	}

	percent := 0
	if p, ok := args["percent"].(float64); ok {
		percent = int(p)
	}
	if percent < 0 || percent > 100 {
		return ErrorResult("percent must be 0-100")
	}
	step, _ := args["text"].(string)

	// Verify ownership.
	task, err := t.manager.teamStore.GetTask(ctx, taskID)
	if err != nil {
		return ErrorResult("task not found: " + err.Error())
	}
	if task.TeamID != team.ID {
		return ErrorResult("task does not belong to your team")
	}
	if task.OwnerAgentID == nil || *task.OwnerAgentID != agentID {
		return ErrorResult("only the task owner can update progress")
	}

	if err := t.manager.teamStore.UpdateTaskProgress(ctx, taskID, team.ID, percent, step); err != nil {
		return ErrorResult("failed to update progress: " + err.Error())
	}

	t.manager.broadcastTeamEvent(protocol.EventTeamTaskProgress, protocol.TeamTaskEventPayload{
		TeamID:    team.ID.String(),
		TaskID:    taskIDStr,
		Status:    store.TeamTaskStatusInProgress,
		UserID:    store.UserIDFromContext(ctx),
		Channel:   ToolChannelFromCtx(ctx),
		ChatID:    ToolChatIDFromCtx(ctx),
		Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})

	return SilentResult(fmt.Sprintf("Progress updated: %d%% %s", percent, step))
}

func (t *TeamTasksTool) executeAttach(ctx context.Context, args map[string]any) *Result {
	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	taskIDStr, _ := args["task_id"].(string)
	if taskIDStr == "" {
		return ErrorResult("task_id is required for attach action")
	}
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		return ErrorResult("invalid task_id")
	}

	fileIDStr, _ := args["file_id"].(string)
	if fileIDStr == "" {
		return ErrorResult("file_id is required for attach action")
	}
	fileID, err := uuid.Parse(fileIDStr)
	if err != nil {
		return ErrorResult("invalid file_id")
	}

	// Verify task belongs to team.
	task, err := t.manager.teamStore.GetTask(ctx, taskID)
	if err != nil {
		return ErrorResult("task not found: " + err.Error())
	}
	if task.TeamID != team.ID {
		return ErrorResult("task does not belong to your team")
	}

	if err := t.manager.teamStore.AttachFileToTask(ctx, &store.TeamTaskAttachmentData{
		TaskID:  taskID,
		FileID:  fileID,
		AddedBy: &agentID,
	}); err != nil {
		return ErrorResult("failed to attach file: " + err.Error())
	}

	return NewResult(fmt.Sprintf("File attached to task %s.", taskIDStr))
}

func (t *TeamTasksTool) executeUpdate(ctx context.Context, args map[string]any) *Result {
	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}
	if err := t.manager.requireLead(ctx, team, agentID); err != nil {
		return ErrorResult(err.Error())
	}

	taskIDStr, _ := args["task_id"].(string)
	if taskIDStr == "" {
		return ErrorResult("task_id is required for update action")
	}
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		return ErrorResult("invalid task_id")
	}

	// Verify task belongs to this team (prevent cross-team update).
	task, err := t.manager.teamStore.GetTask(ctx, taskID)
	if err != nil {
		return ErrorResult("task not found: " + err.Error())
	}
	if task.TeamID != team.ID {
		return ErrorResult("task does not belong to your team")
	}

	updates := map[string]any{}
	if desc, ok := args["description"].(string); ok {
		updates["description"] = desc
	}
	if subj, ok := args["subject"].(string); ok && subj != "" {
		updates["subject"] = subj
	}
	if len(updates) == 0 {
		return ErrorResult("no updates provided (set description or subject)")
	}

	if err := t.manager.teamStore.UpdateTask(ctx, taskID, updates); err != nil {
		return ErrorResult("failed to update task: " + err.Error())
	}

	return NewResult(fmt.Sprintf("Task %s updated.", taskIDStr))
}

func (t *TeamTasksTool) executeAwaitReply(ctx context.Context, args map[string]any) *Result {
	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	taskIDStr, _ := args["task_id"].(string)
	if taskIDStr == "" {
		return ErrorResult("task_id is required for await_reply action")
	}
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		return ErrorResult("invalid task_id")
	}

	followupMessage, _ := args["text"].(string)
	if followupMessage == "" {
		return ErrorResult("text is required for await_reply action (the reminder message)")
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
	return NewResult(fmt.Sprintf("Follow-up set for task %s. First reminder in %d minutes via %s (%s).", taskIDStr, delayMinutes, channel, maxDesc))
}

func (t *TeamTasksTool) executeClearFollowup(ctx context.Context, args map[string]any) *Result {
	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	taskIDStr, _ := args["task_id"].(string)
	if taskIDStr == "" {
		return ErrorResult("task_id is required for clear_followup action")
	}
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		return ErrorResult("invalid task_id")
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

	return NewResult(fmt.Sprintf("Follow-up reminders cleared for task %s.", taskIDStr))
}
