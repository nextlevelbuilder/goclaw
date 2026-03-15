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
		content += fmt.Sprintf("\n\n[Team workspace: %s — all files you create will be saved here, accessible by the team lead and other members via workspace_read.]", ws)
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

	meta := map[string]string{
		"origin_channel":   originChannel,
		"origin_peer_kind": "direct",
		"origin_chat_id":   originChatID,
		"origin_user_id":   originUserID,
		"from_agent":       fromAgent,
		"to_agent":         ag.AgentKey,
		"team_task_id":     task.ID.String(),
		"team_id":          teamID.String(),
	}
	if localKey := ToolLocalKeyFromCtx(ctx); localKey != "" {
		meta["origin_local_key"] = localKey
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

// DispatchUnblockedTasks finds pending tasks with an assigned owner, claims them
// (pending → in_progress), and dispatches them immediately.
// Called after task completion/cancellation to start newly-unblocked work
// instead of waiting for the ticker (up to 5 min delay).
func (m *TeamToolManager) DispatchUnblockedTasks(ctx context.Context, teamID uuid.UUID) {
	tasks, err := m.teamStore.ListRecoverableTasks(ctx, teamID)
	if err != nil {
		return
	}
	for i := range tasks {
		task := &tasks[i]
		if task.Status == store.TeamTaskStatusPending && task.OwnerAgentID != nil {
			// Assign (pending → in_progress + lock) so consumer can auto-complete.
			if err := m.teamStore.AssignTask(ctx, task.ID, *task.OwnerAgentID, teamID); err != nil {
				slog.Warn("DispatchUnblockedTasks: assign failed", "task_id", task.ID, "error", err)
				continue
			}
			m.broadcastTeamEvent(protocol.EventTeamTaskAssigned, protocol.TeamTaskEventPayload{
				TeamID:        teamID.String(),
				TaskID:        task.ID.String(),
				Status:        store.TeamTaskStatusInProgress,
				OwnerAgentKey: m.agentKeyFromID(ctx, *task.OwnerAgentID),
				Timestamp:     time.Now().UTC().Format("2006-01-02T15:04:05Z"),
				ActorType:     "system",
				ActorID:       "dispatch_unblocked",
			})

			// Append completed blocker results so the member agent has context.
			if summary := m.buildBlockerResultsSummary(ctx, task); summary != "" {
				task.Description += "\n\n" + summary
			}

			// Restore leader's trace context (stored in task metadata during creation)
			// so the member agent's trace links back to the leader, not the completing member.
			dispatchCtx := m.restoreTraceContext(ctx, task)
			m.dispatchTaskToAgent(dispatchCtx, task, teamID, *task.OwnerAgentID)
		}
	}
}
