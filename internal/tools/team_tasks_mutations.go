package tools

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

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
	// Store peer kind so dispatches preserve the correct session scope (group vs direct).
	if pk := ToolPeerKindFromCtx(ctx); pk != "" {
		taskMeta["peer_kind"] = pk
	}
	// Store local key so forum-topic routing works on deferred/unblocked dispatches.
	if lk := ToolLocalKeyFromCtx(ctx); lk != "" {
		taskMeta["local_key"] = lk
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
		Metadata:         taskMeta,
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
		return ErrorResult("only the assigned task owner can update progress. As team lead, task results arrive automatically when members complete their work.")
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
