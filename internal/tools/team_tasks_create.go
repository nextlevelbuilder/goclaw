package tools

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func (t *TeamTasksTool) executeCreate(ctx context.Context, args map[string]any) *Result {
	team, agentID, err := t.manager.ResolveTeam(ctx)
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
	} else if err := t.manager.RequireLead(ctx, team, agentID); err != nil {
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
		depTask, err := t.manager.Store().GetTask(ctx, depID)
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
	assigneeID, err := t.manager.ResolveAgentByKey(ctx, assigneeKey)
	if err != nil {
		return ErrorResult(fmt.Sprintf("assignee %q not found: %v", assigneeKey, err))
	}
	// Verify assignee is a member of this team.
	members, err := t.manager.CachedListMembers(ctx, team.ID, agentID)
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

	// Compute team workspace via layered pipeline: tenant → team → user/chat.
	shared := IsSharedWorkspace(team.Settings)
	taskMeta := make(map[string]any)
	teamWsDir := ResolveWorkspace(t.manager.DataDir(),
		TenantLayer(store.TenantIDFromContext(ctx), store.TenantSlugFromContext(ctx)),
		TeamLayer(team.ID),
		UserChatLayer(chatID, shared),
	)
	taskMeta[TaskMetaTeamWorkspace] = teamWsDir
	// Auto-collect media files from current run to team workspace.
	// When leader received files from user and creates a task, copy those
	// files to the team workspace so members can access them via read_file.
	// Also rewrite any media paths in the description to point to the workspace copy,
	// since members can't access the original .media/ paths outside their workspace.
	if mediaPaths := RunMediaPathsFromCtx(ctx); len(mediaPaths) > 0 {
		if wsDir, _ := taskMeta[TaskMetaTeamWorkspace].(string); wsDir != "" {
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
		taskMeta[TaskMetaPeerKind] = pk
	}
	// Store local key so forum-topic routing works on deferred/unblocked dispatches.
	if lk := ToolLocalKeyFromCtx(ctx); lk != "" {
		taskMeta[TaskMetaLocalKey] = lk
	}
	// Store origin session key so deferred dispatches route announces correctly.
	// WS sessions use non-standard key format that BuildScopedSessionKey() cannot reproduce.
	if sk := ToolSessionKeyFromCtx(ctx); sk != "" {
		taskMeta[TaskMetaOriginSession] = sk
	}
	// Store leader's trace context so unblocked dispatch links back to the leader's trace.
	if traceID := tracing.TraceIDFromContext(ctx); traceID != uuid.Nil {
		taskMeta[TaskMetaOriginTrace] = traceID.String()
	}
	if rootSpanID := tracing.ParentSpanIDFromContext(ctx); rootSpanID != uuid.Nil {
		taskMeta[TaskMetaOriginRootSpan] = rootSpanID.String()
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

	if err := t.manager.Store().CreateTask(ctx, task); err != nil {
		return ErrorResult("failed to create task: " + err.Error())
	}

	// Auto-copy files referenced in subject+description from leader's personal workspace
	// to team workspace so members can access them.
	if isLead {
		personalWs := ToolWorkspaceFromCtx(ctx)
		searchText := subject + "\n" + description
		if n := autoShareFiles(searchText, personalWs, teamWsDir); n > 0 {
			slog.Info("team_tasks.create: auto-shared files", "count", n, "task_id", task.ID)
		}
	}

	// Persist media files copied during task creation as DB attachments,
	// so the UI and queries see them in team_task_attachments table.
	// (Members get auto-attached via WorkspaceInterceptor, but leaders
	// don't run inside a task context — handle explicitly here.)
	if files, ok := taskMeta["attached_files"].([]any); ok {
		for _, f := range files {
			filePath, ok := f.(string)
			if !ok || filePath == "" {
				continue
			}
			var fileSize int64
			if info, err := os.Stat(filePath); err == nil {
				fileSize = info.Size()
			}
			att := &store.TeamTaskAttachmentData{
				TaskID:           task.ID,
				TeamID:           team.ID,
				ChatID:           chatID,
				Path:             filePath,
				FileSize:         fileSize,
				MimeType:         mimeFromExt(filepath.Ext(filePath)),
				CreatedByAgentID: &agentID,
			}
			if err := t.manager.Store().AttachFileToTask(ctx, att); err != nil {
				slog.Warn("executeCreate: auto-attach media failed", "task_id", task.ID, "path", filePath, "error", err)
			}
		}
	}

	agentKey := t.manager.AgentKeyFromID(ctx, agentID)
	t.manager.BroadcastTeamEvent(ctx, protocol.EventTeamTaskCreated, BuildTaskEventPayload(
		team.ID.String(), task.ID.String(),
		status,
		"agent", agentKey,
		WithSubject(subject),
		WithContextInfo(ctx),
		WithTimestamp(task.CreatedAt.UTC().Format("2006-01-02T15:04:05Z")),
	))
	// Track for post-turn dispatch. If no post-turn hook (e.g. HTTP API), dispatch immediately.
	// Member requests with auto_dispatch=false stay pending for leader review — skip dispatch.
	if status == store.TeamTaskStatusPending && !skipAutoDispatch {
		if ptd := PendingTeamDispatchFromCtx(ctx); ptd != nil {
			ptd.Add(team.ID, task.ID)
		} else {
			// Fallback: assign (pending → in_progress + lock) then dispatch.
			if err := t.manager.Store().AssignTask(ctx, task.ID, assigneeID, team.ID); err != nil {
				slog.Warn("executeCreate: fallback assign failed", "task_id", task.ID, "error", err)
			} else {
				t.manager.BroadcastTeamEvent(ctx, protocol.EventTeamTaskDispatched, BuildTaskEventPayload(
					team.ID.String(), task.ID.String(),
					store.TeamTaskStatusInProgress,
					"system", "fallback_dispatch",
					WithTaskInfo(task.TaskNumber, task.Subject),
					WithOwnerAgentKey(t.manager.AgentKeyFromID(ctx, assigneeID)),
					WithChannel(task.Channel),
					WithChatID(task.ChatID),
					WithPeerKind(ToolPeerKindFromCtx(ctx)),
					WithLocalKey(ToolLocalKeyFromCtx(ctx)),
				))
				t.manager.DispatchTaskToAgent(ctx, task, team, assigneeID)
			}
		}
	}

	assigneeName := t.manager.AgentDisplayName(ctx, t.manager.AgentKeyFromID(ctx, assigneeID))
	if assigneeName == "" {
		assigneeName = t.manager.AgentKeyFromID(ctx, assigneeID)
	}
	msg := fmt.Sprintf("Task created: %s (id=%s, task_number=%d, status=%s, assignee=%s)", subject, task.ID, task.TaskNumber, status, assigneeName)

	// Soft guardrail: warn if subject suggests multiple deliverables.
	// Only checks subject (not description) — detailed descriptions are fine.
	if subject != "" {
		subjLower := strings.ToLower(subject)
		hasCompound := strings.Contains(subjLower, " and ") &&
			(strings.Contains(subjLower, "implement") || strings.Contains(subjLower, "create") ||
				strings.Contains(subjLower, "build") || strings.Contains(subjLower, "design") ||
				strings.Contains(subjLower, "write") || strings.Contains(subjLower, "develop"))
		if hasCompound {
			msg += "\n\nWarning: This task subject suggests multiple deliverables. Consider splitting into separate tasks if they need different skills."
		}
	}
	return NewResult(msg)
}
