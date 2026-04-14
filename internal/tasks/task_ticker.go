package tasks

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
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const (
	defaultRecoveryInterval  = 5 * time.Minute
	defaultStaleThreshold    = 2 * time.Hour
	defaultInReviewThreshold = 4 * time.Hour
	followupCooldown         = 5 * time.Minute
	defaultFollowupInterval  = 30 * time.Minute
)

// TaskTicker periodically recovers stale tasks and re-dispatches pending work.
// All recovery/stale/followup queries are batched across v2 active teams (single SQL each).
type TaskTicker struct {
	teams    store.TeamStore
	agents   store.AgentStore
	msgBus   *bus.MessageBus
	interval time.Duration

	stopCh chan struct{}
	wg     sync.WaitGroup

	mu               sync.Mutex
	lastFollowupSent map[uuid.UUID]time.Time // taskID → last followup sent time
}

func NewTaskTicker(teams store.TeamStore, agents store.AgentStore, msgBus *bus.MessageBus, intervalSec int) *TaskTicker {
	interval := defaultRecoveryInterval
	if intervalSec > 0 {
		interval = time.Duration(intervalSec) * time.Second
	}
	return &TaskTicker{
		teams:            teams,
		agents:           agents,
		msgBus:           msgBus,
		interval:         interval,
		stopCh:           make(chan struct{}),
		lastFollowupSent: make(map[uuid.UUID]time.Time),
	}
}

// Start launches the background recovery loop.
func (t *TaskTicker) Start() {
	t.wg.Add(1)
	go t.loop()
	slog.Info("task ticker started", "interval", t.interval)
}

// Stop signals the ticker to stop and waits for completion.
func (t *TaskTicker) Stop() {
	close(t.stopCh)
	t.wg.Wait()
	slog.Info("task ticker stopped")
}

func (t *TaskTicker) loop() {
	defer t.wg.Done()

	// On startup: force-recover ALL in_progress tasks (lock may not be expired yet,
	// but no agent is running after a restart).
	t.recoverAll(true)

	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			// Periodic: only recover tasks with expired locks.
			t.recoverAll(false)
		}
	}
}

func (t *TaskTicker) recoverAll(forceRecover bool) {
	// Step 1: Batch followups with own timeout (before recovery — recovery resets
	// in_progress→pending, which would make followup tasks invisible since followup
	// queries status='in_progress').
	followupCtx, followupCancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.processFollowups(followupCtx)
	followupCancel()

	// Step 2: Batch recovery — single query across all v2 active teams.
	// Separate timeout so followup duration doesn't eat into recovery budget.
	recoverCtx, recoverCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer recoverCancel()

	var recovered []store.RecoveredTaskInfo
	var err error
	if forceRecover {
		recovered, err = t.teams.ForceRecoverAllTasks(recoverCtx)
	} else {
		recovered, err = t.teams.RecoverAllStaleTasks(recoverCtx)
	}
	if err != nil {
		slog.Warn("task_ticker: batch recovery", "force", forceRecover, "error", err)
	}
	if len(recovered) > 0 {
		slog.Info("task_ticker: recovered tasks", "count", len(recovered), "force", forceRecover)
		t.notifyLeaders(recoverCtx, recovered, "recovered (lock expired)",
			"These tasks were reset to pending because the assigned agent stopped responding.\n"+
				"To re-dispatch: use team_tasks(action=\"retry\", task_id=\"<task_id>\") for each task above.\n"+
				"To cancel: use team_tasks(action=\"update\", task_id=\"<task_id>\", status=\"cancelled\").\n"+
				"To view all tasks: use team_tasks(action=\"list\").")
	}

	// Step 3: Batch mark stale — pending tasks older than 2h.
	staleThreshold := time.Now().Add(-defaultStaleThreshold)
	stale, err := t.teams.MarkAllStaleTasks(recoverCtx, staleThreshold)
	if err != nil {
		slog.Warn("task_ticker: batch mark stale", "error", err)
	}
	if len(stale) > 0 {
		slog.Info("task_ticker: marked stale", "count", len(stale))
		t.notifyLeaders(recoverCtx, stale, "marked stale (no progress for 2+ hours)",
			"These tasks have been pending too long without being picked up.\n"+
				"To re-dispatch: use team_tasks(action=\"retry\", task_id=\"<task_id>\").\n"+
				"To cancel: use team_tasks(action=\"update\", task_id=\"<task_id>\", status=\"cancelled\").\n"+
				"To view current board: use team_tasks(action=\"list\").")
		t.broadcastStaleEvents(recoverCtx, stale)
	}

	// Step 4: Mark in_review tasks stale after 4 hours.
	inReviewThreshold := time.Now().Add(-defaultInReviewThreshold)
	staleReview, err := t.teams.MarkInReviewStaleTasks(recoverCtx, inReviewThreshold)
	if err != nil {
		slog.Warn("task_ticker: batch mark in_review stale", "error", err)
	}
	if len(staleReview) > 0 {
		slog.Info("task_ticker: marked in_review stale", "count", len(staleReview))
		t.notifyLeaders(recoverCtx, staleReview, "in review too long (4+ hours) — marked stale",
			"These tasks have been waiting for approval too long.\n"+
				"To approve: use team_tasks(action=\"approve\", task_id=\"<task_id>\").\n"+
				"To reject: use team_tasks(action=\"reject\", task_id=\"<task_id>\", text=\"reason\").\n"+
				"To retry: use team_tasks(action=\"retry\", task_id=\"<task_id>\").")
		t.broadcastStaleEvents(recoverCtx, staleReview)
	}

	// Step 5: Fix orphaned blocked tasks where all blockers are terminal.
	fixed, err := t.teams.FixOrphanedBlockedTasks(recoverCtx)
	if err != nil {
		slog.Warn("task_ticker: fix orphaned blocked tasks", "error", err)
	}
	if len(fixed) > 0 {
		slog.Info("task_ticker: auto-unblocked orphaned tasks", "count", len(fixed))
		t.notifyLeaders(recoverCtx, fixed, "auto-unblocked (all blockers resolved)",
			"These blocked tasks were automatically unblocked because all their dependencies completed.\n"+
				"They are now pending and will be dispatched if assigned.")
	}

	// Step 6: Prune old cooldown entries to prevent memory leak.
	t.pruneCooldowns()
}

// ============================================================
// Leader notifications (batched per scope)
// ============================================================

type taskScope struct {
	TeamID   uuid.UUID
	TenantID uuid.UUID
	Channel  string // from task's origin channel
	ChatID   string
}

// notifyLeaders sends a batched system message per (teamID, channel, chatID) scope to the leader.
func (t *TaskTicker) notifyLeaders(ctx context.Context, tasks []store.RecoveredTaskInfo, action, hint string) {
	if t.msgBus == nil {
		return
	}

	// Group by (team_id, channel, chat_id) → one message per scope.
	byScope := map[taskScope][]store.RecoveredTaskInfo{}
	for _, task := range tasks {
		key := taskScope{TeamID: task.TeamID, TenantID: task.TenantID, Channel: task.Channel, ChatID: task.ChatID}
		byScope[key] = append(byScope[key], task)
	}

	// Cache team+lead lookups (same team may have multiple scopes).
	// Composite key includes TenantID to clarify multi-tenant intent (UUIDs are globally
	// unique but composite key makes the isolation boundary explicit and future-proof).
	type teamCacheKey struct {
		TeamID   uuid.UUID
		TenantID uuid.UUID
	}
	type leadCacheKey struct {
		AgentID  uuid.UUID
		TenantID uuid.UUID
	}
	teamCache := map[teamCacheKey]*store.TeamData{}
	leadCache := map[leadCacheKey]*store.AgentData{}

	for scope, scopeTasks := range byScope {
		// Inject tenant into ctx so store lookups (GetTeam, GetByID, GetTask) apply the
		// correct tenant filter. Without this, PGTeamStore.GetTeam silently returns
		// (nil, nil) when ctx has no tenant — causing a nil-deref panic on team.LeadAgentID.
		scopeCtx := ctx
		if scope.TenantID != uuid.Nil {
			scopeCtx = store.WithTenantID(ctx, scope.TenantID)
		}

		teamKey := teamCacheKey{TeamID: scope.TeamID, TenantID: scope.TenantID}
		team := teamCache[teamKey]
		if team == nil {
			var err error
			team, err = t.teams.GetTeam(scopeCtx, scope.TeamID)
			if err != nil {
				slog.Warn("task_ticker: get team failed", "team_id", scope.TeamID, "tenant_id", scope.TenantID, "error", err)
				continue
			}
			if team == nil {
				// GetTeam can return (nil, nil) when tenant ctx is missing or team is deleted.
				slog.Warn("task_ticker: team not found (nil)", "team_id", scope.TeamID, "tenant_id", scope.TenantID)
				continue
			}
			teamCache[teamKey] = team
		}

		leadKey := leadCacheKey{AgentID: team.LeadAgentID, TenantID: scope.TenantID}
		lead := leadCache[leadKey]
		if lead == nil {
			var err error
			lead, err = t.agents.GetByID(scopeCtx, team.LeadAgentID)
			if err != nil {
				slog.Warn("task_ticker: get lead agent failed", "agent_id", team.LeadAgentID, "tenant_id", scope.TenantID, "error", err)
				continue
			}
			if lead == nil {
				slog.Warn("task_ticker: lead agent not found (nil)", "agent_id", team.LeadAgentID, "tenant_id", scope.TenantID)
				continue
			}
			leadCache[leadKey] = lead
		}

		// Build batched task list with clear actionable hints.
		var lines []string
		for _, task := range scopeTasks {
			lines = append(lines, fmt.Sprintf("  - Task #%d (id: %s): %s",
				task.TaskNumber, task.ID, task.Subject))
		}
		content := fmt.Sprintf("[System] %d task(s) %s:\n%s\n\n%s",
			len(scopeTasks), action, strings.Join(lines, "\n"), hint)

		// Route using task's channel directly (from RETURNING); fallback to dashboard.
		channel := scope.Channel
		chatID := scope.ChatID
		if channel == "" || channel == "system" || channel == "teammate" {
			channel = "dashboard"
			chatID = scope.TeamID.String()
		}

		// Resolve PeerKind from first task's metadata for correct session routing (#266).
		var peerKind string
		var fullTask *store.TeamTaskData
		if task, err := t.teams.GetTask(scopeCtx, scopeTasks[0].ID); err == nil {
			fullTask = task
			if fullTask != nil && fullTask.Metadata != nil {
				if pk, ok := fullTask.Metadata["peer_kind"].(string); ok {
					peerKind = pk
				}
			}
		}

		if !t.msgBus.TryPublishInbound(bus.InboundMessage{
			Channel:  channel,
			SenderID: "ticker:system",
			ChatID:   chatID,
			Metadata: tools.TaskLocalKeyMetadata(fullTask),
			AgentID:  lead.AgentKey,
			UserID:   team.CreatedBy,
			PeerKind: peerKind,
			TenantID: scope.TenantID,
			Content:  content,
		}) {
			slog.Warn("task_ticker: inbound buffer full, notification dropped",
				"team_id", scope.TeamID, "scope_chat", scope.ChatID)
		}
	}
}

// broadcastStaleEvents sends UI broadcast events per team (for dashboard real-time updates).
func (t *TaskTicker) broadcastStaleEvents(ctx context.Context, tasks []store.RecoveredTaskInfo) {
	if t.msgBus == nil {
		return
	}
	// Deduplicate by team_id — one event per team.
	seen := map[uuid.UUID]bool{}
	for _, task := range tasks {
		if seen[task.TeamID] {
			continue
		}
		seen[task.TeamID] = true
		bus.BroadcastForTenant(t.msgBus, protocol.EventTeamTaskStale, task.TenantID, tools.BuildTaskEventPayload(
			task.TeamID.String(), "",
			store.TeamTaskStatusStale,
			"system", "task_ticker",
		))
	}
}

// ============================================================
// Follow-up reminders (batch)
// ============================================================

func (t *TaskTicker) processFollowups(ctx context.Context) {
	tasks, err := t.teams.ListAllFollowupDueTasks(ctx)
	if err != nil {
		slog.Warn("task_ticker: list all followup tasks", "error", err)
		return
	}
	if len(tasks) == 0 {
		return
	}

	// Group by team_id for per-team interval resolution.
	byTeam := map[uuid.UUID][]store.TeamTaskData{}
	for _, task := range tasks {
		byTeam[task.TeamID] = append(byTeam[task.TeamID], task)
	}
	for teamID, teamTasks := range byTeam {
		if len(teamTasks) == 0 {
			continue
		}
		tenantID := teamTasks[0].TenantID
		scopeCtx := ctx
		if tenantID != uuid.Nil {
			scopeCtx = store.WithTenantID(ctx, tenantID)
		}
		team, err := t.teams.GetTeam(scopeCtx, teamID)
		if err != nil {
			slog.Warn("task_ticker: followups get team failed",
				"team_id", teamID, "tenant_id", tenantID, "error", err)
			continue
		}
		if team == nil {
			slog.Warn("task_ticker: followups team not found (nil)",
				"team_id", teamID, "tenant_id", tenantID)
			continue
		}
		interval := followupInterval(*team)
		t.processTeamFollowups(scopeCtx, teamTasks, interval)
	}
}

// processTeamFollowups sends follow-up reminders for a batch of tasks sharing the same team.
func (t *TaskTicker) processTeamFollowups(ctx context.Context, tasks []store.TeamTaskData, interval time.Duration) {
	now := time.Now()

	for i := range tasks {
		task := &tasks[i]

		// Cooldown: don't send more often than followupCooldown.
		t.mu.Lock()
		lastSent, exists := t.lastFollowupSent[task.ID]
		t.mu.Unlock()
		if exists && now.Sub(lastSent) < followupCooldown {
			continue
		}

		if task.FollowupChannel == "" || task.FollowupChatID == "" {
			continue
		}

		// Format reminder message.
		countLabel := fmt.Sprintf("%d", task.FollowupCount+1)
		if task.FollowupMax > 0 {
			countLabel = fmt.Sprintf("%d/%d", task.FollowupCount+1, task.FollowupMax)
		}
		content := fmt.Sprintf("Reminder (%s): %s", countLabel, task.FollowupMessage)

		if !t.msgBus.TryPublishOutbound(followupOutboundMessage(task, content)) {
			slog.Warn("task_ticker: outbound buffer full, skipping followup", "task_id", task.ID)
			continue
		}

		// Compute next followup_at.
		newCount := task.FollowupCount + 1
		var nextAt *time.Time
		if task.FollowupMax == 0 || newCount < task.FollowupMax {
			next := now.Add(interval)
			nextAt = &next
		}
		// nextAt = nil when max reached → stops future reminders.

		if err := t.teams.IncrementFollowupCount(ctx, task.ID, nextAt); err != nil {
			slog.Warn("task_ticker: increment followup count", "task_id", task.ID, "error", err)
		}

		t.mu.Lock()
		t.lastFollowupSent[task.ID] = now
		t.mu.Unlock()

		slog.Info("task_ticker: sent followup reminder",
			"task_id", task.ID,
			"task_number", task.TaskNumber,
			"count", newCount,
			"channel", task.FollowupChannel,
			"team_id", task.TeamID,
		)
	}
}

func followupOutboundMessage(task *store.TeamTaskData, content string) bus.OutboundMessage {
	message := bus.OutboundMessage{
		Channel: task.FollowupChannel,
		ChatID:  task.FollowupChatID,
		Content: content,
	}
	message.Metadata = tools.TaskLocalKeyMetadata(task)
	return message
}

// followupInterval parses the team's followup_interval_minutes setting.
func followupInterval(team store.TeamData) time.Duration {
	if team.Settings != nil {
		var settings map[string]any
		if json.Unmarshal(team.Settings, &settings) == nil {
			if v, ok := settings["followup_interval_minutes"].(float64); ok && v > 0 {
				return time.Duration(int(v)) * time.Minute
			}
		}
	}
	return defaultFollowupInterval
}

func (t *TaskTicker) pruneCooldowns() {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	for id, ts := range t.lastFollowupSent {
		if now.Sub(ts) > 2*followupCooldown {
			delete(t.lastFollowupSent, id)
		}
	}
}
