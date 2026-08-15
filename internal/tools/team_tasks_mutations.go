package tools

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// resolveTeamAndTask is the common preamble for task mutation actions:
// resolve team context + parse task ID from args.
func (t *TeamTasksTool) resolveTeamAndTask(ctx context.Context, args map[string]any) (*store.TeamData, uuid.UUID, uuid.UUID, error) {
	team, agentID, err := t.manager.ResolveTeam(ctx)
	if err != nil {
		return nil, uuid.Nil, uuid.Nil, err
	}
	taskID, err := resolveTaskID(ctx, args)
	if err != nil {
		return nil, uuid.Nil, uuid.Nil, err
	}
	return team, agentID, taskID, nil
}

func (t *TeamTasksTool) executeComment(ctx context.Context, args map[string]any) *Result {
	team, agentID, taskID, err := t.resolveTeamAndTask(ctx, args)
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
	task, err := t.manager.Store().GetTask(ctx, taskID)
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

	if err := t.manager.Store().AddTaskComment(ctx, &store.TeamTaskCommentData{
		TaskID:      taskID,
		AgentID:     &agentID,
		Content:     text,
		CommentType: commentType,
	}); err != nil {
		return ErrorResult("failed to add comment: " + err.Error())
	}

	t.manager.BroadcastTeamEvent(ctx, protocol.EventTeamTaskCommented, BuildTaskEventPayload(
		team.ID.String(), taskID.String(),
		"",
		"agent", t.manager.AgentKeyFromID(ctx, agentID),
		WithTaskInfo(task.TaskNumber, task.Subject),
		WithCommentText(truncatePreview(text, 500)),
		WithContextInfo(ctx),
	))

	// Record action flag after successful store operation.
	recordTaskAction(ctx, func(f *TaskActionFlags) {
		f.Commented = true
		if commentType == "blocker" {
			f.Escalated = true
		}
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
	team, agentID, taskID, err := t.resolveTeamAndTask(ctx, args)
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
	task, err := t.manager.Store().GetTask(ctx, taskID)
	if err != nil {
		return ErrorResult("task not found: " + err.Error())
	}
	if task.TeamID != team.ID {
		return ErrorResult("task does not belong to your team")
	}
	if task.OwnerAgentID == nil || *task.OwnerAgentID != agentID {
		return ErrorResult("only the assigned task owner can update progress. As team lead, task results arrive automatically when members complete their work.")
	}

	// Early exit: task already terminal — skip DB write entirely.
	// Reuses the task fetched above so no extra query needed.
	switch task.Status {
	case store.TeamTaskStatusCompleted, store.TeamTaskStatusFailed, store.TeamTaskStatusCancelled:
		return SilentResult(fmt.Sprintf("Task already %s — progress update skipped.", task.Status))
	}

	// Prevent progress regression — keep the higher value.
	if percent < task.ProgressPercent {
		percent = task.ProgressPercent
	}

	if err := t.manager.Store().UpdateTaskProgress(ctx, taskID, team.ID, percent, step); err != nil {
		// Status may have changed between GetTask and UpdateTaskProgress (race with completeTask).
		// Fast path: check in-memory turn flags before hitting DB again.
		if flags := TaskActionFlagsFromCtx(ctx); flags != nil && flags.Completed {
			return SilentResult("Task already completed — progress update skipped.")
		}
		// Slow path: re-query for stale recovery (pending) or concurrent status change.
		if current, getErr := t.manager.Store().GetTask(ctx, taskID); getErr == nil && current != nil {
			switch current.Status {
			case store.TeamTaskStatusCompleted, store.TeamTaskStatusFailed, store.TeamTaskStatusCancelled:
				return SilentResult(fmt.Sprintf("Task already %s — progress update skipped.", current.Status))
			case store.TeamTaskStatusPending:
				// Task was reset by stale recovery — re-assign and retry once.
				if t.manager.Store().AssignTask(ctx, taskID, agentID, team.ID) == nil {
					if t.manager.Store().UpdateTaskProgress(ctx, taskID, team.ID, percent, step) == nil {
						slog.Info("executeProgress: re-assigned stale-recovered task", "task_id", taskID)
						recordTaskAction(ctx, func(f *TaskActionFlags) { f.Progressed = true })
						return SilentResult(fmt.Sprintf("Progress updated: %d%% %s", percent, step))
					}
				}
			}
		}
		return ErrorResult("failed to update progress: " + err.Error())
	}
	// Record action flag after successful store operation.
	recordTaskAction(ctx, func(f *TaskActionFlags) { f.Progressed = true })

	ownerKey := ""
	if task.OwnerAgentID != nil {
		ownerKey = t.manager.AgentKeyFromID(ctx, *task.OwnerAgentID)
	}
	t.manager.BroadcastTeamEvent(ctx, protocol.EventTeamTaskProgress, BuildTaskEventPayload(
		team.ID.String(), taskID.String(),
		store.TeamTaskStatusInProgress,
		"", "",
		WithTaskInfo(task.TaskNumber, task.Subject),
		WithOwnerAgentKey(ownerKey),
		WithProgress(percent, step),
		WithContextInfo(ctx),
	))

	return SilentResult(fmt.Sprintf("Progress updated: %d%% %s", percent, step))
}

func (t *TeamTasksTool) executeAttach(ctx context.Context, args map[string]any) *Result {
	team, agentID, taskID, err := t.resolveTeamAndTask(ctx, args)
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
	task, err := t.manager.Store().GetTask(ctx, taskID)
	if err != nil {
		return ErrorResult("task not found: " + err.Error())
	}
	if task.TeamID != team.ID {
		return ErrorResult("task does not belong to your team")
	}

	chatID := ToolChatIDFromCtx(ctx)
	if err := t.manager.Store().AttachFileToTask(ctx, &store.TeamTaskAttachmentData{
		TaskID:           taskID,
		TeamID:           team.ID,
		ChatID:           chatID,
		Path:             filePath,
		CreatedByAgentID: &agentID,
	}); err != nil {
		return ErrorResult("failed to attach file: " + err.Error())
	}

	t.manager.BroadcastTeamEvent(ctx, protocol.EventTeamTaskAttachmentAdded, BuildTaskEventPayload(
		team.ID.String(), taskID.String(),
		"",
		"agent", t.manager.AgentKeyFromID(ctx, agentID),
		WithTaskInfo(task.TaskNumber, task.Subject),
		WithContextInfo(ctx),
	))

	return NewResult(fmt.Sprintf("File attached to task #%d \"%s\" (id: %s).", task.TaskNumber, task.Subject, taskID))
}

func (t *TeamTasksTool) executeUpdate(ctx context.Context, args map[string]any) *Result {
	team, agentID, taskID, err := t.resolveTeamAndTask(ctx, args)
	if err != nil {
		return ErrorResult(err.Error())
	}
	if err := t.manager.RequireLead(ctx, team, agentID); err != nil {
		return ErrorResult(err.Error())
	}

	// Verify task belongs to this team (prevent cross-team update).
	task, err := t.manager.Store().GetTask(ctx, taskID)
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
			depTasks, err := t.manager.Store().GetTasksByIDs(ctx, blockedBy)
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

	if err := t.manager.Store().UpdateTask(ctx, taskID, updates); err != nil {
		return ErrorResult("failed to update task: " + err.Error())
	}

	t.manager.BroadcastTeamEvent(ctx, protocol.EventTeamTaskUpdated, BuildTaskEventPayload(
		team.ID.String(), taskID.String(),
		task.Status,
		"agent", t.manager.AgentKeyFromID(ctx, agentID),
		WithSubject(task.Subject),
		WithContextInfo(ctx),
	))

	return NewResult(fmt.Sprintf("Task #%d \"%s\" updated (id: %s).", task.TaskNumber, task.Subject, taskID))
}
