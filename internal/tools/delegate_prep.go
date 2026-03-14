package tools

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func (dm *DelegateManager) prepareDelegation(ctx context.Context, opts DelegateOpts, mode string) (*DelegationTask, *store.AgentLinkData, error) {
	sourceAgentID := store.AgentIDFromContext(ctx)
	if sourceAgentID == uuid.Nil {
		return nil, nil, fmt.Errorf("delegation requires database stores (no agent ID in context)")
	}

	sourceAgent, err := dm.agentStore.GetByID(ctx, sourceAgentID)
	if err != nil {
		return nil, nil, fmt.Errorf("source agent not found: %w", err)
	}

	targetAgent, err := dm.agentStore.GetByKey(ctx, opts.TargetAgentKey)
	if err != nil {
		return nil, nil, fmt.Errorf("target agent %q not found", opts.TargetAgentKey)
	}

	link, err := dm.linkStore.GetLinkBetween(ctx, sourceAgentID, targetAgent.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to check delegation permission: %w", err)
	}
	if link == nil {
		return nil, nil, fmt.Errorf("no delegation link from this agent to %q. Available targets are listed in AGENTS.md", opts.TargetAgentKey)
	}

	userID := store.UserIDFromContext(ctx)
	if err := checkUserPermission(link.Settings, userID); err != nil {
		return nil, nil, err
	}

	// Resolve team once — used for task enforcement, validation, and access checks.
	var team *store.TeamData
	if dm.teamStore != nil {
		team, _ = dm.teamStore.GetTeamForAgent(ctx, sourceAgentID)
	}

	// Auto-create team task when team_task_id is omitted (v2 teams only).
	// This eliminates the two-step create→spawn dance that caused LLM hallucination
	// (LLM would call create+spawn in parallel, hallucinating the task_id).
	// Only the team lead can create tasks — members must ask the lead.
	if team != nil && opts.TeamTaskID == uuid.Nil && IsTeamV2(team) {
		if sourceAgentID != team.LeadAgentID {
			return nil, nil, fmt.Errorf("only the team lead can create team tasks — ask your lead to assign this task")
		}
		subject := opts.Label
		if subject == "" {
			subject = opts.Task
			if len(subject) > 100 {
				subject = subject[:100] + "..."
			}
		}
		taskData := &store.TeamTaskData{
			TeamID:           team.ID,
			Subject:          subject,
			Description:      opts.Task,
			Status:           store.TeamTaskStatusPending,
			UserID:           store.UserIDFromContext(ctx),
			Channel:          ToolChannelFromCtx(ctx),
			TaskType:         "delegation",
			CreatedByAgentID: &sourceAgentID,
			ChatID:           ToolChatIDFromCtx(ctx),
		}
		if err := dm.teamStore.CreateTask(ctx, taskData); err != nil {
			return nil, nil, fmt.Errorf("failed to auto-create team task: %w", err)
		}
		opts.TeamTaskID = taskData.ID
		slog.Info("delegate: auto-created team task",
			"task_id", taskData.ID, "subject", subject, "target", opts.TargetAgentKey)
	}

	// Validate that team_task_id belongs to the agent's team (prevent cross-team task completion).
	if dm.teamStore != nil && opts.TeamTaskID != uuid.Nil {
		teamTask, err := dm.teamStore.GetTask(ctx, opts.TeamTaskID)
		if err != nil {
			return nil, nil, fmt.Errorf(
				"team_task_id %s not found. Use team_tasks action=list to see available tasks, or omit team_task_id to auto-create.",
				opts.TeamTaskID)
		}

		// Guard: scope task to current user_id (prevent cross-group task leak).
		// user_id is the GROUP composite ID (e.g. "group:telegram:-1003701523276"), NOT the sender.
		// Delegate/system channels skip this check — they operate cross-context by design.
		currentUserID := store.UserIDFromContext(ctx)
		channel := ToolChannelFromCtx(ctx)
		if channel != ChannelDelegate && channel != ChannelSystem &&
			teamTask.UserID != "" && currentUserID != "" && teamTask.UserID != currentUserID {
			return nil, nil, fmt.Errorf(
				"team_task_id %s belongs to a different context. Omit team_task_id to auto-create a new task.",
				opts.TeamTaskID)
		}

		// Guard: reject completed/cancelled tasks — enforce "one task per delegation".
		if teamTask.Status == store.TeamTaskStatusCompleted || teamTask.Status == store.TeamTaskStatusCancelled {
			ownerLabel := "another agent"
			if teamTask.OwnerAgentKey != "" {
				ownerLabel = teamTask.OwnerAgentKey
			}
			return nil, nil, fmt.Errorf(
				"team_task_id %s is already %s (completed by %q). Omit team_task_id to auto-create a new task.",
				opts.TeamTaskID, teamTask.Status, ownerLabel)
		}

		// Guard: reject in-progress tasks — prevent multiple delegations on the same task.
		// This catches the pattern where an LLM reuses a team_task_id across parallel spawns.
		if teamTask.Status == store.TeamTaskStatusInProgress {
			ownerLabel := "another delegation"
			if teamTask.OwnerAgentKey != "" {
				ownerLabel = teamTask.OwnerAgentKey
			}
			return nil, nil, fmt.Errorf(
				"team_task_id %s is already in progress (claimed by %q). Each spawn needs its own task — omit team_task_id to auto-create.",
				opts.TeamTaskID, ownerLabel)
		}

		if team != nil {
			if teamTask.TeamID != team.ID {
				return nil, nil, fmt.Errorf("team_task_id does not belong to your team")
			}
			userID := store.UserIDFromContext(ctx)
			ch := ToolChannelFromCtx(ctx)
			if err := checkTeamAccess(team.Settings, userID, ch); err != nil {
				return nil, nil, fmt.Errorf("team access denied: %w", err)
			}
		}

		// Auto-populate task description from spawn prompt if empty.
		// This ensures the task board has full context for audit/visibility
		// without relying on the LLM to set description at task creation time.
		if teamTask.Description == "" && opts.Task != "" {
			_ = dm.teamStore.UpdateTask(ctx, opts.TeamTaskID, map[string]any{
				"description": opts.Task,
			})
		}

		// Claim task early so status moves to in_progress immediately (v2 only).
		// This prevents the pending reminder from re-triggering spawns for
		// tasks that are already running. The ClaimTask in autoCompleteTeamTask()
		// will harmlessly fail (WHERE status='pending' won't match).
		if team != nil && IsTeamV2(team) {
			if err := dm.teamStore.ClaimTask(ctx, opts.TeamTaskID, targetAgent.ID, teamTask.TeamID); err != nil {
				slog.Warn("delegate: task claim race — another delegation may have claimed this task",
					"task_id", opts.TeamTaskID, "target", opts.TargetAgentKey, "error", err)
			}
		}
	}

	linkCount := dm.ActiveCountForLink(sourceAgentID, targetAgent.ID)
	if link.MaxConcurrent > 0 && linkCount >= link.MaxConcurrent {
		return nil, nil, fmt.Errorf("delegation link to %q is at capacity (%d/%d active). Try again later or handle the task yourself",
			opts.TargetAgentKey, linkCount, link.MaxConcurrent)
	}

	targetCount := dm.ActiveCountForTarget(targetAgent.ID)
	maxLoad := parseMaxDelegationLoad(targetAgent.OtherConfig)
	if targetCount >= maxLoad {
		return nil, nil, fmt.Errorf("agent %q is at capacity (%d/%d active delegations). Either wait and retry, use a different agent, or handle the task yourself",
			opts.TargetAgentKey, targetCount, maxLoad)
	}

	channel := ToolChannelFromCtx(ctx)
	chatID := ToolChatIDFromCtx(ctx)
	peerKind := ToolPeerKindFromCtx(ctx)
	localKey := ToolLocalKeyFromCtx(ctx)

	delegationID := uuid.NewString()[:12]
	task := &DelegationTask{
		ID:             delegationID,
		SourceAgentID:  sourceAgentID,
		SourceAgentKey:    sourceAgent.AgentKey,
		SourceDisplayName: sourceAgent.DisplayName,
		TargetAgentID:  targetAgent.ID,
		TargetAgentKey:    opts.TargetAgentKey,
		TargetDisplayName: targetAgent.DisplayName,
		UserID:         userID,
		Task:           opts.Task,
		Status:         "running",
		Mode:           mode,
		SessionKey: fmt.Sprintf("delegate:%s:%s:%s",
			sourceAgentID.String()[:8], opts.TargetAgentKey, delegationID),
		CreatedAt:        time.Now(),
		OriginChannel:    channel,
		OriginChatID:     chatID,
		OriginPeerKind:   peerKind,
		OriginLocalKey:   localKey,
		OriginSessionKey: ToolSessionKeyFromCtx(ctx),
		OriginTraceID:    tracing.TraceIDFromContext(ctx),
		OriginRootSpanID: tracing.ParentSpanIDFromContext(ctx),
		TeamTaskID:       opts.TeamTaskID,
	}

	// Carry team_id from the link (for delegation history filtering by team)
	if link.TeamID != nil {
		task.TeamID = *link.TeamID
	}

	// Resolve progress notifications: per-team setting overrides global default.
	task.progressEnabled = dm.progressEnabled
	if team != nil {
		task.progressEnabled = parseProgressNotifications(team.Settings, dm.progressEnabled)
	}

	return task, link, nil
}

// injectDependencyResults fetches completed dependency results for a task's
// blocked_by prerequisites and prepends them to opts.Context. This ensures the
// delegate agent receives prior results without needing to search for them.
func (dm *DelegateManager) injectDependencyResults(ctx context.Context, opts *DelegateOpts) {
	if dm.teamStore == nil || opts.TeamTaskID == uuid.Nil {
		return
	}
	teamTask, err := dm.teamStore.GetTask(ctx, opts.TeamTaskID)
	if err != nil || len(teamTask.BlockedBy) == 0 {
		return
	}

	var depContext []string
	for _, depID := range teamTask.BlockedBy {
		dep, err := dm.teamStore.GetTask(ctx, depID)
		if err != nil || dep.Result == nil || *dep.Result == "" {
			continue
		}
		result := *dep.Result
		if len(result) > 8000 {
			result = result[:8000] + "\n[...truncated]"
		}
		agentLabel := dep.OwnerAgentKey
		if agentLabel == "" {
			agentLabel = "unknown"
		}
		depContext = append(depContext, fmt.Sprintf(
			"--- Result from dependency task %q (id=%s, by %s) ---\n%s",
			dep.Subject, dep.ID, agentLabel, result))
	}

	if len(depContext) > 0 {
		injected := strings.Join(depContext, "\n\n")
		if opts.Context != "" {
			opts.Context = injected + "\n\n" + opts.Context
		} else {
			opts.Context = injected
		}
	}
}

// injectWorkspaceContext lists workspace files and prepends metadata to opts.Context.
// Uses task.UserID as workspace scope (stable across WS reconnects).
func (dm *DelegateManager) injectWorkspaceContext(ctx context.Context, task *DelegationTask, opts *DelegateOpts) {
	if dm.teamStore == nil || task.TeamID == uuid.Nil {
		return
	}
	channel := ""
	chatID := task.UserID
	if chatID == "" {
		chatID = store.UserIDFromContext(ctx)
	}

	files, err := dm.teamStore.ListWorkspaceFiles(ctx, task.TeamID, channel, chatID)
	if err != nil || len(files) == 0 {
		return
	}

	var lines []string
	for _, f := range files {
		tag := ""
		if f.Pinned {
			tag = " [pinned]"
		}
		for _, t := range f.Tags {
			tag += " [" + t + "]"
		}
		lines = append(lines, fmt.Sprintf("- %s (%s, %d bytes, by %s)%s",
			f.FileName, f.MimeType, f.SizeBytes, f.UploadedByKey, tag))
	}
	wsCtx := "--- Team workspace files (use workspace_read to access) ---\n" +
		strings.Join(lines, "\n")

	if opts.Context != "" {
		opts.Context = wsCtx + "\n\n" + opts.Context
	} else {
		opts.Context = wsCtx
	}
}

// sendProgressNotification sends a grouped "still working" message listing all
// active delegations from the same source agent. Uses progressSent to dedup —
// concurrent tickers only send one notification per cycle, then release for next tick.
func (dm *DelegateManager) sendProgressNotification(task *DelegationTask) {
	if !task.progressEnabled {
		return
	}
	// Skip internal/delegate channels — only notify on real user-facing channels.
	if dm.msgBus == nil || task.OriginChannel == "" || task.OriginChatID == "" ||
		task.OriginChannel == ChannelDelegate || task.OriginChannel == ChannelSystem {
		return
	}

	// Dedup: one grouped notification per source agent per chat per tick cycle.
	dedupKey := task.SourceAgentID.String() + ":" + task.OriginChatID
	if _, loaded := dm.progressSent.LoadOrStore(dedupKey, true); loaded {
		return
	}
	defer dm.progressSent.Delete(dedupKey) // release for next tick

	// Collect all active delegations from same source agent.
	active := dm.ListActive(task.SourceAgentID)
	if len(active) == 0 {
		return
	}

	var lines []string
	for _, t := range active {
		elapsed := time.Since(t.CreatedAt).Round(time.Second)
		label := t.TargetAgentKey
		if t.TargetDisplayName != "" {
			label = fmt.Sprintf("%s (%s)", t.TargetDisplayName, t.TargetAgentKey)
		}
		// Include current activity if available
		phase, tool := t.GetActivity()
		activityStr := formatDelegateActivity(phase, tool)
		if activityStr != "" {
			lines = append(lines, fmt.Sprintf("- %s %s — %s", activityStr, label, elapsed))
		} else {
			lines = append(lines, fmt.Sprintf("- %s: %s", label, elapsed))
		}
	}

	content := fmt.Sprintf("🏗 Your team is working on it...\n%s", strings.Join(lines, "\n"))

	dm.msgBus.PublishOutbound(bus.OutboundMessage{
		Channel: task.OriginChannel,
		ChatID:  task.OriginChatID,
		Content: content,
		Metadata: map[string]string{
			"local_key": task.OriginLocalKey,
			"peer_kind": task.OriginPeerKind,
		},
	})

	// Emit WS progress event alongside the outbound channel message.
	var progressItems []protocol.DelegationProgressItem
	for _, t := range active {
		phase, tool := t.GetActivity()
		item := protocol.DelegationProgressItem{
			DelegationID:      t.ID,
			TargetAgentKey:    t.TargetAgentKey,
			TargetDisplayName: t.TargetDisplayName,
			ElapsedMS:         int(time.Since(t.CreatedAt).Milliseconds()),
			Activity:          phase,
			Tool:              tool,
		}
		if t.TeamTaskID != uuid.Nil {
			item.TeamTaskID = t.TeamTaskID.String()
		}
		progressItems = append(progressItems, item)
	}
	dm.msgBus.Broadcast(bus.Event{
		Name: protocol.EventDelegationProgress,
		Payload: protocol.DelegationProgressPayload{
			SourceAgentID:  task.SourceAgentID.String(),
			SourceAgentKey: task.SourceAgentKey,
			UserID:         task.UserID,
			Channel:        task.OriginChannel,
			ChatID:         task.OriginChatID,
			TeamID:         func() string { if task.TeamID != uuid.Nil { return task.TeamID.String() }; return "" }(),
			Active:         progressItems,
		},
	})
}

func buildDelegateMessage(opts DelegateOpts) string {
	if opts.Context != "" {
		return fmt.Sprintf("[Additional Context]\n%s\n\n[Task]\n%s", opts.Context, opts.Task)
	}
	return opts.Task
}

func (dm *DelegateManager) buildRunRequest(task *DelegationTask, message string) DelegateRunRequest {
	req := DelegateRunRequest{
		SessionKey: task.SessionKey,
		Message:    message,
		UserID:     task.UserID,
		Channel:    ChannelDelegate,
		ChatID:     task.OriginChatID,
		PeerKind:   task.OriginPeerKind,
		RunID:      fmt.Sprintf("delegate-%s", task.ID),
		Stream:     false,
		ExtraSystemPrompt: "[Delegation Context]\nYou are handling a delegated task from another agent.\n" +
			"- Focus exclusively on the delegated task below.\n" +
			"- Your complete response will be returned to the requesting agent.\n" +
			"- Do NOT try to communicate with the end user directly.\n" +
			"- Do NOT use your persona name or self-references (e.g. do not say your name). Write factual, neutral content.\n" +
			"- Be concise and deliver actionable results.\n" +
			"- IMPORTANT: If the delegated task falls outside your expertise scope (as defined in your SOUL.md), politely refuse and explain that this task is not within your domain. Do NOT attempt tasks outside your scope.",
		DelegationID:  task.ID,
		TeamID:        func() string { if task.TeamID != uuid.Nil { return task.TeamID.String() }; return "" }(),
		TeamTaskID:    func() string { if task.TeamTaskID != uuid.Nil { return task.TeamTaskID.String() }; return "" }(),
		ParentAgentID: task.SourceAgentKey,
	}

	// Propagate workspace scope to delegate so workspace tools write to the
	// origin user's workspace, not the "delegate" channel. Scope = userID.
	req.WorkspaceChannel = ""
	req.WorkspaceChatID = task.UserID

	// Propagate parent's recent image media to delegate for vision context.
	if dm.mediaLoader != nil && dm.sessionStore != nil && task.OriginSessionKey != "" {
		req.Media = dm.resolveParentMedia(task.OriginSessionKey)
	}

	return req
}

// formatDelegateActivity returns an emoji+label for the delegate's current phase.
func formatDelegateActivity(phase, tool string) string {
	switch phase {
	case "thinking":
		return "💭"
	case "tool_exec":
		if strings.HasPrefix(tool, "web") {
			return "🔍"
		}
		if tool == "exec" {
			return "⚡"
		}
		if tool == "spawn" || tool == "delegate" {
			return "👥"
		}
		return "🔧"
	case "compacting":
		return "📦"
	default:
		return ""
	}
}

// resolveParentMedia loads image media files from the parent session's recent MediaRefs.
func (dm *DelegateManager) resolveParentMedia(parentSessionKey string) []bus.MediaFile {
	history := dm.sessionStore.GetHistory(parentSessionKey)
	if len(history) == 0 {
		return nil
	}

	// Scan last 5 messages for image MediaRefs.
	var files []bus.MediaFile
	count := 0
	for i := len(history) - 1; i >= 0 && count < 5; i-- {
		if len(history[i].MediaRefs) == 0 {
			continue
		}
		for _, ref := range history[i].MediaRefs {
			if ref.Kind != "image" {
				continue
			}
			if p, err := dm.mediaLoader.LoadPath(ref.ID); err == nil {
				files = append(files, bus.MediaFile{Path: p, MimeType: ref.MimeType})
			}
		}
		count++
	}
	return files
}
