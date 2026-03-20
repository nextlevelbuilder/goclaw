package tools

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// maxTaskDispatches is the max number of times a single task can be dispatched
// before it auto-fails. Prevents infinite loops when agents can't complete a task.
const maxTaskDispatches = 3

// dispatchTaskToAgent publishes a teammate-style inbound message so the
// gateway consumer picks it up and runs the assigned agent, then auto-completes
// the task on success or auto-fails on error.
//
// Routing uses task.Channel/task.ChatID (set at creation time) as primary source,
// falling back to ctx only for initial dispatch when the task is created and dispatched
// in the same call. This ensures correct routing even when called from
// DispatchUnblockedTasks (where ctx is the member agent's context, not the lead's).
func (m *TeamToolManager) dispatchTaskToAgent(ctx context.Context, task *store.TeamTaskData, teamID, agentID uuid.UUID) {
	if m.msgBus == nil {
		return
	}

	// Circuit breaker: auto-fail tasks that have been dispatched too many times.
	dispatchCount := 0
	if dc, ok := task.Metadata["dispatch_count"].(float64); ok {
		dispatchCount = int(dc)
	}
	if dispatchCount >= maxTaskDispatches {
		slog.Warn("team_tasks.dispatch: max dispatch count reached, auto-failing task",
			"task_id", task.ID, "dispatch_count", dispatchCount)
		failReason := fmt.Sprintf("Task auto-failed after %d dispatch attempts", dispatchCount)
		_ = m.teamStore.UpdateTask(ctx, task.ID, map[string]any{
			"status": store.TeamTaskStatusFailed,
			"result": failReason,
		})
		return
	}

	// Increment dispatch count in metadata.
	if task.Metadata == nil {
		task.Metadata = make(map[string]any)
	}
	task.Metadata["dispatch_count"] = dispatchCount + 1
	_ = m.teamStore.UpdateTask(ctx, task.ID, map[string]any{"metadata": task.Metadata})

	ag, err := m.cachedGetAgentByID(ctx, agentID)
	if err != nil {
		slog.Warn("team_tasks.dispatch: cannot resolve agent", "agent_id", agentID, "error", err)
		return
	}

	content := fmt.Sprintf("[Assigned task #%d (id: %s)]: %s", task.TaskNumber, task.ID, task.Subject)
	if task.Description != "" {
		content += "\n\n" + task.Description
	}
	// Hint: tell the agent it's on a team task and where the shared workspace is.
	if ws := taskTeamWorkspace(task); ws != "" {
		content += fmt.Sprintf("\n\n[Team workspace: %s — use read_file/write_file/list_files to access shared files. All files you write are visible to the team lead and other members.]", ws)
	}
	// List attached files so member knows what's available to read.
	if files, ok := task.Metadata["attached_files"].([]any); ok && len(files) > 0 {
		content += "\n\n[Attached files in team workspace — use read_file to access:]"
		for _, f := range files {
			if path, ok := f.(string); ok {
				content += "\n- attachments/" + filepath.Base(path)
			}
		}
	}

	// Use task's stored channel/chat as primary source for routing.
	// Falls back to ctx values for initial dispatch (task just created, fields match ctx).
	originChannel := task.Channel
	if originChannel == "" {
		originChannel = ToolChannelFromCtx(ctx)
	}
	originChatID := task.ChatID
	if originChatID == "" {
		originChatID = ToolChatIDFromCtx(ctx)
	}
	// Resolve lead agent key for completion announce routing.
	fromAgent := ToolAgentKeyFromCtx(ctx)
	if team, err := m.teamStore.GetTeamForAgent(ctx, store.AgentIDFromContext(ctx)); err == nil && team != nil {
		if leadAg, err := m.cachedGetAgentByID(ctx, team.LeadAgentID); err == nil {
			fromAgent = leadAg.AgentKey
		}
	}

	// Resolve user ID: prefer context (available during leader's turn),
	// fall back to task's chat ID (stable for dispatches from consumer/ticker context).
	originUserID := store.UserIDFromContext(ctx)
	if originUserID == "" {
		originUserID = originChatID
	}

	// Resolve peer kind from context; fallback to task metadata, then "direct".
	originPeerKind := ToolPeerKindFromCtx(ctx)
	if originPeerKind == "" {
		if pk, ok := task.Metadata["peer_kind"].(string); ok && pk != "" {
			originPeerKind = pk
		} else {
			originPeerKind = "direct"
		}
	}

	meta := map[string]string{
		"origin_channel":   originChannel,
		"origin_peer_kind": originPeerKind,
		"origin_chat_id":   originChatID,
		"origin_user_id":   originUserID,
		"from_agent":          fromAgent,
		"to_agent":            ag.AgentKey,
		"to_agent_display":    ag.DisplayName,
		"team_task_id":        task.ID.String(),
		"team_id":             teamID.String(),
	}
	// Resolve local key from context; fallback to task metadata for deferred dispatches.
	localKey := ToolLocalKeyFromCtx(ctx)
	if localKey == "" {
		if lk, ok := task.Metadata["local_key"].(string); ok {
			localKey = lk
		}
	}
	if localKey != "" {
		meta["origin_local_key"] = localKey
	}
	// Resolve origin session key from context; fallback to task metadata for deferred dispatches.
	// WS sessions use non-standard key format that BuildScopedSessionKey() cannot reproduce.
	originSessionKey := ToolSessionKeyFromCtx(ctx)
	if originSessionKey == "" {
		if sk, ok := task.Metadata["origin_session_key"].(string); ok {
			originSessionKey = sk
		}
	}
	if originSessionKey != "" {
		meta["origin_session_key"] = originSessionKey
	}
	// Pass the team workspace dir so member agents write files to the shared folder.
	if ws := taskTeamWorkspace(task); ws != "" {
		meta["team_workspace"] = ws
	}
	// Propagate trace context so member agent's trace links back to the lead's trace,
	// and the announce-back run nests under the lead's root span.
	if traceID := tracing.TraceIDFromContext(ctx); traceID != uuid.Nil {
		meta["origin_trace_id"] = traceID.String()
	}
	if rootSpanID := tracing.ParentSpanIDFromContext(ctx); rootSpanID != uuid.Nil {
		meta["origin_root_span_id"] = rootSpanID.String()
	}

	if !m.msgBus.TryPublishInbound(bus.InboundMessage{
		Channel:  "system",
		SenderID: "teammate:dashboard",
		ChatID:   teamID.String(),
		Content:  content,
		UserID:   originUserID,
		AgentID:  ag.AgentKey,
		Metadata: meta,
	}) {
		slog.Warn("team_tasks.dispatch: inbound buffer full, task dispatch dropped — ticker will retry",
			"task_id", task.ID, "agent_key", ag.AgentKey)
		return
	}
	slog.Info("team_tasks.dispatch: sent task to agent",
		"task_id", task.ID,
		"agent_key", ag.AgentKey,
		"team_id", teamID,
	)
}

// buildBlockerResultsSummary fetches completed blocker tasks (stored in metadata
// during creation) and formats their results for inclusion in the dispatch content.
func (m *TeamToolManager) buildBlockerResultsSummary(ctx context.Context, task *store.TeamTaskData) string {
	if task.Metadata == nil {
		return ""
	}
	rawBlockers, ok := task.Metadata["original_blocked_by"]
	if !ok {
		return ""
	}
	blockerStrs, ok := rawBlockers.([]any)
	if !ok || len(blockerStrs) == 0 {
		return ""
	}

	var parts []string
	for _, raw := range blockerStrs {
		idStr, ok := raw.(string)
		if !ok {
			continue
		}
		blockerID, err := uuid.Parse(idStr)
		if err != nil {
			continue
		}
		bt, err := m.teamStore.GetTask(ctx, blockerID)
		if err != nil || bt == nil || bt.Result == nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("--- Result from blocker task #%d (%s) ---\n%s",
			bt.TaskNumber, bt.Subject, *bt.Result))
	}
	if len(parts) == 0 {
		return ""
	}
	return "=== Completed blocker task results ===\n\n" + strings.Join(parts, "\n\n")
}

// truncatePreview returns s truncated to maxRunes runes with "..." appended.
// Uses rune slicing to avoid splitting multi-byte UTF-8 characters.
func truncatePreview(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// buildRecentCommentsSummary fetches the 3 most recent comments on a task and
// formats them for inclusion in dispatch content. This ensures re-dispatched
// tasks (after reject, retry, stale recovery) include relevant context like
// rejection reasons or progress notes without mutating the task description.
func (m *TeamToolManager) buildRecentCommentsSummary(ctx context.Context, taskID uuid.UUID) string {
	comments, err := m.teamStore.ListRecentTaskComments(ctx, taskID, 3)
	if err != nil || len(comments) == 0 {
		return ""
	}

	var parts []string
	for _, c := range comments {
		author := "system"
		if c.AgentID != nil {
			author = m.agentKeyFromID(ctx, *c.AgentID)
		} else if c.UserID != "" {
			author = c.UserID
		}
		parts = append(parts, fmt.Sprintf("- [%s]: %s", author, truncatePreview(c.Content, 500)))
	}
	return "[Recent comments]\n" + strings.Join(parts, "\n")
}

// restoreTraceContext returns a context with the leader's trace IDs restored
// from task metadata. This is needed because DispatchUnblockedTasks runs in
// the member agent's context (during executeComplete), but the dispatch should
// link back to the leader's trace, not the member's.
func (m *TeamToolManager) restoreTraceContext(ctx context.Context, task *store.TeamTaskData) context.Context {
	if task.Metadata == nil {
		return ctx
	}
	if traceIDStr, ok := task.Metadata["origin_trace_id"].(string); ok {
		if traceID, err := uuid.Parse(traceIDStr); err == nil {
			ctx = tracing.WithTraceID(ctx, traceID)
		}
	}
	if spanIDStr, ok := task.Metadata["origin_root_span_id"].(string); ok {
		if spanID, err := uuid.Parse(spanIDStr); err == nil {
			ctx = tracing.WithParentSpanID(ctx, spanID)
		}
	}
	return ctx
}

// DispatchUnblockedTasks finds pending tasks with an assigned owner, claims the
// highest-priority task per owner (pending → in_progress), and dispatches it.
// Only one task per owner is dispatched per round — remaining tasks stay pending
// and get dispatched after the current one completes. This ensures priority
// ordering and prevents the cancellation bug where CancelSession kills innocent
// queued tasks sharing the same session.
// Called after task completion/cancellation to start newly-unblocked work
// instead of waiting for the ticker (up to 5 min delay).
func (m *TeamToolManager) DispatchUnblockedTasks(ctx context.Context, teamID uuid.UUID) {
	tasks, err := m.teamStore.ListRecoverableTasks(ctx, teamID)
	if err != nil {
		return
	}
	// Track which owners already have a task dispatched this round.
	// ListRecoverableTasks orders by priority DESC, created_at — so the first
	// pending task per owner is automatically the highest priority.
	dispatched := make(map[uuid.UUID]bool)
	for i := range tasks {
		task := &tasks[i]
		if task.Status != store.TeamTaskStatusPending || task.OwnerAgentID == nil {
			continue
		}
		ownerID := *task.OwnerAgentID
		if dispatched[ownerID] {
			continue // skip — this owner already has a higher-priority task dispatched
		}
		// Assign (pending → in_progress + lock) so consumer can auto-complete.
		if err := m.teamStore.AssignTask(ctx, task.ID, ownerID, teamID); err != nil {
			slog.Warn("DispatchUnblockedTasks: assign failed", "task_id", task.ID, "error", err)
			continue
		}
		dispatched[ownerID] = true
		m.broadcastTeamEvent(protocol.EventTeamTaskDispatched, protocol.TeamTaskEventPayload{
			TeamID:        teamID.String(),
			TaskID:        task.ID.String(),
			TaskNumber:    task.TaskNumber,
			Subject:       task.Subject,
			Status:        store.TeamTaskStatusInProgress,
			OwnerAgentKey: m.agentKeyFromID(ctx, ownerID),
			Channel:       task.Channel,
			ChatID:        task.ChatID,
			Timestamp:     time.Now().UTC().Format("2006-01-02T15:04:05Z"),
			ActorType:     "system",
			ActorID:       "dispatch_unblocked",
		})

		// Append completed blocker results so the member agent has context.
		if summary := m.buildBlockerResultsSummary(ctx, task); summary != "" {
			task.Description += "\n\n" + summary
		}

		// Append recent comments so re-dispatched tasks include rejection reasons,
		// progress notes, or other context from previous runs.
		if commentCtx := m.buildRecentCommentsSummary(ctx, task.ID); commentCtx != "" {
			task.Description += "\n\n" + commentCtx
		}

		// Restore leader's trace context (stored in task metadata during creation)
		// so the member agent's trace links back to the leader, not the completing member.
		dispatchCtx := m.restoreTraceContext(ctx, task)
		m.dispatchTaskToAgent(dispatchCtx, task, teamID, ownerID)
	}
}
