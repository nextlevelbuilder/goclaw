package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
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
				"type": "string",
				"description": "'list', 'get', 'create', 'claim', 'complete', 'cancel', 'approve', 'reject', 'search', 'review', 'comment', 'progress', 'attach', 'update', 'ask_user', or 'clear_ask_user'. " +
					"ask_user: set a periodic reminder that is sent to the USER (not the team) when you need the user's input/decision to continue (e.g. 'Which design do you prefer?'). ONLY use when you have a question for the user. Do NOT use for status updates, waiting for teammates, or notifications — use 'progress' instead. " +
					"clear_ask_user: cancel a previously set ask_user reminder. " +
					"retry: re-dispatch a stale or failed task.",
			},
			"task_id": map[string]any{
				"type":        "string",
				"description": "Task UUID (required for most actions except list, create, search). When working on a dispatched task, this is auto-resolved from context — you can omit it for complete/progress/comment.",
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
				"description": "Text content: comment text, cancel/reject reason, progress update, or ask_user reminder question (must be a question asking the user for input/decision)",
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
				"description": "Task IDs that must complete first (for create/update)",
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
			"assignee": map[string]any{
				"type":        "string",
				"description": "Agent key to assign task to (for create). Auto-dispatches to that team member.",
			},
		},
		"required": []string{"action"},
	}
}

// v2Actions lists team_tasks actions that require team version >= 2.
var v2Actions = map[string]bool{
	"approve": true, "reject": true, "review": true, "comment": true,
	"progress": true, "attach": true, "update": true,
	"ask_user": true, "clear_ask_user": true, "retry": true,
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
	case "ask_user":
		return t.executeAskUser(ctx, args)
	case "clear_ask_user":
		return t.executeClearAskUser(ctx, args)
	case "retry":
		return t.executeRetry(ctx, args)
	default:
		return ErrorResult(fmt.Sprintf("unknown action: %s (use list, get, create, claim, complete, cancel, search, review, comment, progress, attach, update, ask_user, clear_ask_user, or retry)", action))
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

// resolveTaskID extracts and validates the task_id from tool arguments.
// Falls back to the dispatched task ID from context when task_id is empty or
// not a valid UUID (agents often pass task_number like "1" instead of the UUID).
func resolveTaskID(ctx context.Context, args map[string]any) (uuid.UUID, error) {
	taskIDStr, _ := args["task_id"].(string)

	// Try parsing as UUID first.
	if taskIDStr != "" {
		if id, err := uuid.Parse(taskIDStr); err == nil {
			return id, nil
		}
	}

	// Fall back to the dispatched team task ID from context.
	if ctxID := TeamTaskIDFromCtx(ctx); ctxID != "" {
		if id, err := uuid.Parse(ctxID); err == nil {
			return id, nil
		}
	}

	if taskIDStr == "" {
		return uuid.Nil, fmt.Errorf("task_id is required")
	}
	return uuid.Nil, fmt.Errorf("invalid task_id %q — use the UUID from task list, not the task number", taskIDStr)
}

func (t *TeamTasksTool) executeGet(ctx context.Context, args map[string]any) *Result {
	team, _, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	taskID, err := resolveTaskID(ctx, args)
	if err != nil {
		return ErrorResult(err.Error())
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
				id, err := uuid.Parse(s)
				if err != nil {
					return ErrorResult(fmt.Sprintf("blocked_by contains invalid task ID %q — must be a real task UUID from a previous create call. Create dependency tasks first, then use their IDs.", s))
				}
				blockedBy = append(blockedBy, id)
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

	// Resolve optional assignee (agent key → UUID). Must be a team member.
	var assigneeID uuid.UUID
	if assigneeKey, _ := args["assignee"].(string); assigneeKey != "" {
		aid, err := t.manager.resolveAgentByKey(assigneeKey)
		if err != nil {
			return ErrorResult(fmt.Sprintf("assignee %q not found: %v", assigneeKey, err))
		}
		// Verify assignee is a member of this team.
		members, err := t.manager.cachedListMembers(ctx, team.ID, agentID)
		if err != nil {
			return ErrorResult("failed to verify team membership: " + err.Error())
		}
		isMember := false
		for _, m := range members {
			if m.AgentID == aid {
				isMember = true
				break
			}
		}
		if !isMember {
			return ErrorResult(fmt.Sprintf("agent %q is not a member of this team", assigneeKey))
		}
		assigneeID = aid
	}

	requireApproval, _ := args["require_approval"].(bool)
	status := store.TeamTaskStatusPending
	if requireApproval {
		status = store.TeamTaskStatusInReview
	} else if len(blockedBy) > 0 {
		status = store.TeamTaskStatusBlocked
	}
	// Assigned tasks without blockers stay pending — dispatched after the turn
	// ends via post-turn processing (avoids race with blocked_by setup).

	chatID := ToolChatIDFromCtx(ctx)

	// Compute the team workspace directory so member agents write files to the
	// shared team folder (teams/{teamID}/{chatID}/) instead of their own personal workspace.
	// This aligns write_file/create_image with workspace_read/workspace_write paths.
	taskMeta := make(map[string]any)
	if teamWsDir, err := workspaceDir(t.manager.dataDir, team.ID, "", chatID); err == nil {
		taskMeta["team_workspace"] = teamWsDir
	}
	// Preserve original blocked_by list for blocker-result forwarding when task unblocks.
	if len(blockedBy) > 0 {
		ids := make([]string, len(blockedBy))
		for i, id := range blockedBy {
			ids[i] = id.String()
		}
		taskMeta["original_blocked_by"] = ids
	}
	// Store leader's trace context so unblocked dispatch links back to the leader's trace.
	if traceID := tracing.TraceIDFromContext(ctx); traceID != uuid.Nil {
		taskMeta["origin_trace_id"] = traceID.String()
	}
	if rootSpanID := tracing.ParentSpanIDFromContext(ctx); rootSpanID != uuid.Nil {
		taskMeta["origin_root_span_id"] = rootSpanID.String()
	}

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
		Metadata: taskMeta,
	}
	if assigneeID != uuid.Nil {
		task.OwnerAgentID = &assigneeID
	}

	if err := t.manager.teamStore.CreateTask(ctx, task); err != nil {
		return ErrorResult("failed to create task: " + err.Error())
	}

	agentKey := t.manager.agentKeyFromID(ctx, agentID)
	t.manager.broadcastTeamEvent(protocol.EventTeamTaskCreated, protocol.TeamTaskEventPayload{
		TeamID:    team.ID.String(),
		TaskID:    task.ID.String(),
		Subject:   subject,
		Status:    status,
		UserID:    store.UserIDFromContext(ctx),
		Channel:   ToolChannelFromCtx(ctx),
		ChatID:    chatID,
		Timestamp: task.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		ActorType: "agent",
		ActorID:   agentKey,
	})
	if assigneeID != uuid.Nil {
		t.manager.broadcastTeamEvent(protocol.EventTeamTaskAssigned, protocol.TeamTaskEventPayload{
			TeamID:        team.ID.String(),
			TaskID:        task.ID.String(),
			Status:        status,
			OwnerAgentKey: t.manager.agentKeyFromID(ctx, assigneeID),
			UserID:        store.UserIDFromContext(ctx),
			Channel:       ToolChannelFromCtx(ctx),
			ChatID:        chatID,
			Timestamp:     task.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			ActorType:     "agent",
			ActorID:       agentKey,
		})
	}

	// Track for post-turn dispatch. If no post-turn hook (e.g. HTTP API), dispatch immediately.
	if assigneeID != uuid.Nil && status == store.TeamTaskStatusPending {
		if ptd := PendingTeamDispatchFromCtx(ctx); ptd != nil {
			ptd.Add(team.ID, task.ID)
		} else {
			// Fallback: assign (pending → in_progress + lock) then dispatch.
			if err := t.manager.teamStore.AssignTask(ctx, task.ID, assigneeID, team.ID); err != nil {
				slog.Warn("executeCreate: fallback assign failed", "task_id", task.ID, "error", err)
			} else {
				t.manager.broadcastTeamEvent(protocol.EventTeamTaskAssigned, protocol.TeamTaskEventPayload{
					TeamID:        team.ID.String(),
					TaskID:        task.ID.String(),
					Status:        store.TeamTaskStatusInProgress,
					OwnerAgentKey: t.manager.agentKeyFromID(ctx, assigneeID),
					Timestamp:     time.Now().UTC().Format("2006-01-02T15:04:05Z"),
					ActorType:     "system",
					ActorID:       "fallback_dispatch",
				})
				t.manager.dispatchTaskToAgent(ctx, task, team.ID, assigneeID)
			}
		}
	}

	return NewResult(fmt.Sprintf("Task created: %s (id=%s, identifier=%s, status=%s)", subject, task.ID, task.Identifier, status))
}

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
	// Delegate agents cannot complete tasks — autoCompleteTeamTask handles it.
	if ToolChannelFromCtx(ctx) == ChannelDelegate {
		return ErrorResult("delegate agents cannot complete team tasks directly — results are auto-completed when delegation finishes")
	}

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
	t.manager.broadcastTeamEvent(protocol.EventTeamTaskCompleted, protocol.TeamTaskEventPayload{
		TeamID:           team.ID.String(),
		TaskID:           taskID.String(),
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

	// Immediately dispatch any newly-unblocked tasks.
	t.manager.DispatchUnblockedTasks(ctx, team.ID)

	return NewResult(fmt.Sprintf("Task %s completed. Dependent tasks have been unblocked.", taskID))
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

	// Immediately dispatch any newly-unblocked tasks.
	t.manager.DispatchUnblockedTasks(ctx, team.ID)

	return NewResult(fmt.Sprintf("Task %s cancelled. Any running delegation has been stopped and dependent tasks unblocked.", taskID))
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

func (t *TeamTasksTool) executeComment(ctx context.Context, args map[string]any) *Result {
	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	taskID, err := resolveTaskID(ctx, args)
	if err != nil {
		return ErrorResult(err.Error())
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
		TaskID:    taskID.String(),
		UserID:    store.UserIDFromContext(ctx),
		Channel:   ToolChannelFromCtx(ctx),
		ChatID:    ToolChatIDFromCtx(ctx),
		Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})

	return NewResult(fmt.Sprintf("Comment added to task %s.", taskID))
}

func (t *TeamTasksTool) executeProgress(ctx context.Context, args map[string]any) *Result {
	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	taskID, err := resolveTaskID(ctx, args)
	if err != nil {
		return ErrorResult(err.Error())
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
		TaskID:    taskID.String(),
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

	taskID, err := resolveTaskID(ctx, args)
	if err != nil {
		return ErrorResult(err.Error())
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

	return NewResult(fmt.Sprintf("File attached to task %s.", taskID))
}

func (t *TeamTasksTool) executeUpdate(ctx context.Context, args map[string]any) *Result {
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
	if raw, ok := args["blocked_by"].([]any); ok {
		var blockedBy []uuid.UUID
		for _, v := range raw {
			if s, ok := v.(string); ok {
				id, err := uuid.Parse(s)
				if err != nil {
					return ErrorResult(fmt.Sprintf("blocked_by contains invalid task ID %q — must be a real task UUID.", s))
				}
				blockedBy = append(blockedBy, id)
			}
		}
		updates["blocked_by"] = blockedBy
	}
	if len(updates) == 0 {
		return ErrorResult("no updates provided (set description, subject, or blocked_by)")
	}

	if err := t.manager.teamStore.UpdateTask(ctx, taskID, updates); err != nil {
		return ErrorResult("failed to update task: " + err.Error())
	}

	t.manager.broadcastTeamEvent(protocol.EventTeamTaskUpdated, protocol.TeamTaskEventPayload{
		TeamID:    team.ID.String(),
		TaskID:    taskID.String(),
		Subject:   task.Subject,
		Status:    task.Status,
		UserID:    store.UserIDFromContext(ctx),
		Channel:   ToolChannelFromCtx(ctx),
		ChatID:    ToolChatIDFromCtx(ctx),
		Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		ActorType: "agent",
		ActorID:   t.manager.agentKeyFromID(ctx, agentID),
	})

	return NewResult(fmt.Sprintf("Task %s updated.", taskID))
}

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

	t.manager.broadcastTeamEvent(protocol.EventTeamTaskAssigned, protocol.TeamTaskEventPayload{
		TeamID:        team.ID.String(),
		TaskID:        taskID.String(),
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
