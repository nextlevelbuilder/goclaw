package tools

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func (t *TeamTasksTool) executeCreate(ctx context.Context, args map[string]any) *Result {
	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	// Determine if caller is a lead or a member.
	isLead := agentID == team.LeadAgentID
	channel := ToolChannelFromCtx(ctx)
	if channel == ChannelTeammate || channel == ChannelSystem {
		isLead = true // system/teammate channels act on behalf of the lead
	}

	taskType, _ := args["task_type"].(string)
	if taskType == "" {
		taskType = "general"
	}

	if !isLead {
		// Members may only create "request" tasks when the feature is enabled.
		memberCfg := ParseMemberRequestConfig(team.Settings)
		if !memberCfg.Enabled {
			return ErrorResult("Members cannot create tasks. Use team_tasks(action=\"comment\") to communicate.")
		}
		if taskType != "request" {
			return ErrorResult("Members can only create task_type=\"request\". Use team_tasks(action=\"comment\") to communicate.")
		}
	} else if err := t.manager.requireLead(ctx, team, agentID); err != nil {
		return ErrorResult(err.Error())
	}

	// Gate: must list tasks before creating to prevent duplicates in concurrent group chat.
	if ptd := PendingTeamDispatchFromCtx(ctx); ptd != nil && !ptd.HasListed() {
		return ErrorResult("You must check existing tasks first. Call team_tasks(action=\"search\", query=\"<keywords>\") to check for similar tasks before creating — this saves tokens vs listing all. Alternatively use action=\"list\" to see the full board.")
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

	// Validate that all blocked_by tasks belong to the same team and are not terminal.
	for _, depID := range blockedBy {
		depTask, err := t.manager.teamStore.GetTask(ctx, depID)
		if err != nil {
			return ErrorResult(fmt.Sprintf("blocked_by task %s not found: %v", depID, err))
		}
		if depTask.TeamID != team.ID {
			return ErrorResult(fmt.Sprintf("blocked_by task %s belongs to a different team", depID))
		}
		switch depTask.Status {
		case store.TeamTaskStatusCompleted, store.TeamTaskStatusCancelled, store.TeamTaskStatusFailed:
			return ErrorResult(fmt.Sprintf(
				"blocked_by task %s (%s) is already %s. "+
					"Do not block on finished tasks — create this task without blocked_by, "+
					"or pass the completed task's result in the description instead.",
				depID, depTask.Subject, depTask.Status))
		}
	}

	// Resolve assignee (agent key → UUID). Required — every task must be assigned.
	assigneeKey, _ := args["assignee"].(string)
	if assigneeKey == "" {
		return ErrorResult("assignee is required — specify which team member should handle this task")
	}
	assigneeID, err := t.manager.resolveAgentByKey(ctx, assigneeKey)
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
		if m.AgentID == assigneeID {
			isMember = true
			break
		}
	}
	if !isMember {
		return ErrorResult(fmt.Sprintf("agent %q is not a member of this team", assigneeKey))
	}
	// Prevent lead from self-assigning — causes dual-session execution + loop.
	// buildCreateHint already hides the lead from the member list hint,
	// but weaker models may still attempt self-assignment.
	if assigneeID == team.LeadAgentID {
		return ErrorResult("team lead cannot assign tasks to itself — delegate to a team member instead. You are the team lead; handle this work directly or assign to one of your members.")
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

	// Member requests without auto_dispatch stay pending for leader review.
	memberCfgForDispatch := ParseMemberRequestConfig(team.Settings)
	skipAutoDispatch := !isLead && taskType == "request" && !memberCfgForDispatch.AutoDispatch

	chatID := ToolChatIDFromCtx(ctx)

	// Shared workspace: scope by teamID only. Isolated (default): scope by chatID too.
	wsChat := chatID
	if IsSharedWorkspace(team.Settings) {
		wsChat = ""
	}

	// Compute the team workspace directory (tenant-scoped) so member agents
	// write files to the shared team folder instead of their own personal workspace.
	taskMeta := make(map[string]any)
	tenantBase := config.TenantWorkspace(t.manager.dataDir, store.TenantIDFromContext(ctx), store.TenantSlugFromContext(ctx))
	if teamWsDir, err := WorkspaceDir(tenantBase, team.ID, wsChat); err == nil {
		taskMeta["team_workspace"] = teamWsDir
	}
	// Auto-collect media files from current run to team workspace.
	// When leader received files from user and creates a task, copy those
	// files to the team workspace so members can access them via read_file.
	// Also rewrite any media paths in the description to point to the workspace copy,
	// since members can't access the original .media/ paths outside their workspace.
	if mediaPaths := RunMediaPathsFromCtx(ctx); len(mediaPaths) > 0 {
		if wsDir, _ := taskMeta["team_workspace"].(string); wsDir != "" {
			nameMap := RunMediaNamesFromCtx(ctx)
			if copiedPaths := copyMediaToWorkspace(mediaPaths, wsDir, nameMap); len(copiedPaths) > 0 {
				// Store as []any so type assertion works both before and after JSON round-trip.
				files := make([]any, len(copiedPaths))
				for i, p := range copiedPaths {
					files[i] = p
				}
				taskMeta["attached_files"] = files

				// Rewrite media paths in description so members see workspace paths.
				for i, src := range mediaPaths {
					if i < len(copiedPaths) {
						description = strings.ReplaceAll(description, src, copiedPaths[i])
					}
				}
			}
		}
	}

	// Preserve original blocked_by list for blocker-result forwarding when task unblocks.
	if len(blockedBy) > 0 {
		ids := make([]string, len(blockedBy))
		for i, id := range blockedBy {
			ids[i] = id.String()
		}
		taskMeta["original_blocked_by"] = ids
	}
	// Store peer kind so dispatches preserve the correct session scope (group vs direct).
	if pk := ToolPeerKindFromCtx(ctx); pk != "" {
		taskMeta["peer_kind"] = pk
	}
	// Store local key so forum-topic routing works on deferred/unblocked dispatches.
	if lk := ToolLocalKeyFromCtx(ctx); lk != "" {
		taskMeta["local_key"] = lk
	}
	// Store origin session key so deferred dispatches route announces correctly.
	// WS sessions use non-standard key format that BuildScopedSessionKey() cannot reproduce.
	if sk := ToolSessionKeyFromCtx(ctx); sk != "" {
		taskMeta["origin_session_key"] = sk
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
		TaskType:         taskType,
		CreatedByAgentID: &agentID,
		ChatID:           chatID,
		Metadata:         taskMeta,
	}
	task.OwnerAgentID = &assigneeID

	// Auto-link member request to the member's current task as parent.
	if !isLead && taskType == "request" {
		if parentIDStr := TeamTaskIDFromCtx(ctx); parentIDStr != "" {
			if parentUUID, err := uuid.Parse(parentIDStr); err == nil {
				task.ParentID = &parentUUID
			}
		}
	}

	if err := t.manager.teamStore.CreateTask(ctx, task); err != nil {
		return ErrorResult("failed to create task: " + err.Error())
	}

	agentKey := t.manager.agentKeyFromID(ctx, agentID)
	t.manager.broadcastTeamEvent(ctx, protocol.EventTeamTaskCreated, protocol.TeamTaskEventPayload{
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
	// Track for post-turn dispatch. If no post-turn hook (e.g. HTTP API), dispatch immediately.
	// Member requests with auto_dispatch=false stay pending for leader review — skip dispatch.
	if status == store.TeamTaskStatusPending && !skipAutoDispatch {
		if ptd := PendingTeamDispatchFromCtx(ctx); ptd != nil {
			ptd.Add(team.ID, task.ID)
		} else {
			// Fallback: assign (pending → in_progress + lock) then dispatch.
			if err := t.manager.teamStore.AssignTask(ctx, task.ID, assigneeID, team.ID); err != nil {
				slog.Warn("executeCreate: fallback assign failed", "task_id", task.ID, "error", err)
			} else {
				t.manager.broadcastTeamEvent(ctx, protocol.EventTeamTaskDispatched, protocol.TeamTaskEventPayload{
					TeamID:        team.ID.String(),
					TaskID:        task.ID.String(),
					TaskNumber:    task.TaskNumber,
					Subject:       task.Subject,
					Status:        store.TeamTaskStatusInProgress,
					OwnerAgentKey: t.manager.agentKeyFromID(ctx, assigneeID),
					Channel:       task.Channel,
					ChatID:        task.ChatID,
					Timestamp:     time.Now().UTC().Format("2006-01-02T15:04:05Z"),
					ActorType:     "system",
					ActorID:       "fallback_dispatch",
				})
				t.manager.dispatchTaskToAgent(ctx, task, team, assigneeID)
			}
		}
	}

	assigneeName := t.manager.agentDisplayName(ctx, t.manager.agentKeyFromID(ctx, assigneeID))
	if assigneeName == "" {
		assigneeName = t.manager.agentKeyFromID(ctx, assigneeID)
	}
	return NewResult(fmt.Sprintf("Task created: %s (id=%s, task_number=%d, status=%s, assignee=%s)", subject, task.ID, task.TaskNumber, status, assigneeName))
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

	commentType, _ := args["type"].(string)
	if commentType != "blocker" {
		commentType = "note"
	}

	if err := t.manager.teamStore.AddTaskComment(ctx, &store.TeamTaskCommentData{
		TaskID:      taskID,
		AgentID:     &agentID,
		Content:     text,
		CommentType: commentType,
	}); err != nil {
		return ErrorResult("failed to add comment: " + err.Error())
	}

	t.manager.broadcastTeamEvent(ctx, protocol.EventTeamTaskCommented, protocol.TeamTaskEventPayload{
		TeamID:      team.ID.String(),
		TaskID:      taskID.String(),
		TaskNumber:  task.TaskNumber,
		Subject:     task.Subject,
		CommentText: truncatePreview(text, 500),
		UserID:      store.UserIDFromContext(ctx),
		Channel:     ToolChannelFromCtx(ctx),
		ChatID:      ToolChatIDFromCtx(ctx),
		Timestamp:   time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		ActorType:   "agent",
		ActorID:     t.manager.agentKeyFromID(ctx, agentID),
	})

	// Blocker escalation: auto-fail task + notify leader.
	if commentType == "blocker" {
		return t.handleBlockerComment(ctx, team, task, taskID, agentID, text)
	}

	isLead := agentID == team.LeadAgentID
	msg := fmt.Sprintf("Comment added to task #%d \"%s\" (id: %s).", task.TaskNumber, task.Subject, taskID)
	switch {
	case isLead && task.Status == store.TeamTaskStatusInProgress:
		msg += " Note: the assignee is currently working and won't see this comment during execution. To redirect, wait for completion then use retry action with this task_id."
	case isLead && task.Status == store.TeamTaskStatusCompleted:
		msg += " Task is completed. Use retry action with this task_id to reopen and re-dispatch with your feedback."
	case isLead && task.Status == store.TeamTaskStatusFailed:
		msg += " Task failed. Use retry action with this task_id to re-dispatch with your feedback."
	case !isLead && task.OwnerAgentID != nil && *task.OwnerAgentID == agentID:
		msg += " Your comment will be included in the task report sent to the leader. Continue working on the task."
	}
	return NewResult(msg)
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
		return ErrorResult("only the assigned task owner can update progress. As team lead, task results arrive automatically when members complete their work.")
	}

	// Prevent progress regression — keep the higher value.
	if percent < task.ProgressPercent {
		percent = task.ProgressPercent
	}

	if err := t.manager.teamStore.UpdateTaskProgress(ctx, taskID, team.ID, percent, step); err != nil {
		return ErrorResult("failed to update progress: " + err.Error())
	}

	ownerKey := ""
	if task.OwnerAgentID != nil {
		ownerKey = t.manager.agentKeyFromID(ctx, *task.OwnerAgentID)
	}
	t.manager.broadcastTeamEvent(ctx, protocol.EventTeamTaskProgress, protocol.TeamTaskEventPayload{
		TeamID:          team.ID.String(),
		TaskID:          taskID.String(),
		TaskNumber:      task.TaskNumber,
		Subject:         task.Subject,
		Status:          store.TeamTaskStatusInProgress,
		OwnerAgentKey:   ownerKey,
		ProgressPercent: percent,
		ProgressStep:    step,
		UserID:          store.UserIDFromContext(ctx),
		Channel:         ToolChannelFromCtx(ctx),
		ChatID:          ToolChatIDFromCtx(ctx),
		Timestamp:       time.Now().UTC().Format("2006-01-02T15:04:05Z"),
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

	filePath, _ := args["path"].(string)
	if filePath == "" {
		return ErrorResult("path is required for attach action")
	}

	// Resolve to absolute path within team workspace.
	if !filepath.IsAbs(filePath) {
		if ws := ToolTeamWorkspaceFromCtx(ctx); ws != "" {
			filePath = filepath.Join(ws, filePath)
		}
	}
	filePath = filepath.Clean(filePath)

	// Verify task belongs to team.
	task, err := t.manager.teamStore.GetTask(ctx, taskID)
	if err != nil {
		return ErrorResult("task not found: " + err.Error())
	}
	if task.TeamID != team.ID {
		return ErrorResult("task does not belong to your team")
	}

	chatID := ToolChatIDFromCtx(ctx)
	if err := t.manager.teamStore.AttachFileToTask(ctx, &store.TeamTaskAttachmentData{
		TaskID:           taskID,
		TeamID:           team.ID,
		ChatID:           chatID,
		Path:             filePath,
		CreatedByAgentID: &agentID,
	}); err != nil {
		return ErrorResult("failed to attach file: " + err.Error())
	}

	return NewResult(fmt.Sprintf("File attached to task #%d \"%s\" (id: %s).", task.TaskNumber, task.Subject, taskID))
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
		// Batch-validate all blocker tasks in one query.
		if len(blockedBy) > 0 {
			depTasks, err := t.manager.teamStore.GetTasksByIDs(ctx, blockedBy)
			if err != nil {
				return ErrorResult("failed to validate blocked_by: " + err.Error())
			}
			depMap := make(map[uuid.UUID]*store.TeamTaskData, len(depTasks))
			for i := range depTasks {
				depMap[depTasks[i].ID] = &depTasks[i]
			}
			for _, id := range blockedBy {
				dt, ok := depMap[id]
				if !ok {
					return ErrorResult(fmt.Sprintf("blocked_by task %s not found", id))
				}
				if dt.TeamID != team.ID {
					return ErrorResult(fmt.Sprintf("blocked_by task %s belongs to a different team", id))
				}
				switch dt.Status {
				case store.TeamTaskStatusCompleted, store.TeamTaskStatusCancelled, store.TeamTaskStatusFailed:
					return ErrorResult(fmt.Sprintf(
						"blocked_by task %s (%s) is already %s. "+
							"Remove it from blocked_by — finished tasks cannot block new work.",
						id, dt.Subject, dt.Status))
				}
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

	t.manager.broadcastTeamEvent(ctx, protocol.EventTeamTaskUpdated, protocol.TeamTaskEventPayload{
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

	return NewResult(fmt.Sprintf("Task #%d \"%s\" updated (id: %s).", task.TaskNumber, task.Subject, taskID))
}
