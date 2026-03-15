package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const teamCacheTTL = 5 * time.Minute

// teamCacheEntry wraps cached team data + members with a timestamp for TTL expiration.
type teamCacheEntry struct {
	team     *store.TeamData
	members  []store.TeamMemberData // loaded together with team to avoid separate DB call
	cachedAt time.Time
}

// agentCacheEntry wraps cached agent data with a timestamp for TTL expiration.
type agentCacheEntry struct {
	agent    *store.AgentData
	cachedAt time.Time
}

// TeamToolManager is the shared backend for team_tasks and team_message tools.
// It resolves the calling agent's team from context and provides access to
// the team store, agent store, and message bus.
// Includes a TTL cache for team data to avoid DB queries on every tool call.
type TeamToolManager struct {
	teamStore   store.TeamStore
	agentStore  store.AgentStore
	msgBus      *bus.MessageBus
	dataDir     string // base data directory for workspace path resolution
	teamCache   sync.Map // agentID (uuid.UUID) → *teamCacheEntry
	agentCache    sync.Map // agentID (uuid.UUID) → *agentCacheEntry
	agentKeyCache sync.Map // agentKey (string) → *agentCacheEntry
}

func NewTeamToolManager(teamStore store.TeamStore, agentStore store.AgentStore, msgBus *bus.MessageBus, dataDir string) *TeamToolManager {
	return &TeamToolManager{teamStore: teamStore, agentStore: agentStore, msgBus: msgBus, dataDir: dataDir}
}

// resolveTeam returns the team that the calling agent belongs to.
// When ToolTeamIDFromCtx is set (task dispatch), uses that team ID directly
// instead of GetTeamForAgent — prevents wrong team resolution for multi-team agents.
// Uses a TTL cache to avoid repeated DB queries. Access control
// (user/channel) is checked on every call regardless of cache hit.
func (m *TeamToolManager) resolveTeam(ctx context.Context) (*store.TeamData, uuid.UUID, error) {
	agentID := store.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return nil, uuid.Nil, fmt.Errorf("no agent context — team tools require database stores")
	}

	// If team ID is explicitly set in context (from task dispatch), use it directly.
	// This prevents wrong team resolution when an agent belongs to multiple teams.
	if teamIDStr := ToolTeamIDFromCtx(ctx); teamIDStr != "" {
		teamUUID, err := uuid.Parse(teamIDStr)
		if err == nil && teamUUID != uuid.Nil {
			team, err := m.teamStore.GetTeam(ctx, teamUUID)
			if err != nil {
				slog.Warn("workspace: resolveTeam by context ID failed", "team_id", teamIDStr, "error", err)
				// Fall through to normal resolution
			} else if team != nil {
				return team, agentID, nil
			}
		}
	}

	// Check cache first
	if entry, ok := m.teamCache.Load(agentID); ok {
		ce := entry.(*teamCacheEntry)
		if time.Since(ce.cachedAt) < teamCacheTTL {
			// Cache hit — still check access (user/channel vary per call)
			userID := store.UserIDFromContext(ctx)
			channel := ToolChannelFromCtx(ctx)
			if err := checkTeamAccess(ce.team.Settings, userID, channel); err != nil {
				return nil, uuid.Nil, err
			}
			return ce.team, agentID, nil
		}
		m.teamCache.Delete(agentID) // expired
	}

	// Cache miss → DB
	team, err := m.teamStore.GetTeamForAgent(ctx, agentID)
	if err != nil {
		slog.Warn("workspace: resolveTeam DB error", "agent_id", agentID, "error", err)
		return nil, uuid.Nil, fmt.Errorf("failed to resolve team: %w", err)
	}
	if team == nil {
		slog.Warn("workspace: agent has no team", "agent_id", agentID)
		return nil, uuid.Nil, fmt.Errorf("this agent is not part of any team")
	}

	// Store in cache (load members eagerly to avoid separate DB call later)
	members, _ := m.teamStore.ListMembers(ctx, team.ID)
	m.teamCache.Store(agentID, &teamCacheEntry{team: team, members: members, cachedAt: time.Now()})

	// Check access
	userID := store.UserIDFromContext(ctx)
	channel := ToolChannelFromCtx(ctx)
	if err := checkTeamAccess(team.Settings, userID, channel); err != nil {
		return nil, uuid.Nil, err
	}

	return team, agentID, nil
}

// requireLead checks if the calling agent is the team lead.
// Delegate/system channels bypass this check (they act on behalf of the lead).
func (m *TeamToolManager) requireLead(ctx context.Context, team *store.TeamData, agentID uuid.UUID) error {
	channel := ToolChannelFromCtx(ctx)
	if channel == ChannelDelegate || channel == ChannelSystem {
		return nil
	}
	if agentID != team.LeadAgentID {
		return fmt.Errorf("only the team lead can perform this action")
	}
	return nil
}

// InvalidateTeam clears all cached team + member data.
// Called when team membership, settings, or links change.
// Full clear is acceptable because team mutations are rare (admin-initiated).
func (m *TeamToolManager) InvalidateTeam() {
	m.teamCache.Range(func(k, _ any) bool { m.teamCache.Delete(k); return true })
}

// InvalidateAgentCache clears all cached agent data (by ID and by key).
// Called via pub/sub when agent data changes (update/delete).
func (m *TeamToolManager) InvalidateAgentCache() {
	m.agentCache.Range(func(k, _ any) bool { m.agentCache.Delete(k); return true })
	m.agentKeyCache.Range(func(k, _ any) bool { m.agentKeyCache.Delete(k); return true })
}

// cachedGetAgentByID returns agent data from cache or DB with TTL.
func (m *TeamToolManager) cachedGetAgentByID(ctx context.Context, id uuid.UUID) (*store.AgentData, error) {
	if entry, ok := m.agentCache.Load(id); ok {
		ce := entry.(*agentCacheEntry)
		if time.Since(ce.cachedAt) < teamCacheTTL {
			return ce.agent, nil
		}
		m.agentCache.Delete(id)
	}
	ag, err := m.agentStore.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	e := &agentCacheEntry{agent: ag, cachedAt: now}
	m.agentCache.Store(id, e)
	m.agentKeyCache.Store(ag.AgentKey, e)
	return ag, nil
}

// cachedGetAgentByKey returns agent data from cache or DB with TTL.
func (m *TeamToolManager) cachedGetAgentByKey(ctx context.Context, key string) (*store.AgentData, error) {
	if entry, ok := m.agentKeyCache.Load(key); ok {
		ce := entry.(*agentCacheEntry)
		if time.Since(ce.cachedAt) < teamCacheTTL {
			return ce.agent, nil
		}
		m.agentKeyCache.Delete(key)
	}
	ag, err := m.agentStore.GetByKey(ctx, key)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	e := &agentCacheEntry{agent: ag, cachedAt: now}
	m.agentKeyCache.Store(key, e)
	m.agentCache.Store(ag.ID, e)
	return ag, nil
}

// cachedListMembers returns members from the team cache if available, or falls back to DB.
func (m *TeamToolManager) cachedListMembers(ctx context.Context, teamID uuid.UUID, agentID uuid.UUID) ([]store.TeamMemberData, error) {
	if entry, ok := m.teamCache.Load(agentID); ok {
		ce := entry.(*teamCacheEntry)
		if time.Since(ce.cachedAt) < teamCacheTTL && ce.team.ID == teamID && ce.members != nil {
			return ce.members, nil
		}
	}
	return m.teamStore.ListMembers(ctx, teamID)
}

// resolveAgentByKey looks up an agent by key and returns its UUID.
func (m *TeamToolManager) resolveAgentByKey(key string) (uuid.UUID, error) {
	ag, err := m.cachedGetAgentByKey(context.Background(), key)
	if err != nil {
		return uuid.Nil, fmt.Errorf("agent %q not found: %w", key, err)
	}
	return ag.ID, nil
}

// agentKeyFromID returns the agent_key for a given UUID.
func (m *TeamToolManager) agentKeyFromID(ctx context.Context, id uuid.UUID) string {
	ag, err := m.cachedGetAgentByID(ctx, id)
	if err != nil {
		return id.String()
	}
	return ag.AgentKey
}

// taskTeamWorkspace extracts the team_workspace path from task metadata.
func taskTeamWorkspace(task *store.TeamTaskData) string {
	if task.Metadata == nil {
		return ""
	}
	ws, _ := task.Metadata["team_workspace"].(string)
	return ws
}

// dispatchTaskToAgent publishes a teammate-style inbound message so the
// gateway consumer picks it up and runs the assigned agent, then auto-completes
// the task on success or auto-fails on error.
//
// Routing uses task.Channel/task.ChatID (set at creation time) as primary source,
// falling back to ctx only for initial dispatch when the task is created and dispatched
// in the same call. This ensures correct routing even when called from
// DispatchUnblockedTasks (where ctx is the member agent's context, not the lead's).
// maxTaskDispatches is the max number of times a single task can be dispatched
// before it auto-fails. Prevents infinite loops when agents can't complete a task.
const maxTaskDispatches = 3

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

// PostTurnProcessor validates and dispatches pending team tasks after an agent turn.
type PostTurnProcessor interface {
	ProcessPendingTasks(ctx context.Context, teamID uuid.UUID, taskIDs []uuid.UUID) error
	// DispatchUnblockedTasks finds pending tasks with an owner and dispatches them.
	// Called by the consumer after auto-completing a task to unblock dependent work.
	DispatchUnblockedTasks(ctx context.Context, teamID uuid.UUID)
}

// ProcessPendingTasks validates tasks created during a turn and dispatches unblocked ones.
// Called by the consumer after an agent turn ends.
func (m *TeamToolManager) ProcessPendingTasks(ctx context.Context, teamID uuid.UUID, taskIDs []uuid.UUID) error {
	if len(taskIDs) == 0 {
		return nil
	}

	// Fetch all tasks created in this turn.
	tasks := make([]*store.TeamTaskData, 0, len(taskIDs))
	for _, id := range taskIDs {
		t, err := m.teamStore.GetTask(ctx, id)
		if err != nil {
			slog.Warn("post_turn: cannot fetch task", "task_id", id, "error", err)
			continue
		}
		tasks = append(tasks, t)
	}
	if len(tasks) == 0 {
		return nil
	}

	// Build lookup: taskID → task (for cycle/ref validation).
	taskMap := make(map[uuid.UUID]*store.TeamTaskData, len(tasks))
	for _, t := range tasks {
		taskMap[t.ID] = t
	}

	// Validate blocked_by references and detect cycles.
	cycled, invalidRef := validateBlockedBy(taskMap)

	// Fail tasks with invalid blocked_by references.
	for taskID, badRef := range invalidRef {
		task := taskMap[taskID]
		errMsg := fmt.Sprintf("blocked_by references non-existent task %s", badRef)
		if err := m.teamStore.FailPendingTask(ctx, taskID, teamID, errMsg); err != nil {
			slog.Warn("post_turn: FailPendingTask error", "task_id", taskID, "error", err)
		}
		m.broadcastTeamEvent(protocol.EventTeamTaskFailed, protocol.TeamTaskEventPayload{
			TeamID:    teamID.String(),
			TaskID:    taskID.String(),
			Status:    store.TeamTaskStatusFailed,
			Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
			ActorType: "system",
			ActorID:   "post_turn",
		})
		slog.Warn("post_turn: task failed — invalid blocked_by",
			"task_id", taskID, "identifier", task.Identifier, "bad_ref", badRef)
	}

	// Fail cycled tasks and notify leader.
	if len(cycled) > 0 {
		m.failCycledTasks(ctx, teamID, cycled, taskMap)
	}

	// Dispatch pending assigned tasks (not blocked, not failed).
	for _, task := range tasks {
		if _, isCycled := cycled[task.ID]; isCycled {
			continue
		}
		if _, isInvalid := invalidRef[task.ID]; isInvalid {
			continue
		}
		if task.Status != store.TeamTaskStatusPending || task.OwnerAgentID == nil {
			continue
		}
		if err := m.teamStore.AssignTask(ctx, task.ID, *task.OwnerAgentID, teamID); err != nil {
			slog.Warn("post_turn: assign failed", "task_id", task.ID, "error", err)
			continue
		}
		m.broadcastTeamEvent(protocol.EventTeamTaskAssigned, protocol.TeamTaskEventPayload{
			TeamID:        teamID.String(),
			TaskID:        task.ID.String(),
			Status:        store.TeamTaskStatusInProgress,
			OwnerAgentKey: m.agentKeyFromID(ctx, *task.OwnerAgentID),
			Timestamp:     time.Now().UTC().Format("2006-01-02T15:04:05Z"),
			ActorType:     "system",
			ActorID:       "post_turn",
		})
		// Restore leader's trace context from task metadata (ctx here is the
		// consumer goroutine context which has no trace after the turn ends).
		dispatchCtx := m.restoreTraceContext(ctx, task)
		m.dispatchTaskToAgent(dispatchCtx, task, teamID, *task.OwnerAgentID)
	}

	slog.Info("post_turn: processed pending tasks",
		"team_id", teamID,
		"total", len(tasks),
		"dispatched", countPendingAssigned(tasks, cycled, invalidRef),
	)
	return nil
}

// failCycledTasks fails all tasks in the cycle and notifies the leader.
func (m *TeamToolManager) failCycledTasks(ctx context.Context, teamID uuid.UUID, cycled map[uuid.UUID]bool, taskMap map[uuid.UUID]*store.TeamTaskData) {
	// Build cycle description using task identifiers.
	var ids []string
	for id := range cycled {
		if t := taskMap[id]; t != nil {
			ids = append(ids, t.Identifier)
		}
	}
	cycleDesc := fmt.Sprintf("Circular dependency detected among tasks: %s", strings.Join(ids, " → "))

	for id := range cycled {
		if err := m.teamStore.FailPendingTask(ctx, id, teamID, cycleDesc); err != nil {
			slog.Warn("post_turn: FailPendingTask (cycle) error", "task_id", id, "error", err)
		}
		m.broadcastTeamEvent(protocol.EventTeamTaskFailed, protocol.TeamTaskEventPayload{
			TeamID:    teamID.String(),
			TaskID:    id.String(),
			Status:    store.TeamTaskStatusFailed,
			Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
			ActorType: "system",
			ActorID:   "post_turn",
		})
	}

	// Notify leader via system message.
	m.notifyLeaderCycleError(ctx, teamID, cycleDesc)
}

// notifyLeaderCycleError sends a system message to the leader about cycled tasks.
func (m *TeamToolManager) notifyLeaderCycleError(ctx context.Context, teamID uuid.UUID, cycleDesc string) {
	if m.msgBus == nil {
		return
	}
	team, err := m.teamStore.GetTeam(ctx, teamID)
	if err != nil {
		return
	}
	leadAgent, err := m.cachedGetAgentByID(ctx, team.LeadAgentID)
	if err != nil {
		return
	}
	content := fmt.Sprintf("[System] %s\nPlease recreate these tasks with corrected dependencies.", cycleDesc)
	m.msgBus.TryPublishInbound(bus.InboundMessage{
		Channel:  "system",
		SenderID: "teammate:system",
		ChatID:   teamID.String(),
		AgentID:  leadAgent.AgentKey,
		UserID:   team.CreatedBy,
		Content:  content,
		Metadata: map[string]string{
			"team_id":    teamID.String(),
			"from_agent": leadAgent.AgentKey,
			"to_agent":   leadAgent.AgentKey,
		},
	})
}

// validateBlockedBy checks blocked_by references and detects cycles using Kahn's algorithm.
// Only validates within the batch — out-of-batch blocked_by refs are assumed valid
// (already validated by executeCreate). Self-blocking is caught as invalid.
//
// Note: Cross-turn blocked_by references (tasks from previous turns) work at the DB
// level via unblockDependentTasks (WHERE $1 = ANY(blocked_by)). Cycle detection here
// only considers edges within the current batch — cross-turn deps are external.
//
// Returns: cycled task IDs, and map of taskID → first invalid blocked_by ref.
func validateBlockedBy(taskMap map[uuid.UUID]*store.TeamTaskData) (cycled map[uuid.UUID]bool, invalidRef map[uuid.UUID]uuid.UUID) {
	invalidRef = make(map[uuid.UUID]uuid.UUID)

	// Collect all task IDs in this batch.
	batchIDs := make(map[uuid.UUID]bool, len(taskMap))
	for id := range taskMap {
		batchIDs[id] = true
	}

	// Check for self-blocking only. Out-of-batch blocked_by refs are valid
	// (tasks from previous turns, already validated by executeCreate).
	for id, task := range taskMap {
		for _, dep := range task.BlockedBy {
			if dep == id {
				invalidRef[id] = dep
				break
			}
		}
	}

	// Cycle detection via Kahn's algorithm.
	// Only considers edges within the batch — out-of-batch deps are external
	// and don't participate in cycle detection.
	inDegree := make(map[uuid.UUID]int)
	adj := make(map[uuid.UUID][]uuid.UUID) // blocker → dependents

	for id, task := range taskMap {
		if _, bad := invalidRef[id]; bad {
			continue
		}
		if _, exists := inDegree[id]; !exists {
			inDegree[id] = 0
		}
		for _, dep := range task.BlockedBy {
			if _, bad := invalidRef[dep]; bad {
				continue
			}
			// Only consider edges within the batch for cycle detection.
			if !batchIDs[dep] {
				continue
			}
			adj[dep] = append(adj[dep], id)
			inDegree[id]++
		}
	}

	// BFS: process nodes with in-degree 0.
	var queue []uuid.UUID
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}
	processed := make(map[uuid.UUID]bool)
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		processed[node] = true
		for _, dependent := range adj[node] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	// Any node not processed is part of a cycle.
	cycled = make(map[uuid.UUID]bool)
	for id := range inDegree {
		if !processed[id] {
			cycled[id] = true
		}
	}
	return cycled, invalidRef
}

// countPendingAssigned counts tasks that were eligible for dispatch.
func countPendingAssigned(tasks []*store.TeamTaskData, cycled map[uuid.UUID]bool, invalidRef map[uuid.UUID]uuid.UUID) int {
	n := 0
	for _, t := range tasks {
		if _, c := cycled[t.ID]; c {
			continue
		}
		if _, i := invalidRef[t.ID]; i {
			continue
		}
		if t.Status == store.TeamTaskStatusPending && t.OwnerAgentID != nil {
			n++
		}
	}
	return n
}

// broadcastTeamEvent sends a real-time event via the message bus for team activity visibility.
func (m *TeamToolManager) broadcastTeamEvent(name string, payload any) {
	if m.msgBus == nil {
		return
	}
	m.msgBus.Broadcast(bus.Event{
		Name:    name,
		Payload: payload,
	})
}

// resolveTeamRole returns the calling agent's role in the team.
// Unlike requireLead(), this does NOT bypass for delegate channel —
// workspace RBAC must respect actual roles even during delegation.
func (m *TeamToolManager) resolveTeamRole(ctx context.Context, team *store.TeamData, agentID uuid.UUID) (string, error) {
	if agentID == team.LeadAgentID {
		return store.TeamRoleLead, nil
	}
	members, err := m.cachedListMembers(ctx, team.ID, agentID)
	if err != nil {
		return "", fmt.Errorf("failed to resolve team role: %w", err)
	}
	for _, member := range members {
		if member.AgentID == agentID {
			return member.Role, nil
		}
	}
	return "", fmt.Errorf("agent is not a member of this team")
}

// agentDisplayName returns the display name for an agent key, falling back to empty string.
func (m *TeamToolManager) agentDisplayName(ctx context.Context, key string) string {
	ag, err := m.cachedGetAgentByKey(ctx, key)
	if err != nil || ag.DisplayName == "" {
		return ""
	}
	return ag.DisplayName
}

// ============================================================
// Version helpers
// ============================================================

// IsTeamV2 checks if team has version >= 2 in settings.
// Returns false for nil team, nil/empty settings, or version < 2.
func IsTeamV2(team *store.TeamData) bool {
	if team == nil || team.Settings == nil {
		return false
	}
	var s struct {
		Version int `json:"version"`
	}
	if json.Unmarshal(team.Settings, &s) != nil {
		return false
	}
	return s.Version >= 2
}

// ============================================================
// Follow-up settings helpers
// ============================================================

const (
	defaultFollowupDelayMinutes = 30
	defaultFollowupMaxReminders = 0 // 0 = unlimited
)

// followupDelayMinutes returns the team's followup_interval_minutes setting, or the default.
// Returns 0 for v1 teams (followup disabled).
func (m *TeamToolManager) followupDelayMinutes(team *store.TeamData) int {
	if !IsTeamV2(team) {
		return 0
	}
	if team.Settings == nil {
		return defaultFollowupDelayMinutes
	}
	var settings map[string]any
	if json.Unmarshal(team.Settings, &settings) != nil {
		return defaultFollowupDelayMinutes
	}
	if v, ok := settings["followup_interval_minutes"].(float64); ok && v > 0 {
		return int(v)
	}
	return defaultFollowupDelayMinutes
}

// followupMaxReminders returns the team's followup_max_reminders setting, or the default.
// Returns 0 for v1 teams (followup disabled).
func (m *TeamToolManager) followupMaxReminders(team *store.TeamData) int {
	if !IsTeamV2(team) {
		return 0
	}
	if team.Settings == nil {
		return defaultFollowupMaxReminders
	}
	var settings map[string]any
	if json.Unmarshal(team.Settings, &settings) != nil {
		return defaultFollowupMaxReminders
	}
	if v, ok := settings["followup_max_reminders"].(float64); ok && v >= 0 {
		return int(v)
	}
	return defaultFollowupMaxReminders
}

// ============================================================
// Escalation policy
// ============================================================

// EscalationResult indicates how an action should be handled.
type EscalationResult int

const (
	EscalationNone   EscalationResult = iota // no escalation configured
	EscalationAuto                           // LLM chooses (currently: always review)
	EscalationReview                         // create review task
	EscalationReject                         // reject outright
)

// checkEscalation parses the team's escalation_mode and escalation_actions settings.
// Returns EscalationNone for v1 teams.
func (m *TeamToolManager) checkEscalation(team *store.TeamData, action string) EscalationResult {
	if !IsTeamV2(team) {
		return EscalationNone
	}
	if team.Settings == nil {
		return EscalationNone
	}
	var settings map[string]any
	if err := json.Unmarshal(team.Settings, &settings); err != nil {
		return EscalationNone
	}

	mode, _ := settings["escalation_mode"].(string)
	if mode == "" {
		return EscalationNone
	}

	// Check if action is in escalation_actions list.
	actionsRaw, _ := settings["escalation_actions"].([]any)
	if len(actionsRaw) > 0 {
		found := false
		for _, a := range actionsRaw {
			if s, ok := a.(string); ok && s == action {
				found = true
				break
			}
		}
		if !found {
			return EscalationNone
		}
	}

	switch mode {
	case "auto":
		return EscalationAuto
	case "review":
		return EscalationReview
	case "reject":
		return EscalationReject
	default:
		return EscalationNone
	}
}

// createEscalationTask creates an escalation task and broadcasts the event.
func (m *TeamToolManager) createEscalationTask(ctx context.Context, team *store.TeamData, agentID uuid.UUID, subject, description string) *Result {
	task := &store.TeamTaskData{
		TeamID:           team.ID,
		Subject:          subject,
		Description:      description,
		Status:           store.TeamTaskStatusPending,
		UserID:           store.UserIDFromContext(ctx),
		Channel:          ToolChannelFromCtx(ctx),
		TaskType:         "escalation",
		CreatedByAgentID: &agentID,
		ChatID:           ToolChatIDFromCtx(ctx),
	}
	if err := m.teamStore.CreateTask(ctx, task); err != nil {
		return ErrorResult("failed to create escalation task: " + err.Error())
	}

	m.broadcastTeamEvent(protocol.EventTeamTaskCreated, protocol.TeamTaskEventPayload{
		TeamID:    team.ID.String(),
		TaskID:    task.ID.String(),
		Subject:   subject,
		Status:    store.TeamTaskStatusPending,
		UserID:    store.UserIDFromContext(ctx),
		Channel:   ToolChannelFromCtx(ctx),
		ChatID:    ToolChatIDFromCtx(ctx),
		Timestamp: task.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	})

	// Notify channel if possible.
	m.notifyChannelReview(task)

	return NewResult(fmt.Sprintf("Action requires approval. Escalation task created: %s (id=%s). A human must approve before this action can proceed.", subject, task.Identifier))
}

// notifyChannelReview publishes an outbound message to the origin channel about a pending review.
func (m *TeamToolManager) notifyChannelReview(task *store.TeamTaskData) {
	if m.msgBus == nil || task.Channel == "" || task.ChatID == "" {
		return
	}
	content := fmt.Sprintf("🔔 Escalation: \"%s\" requires human review (task %s).", task.Subject, task.Identifier)
	m.msgBus.PublishOutbound(bus.OutboundMessage{
		Channel: task.Channel,
		ChatID:  task.ChatID,
		Content: content,
	})
}
