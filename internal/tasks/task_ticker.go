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
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const (
	defaultRecoveryInterval = 5 * time.Minute
	defaultStaleThreshold   = 2 * time.Hour
	followupCooldown        = 5 * time.Minute
	defaultFollowupInterval = 30 * time.Minute
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
	ctx := context.Background()

	// Step 1: Batch followups (before recovery — recovery resets in_progress→pending,
	// which would make followup tasks invisible since followup queries status='in_progress').
	t.processFollowups(ctx)

	// Step 2: Batch recovery — single query across all v2 active teams.
	var recovered []store.RecoveredTaskInfo
	var err error
	if forceRecover {
		recovered, err = t.teams.ForceRecoverAllTasks(ctx)
	} else {
		recovered, err = t.teams.RecoverAllStaleTasks(ctx)
	}
	if err != nil {
		slog.Warn("task_ticker: batch recovery", "force", forceRecover, "error", err)
	}
	if len(recovered) > 0 {
		slog.Info("task_ticker: recovered tasks", "count", len(recovered), "force", forceRecover)
		t.notifyLeaders(ctx, recovered, "recovered (lock expired)",
			"These tasks were reset to pending because the assigned agent stopped responding.\n"+
				"To re-dispatch: use team_tasks(action=\"retry\", task_id=\"<task_id>\") for each task above.\n"+
				"To cancel: use team_tasks(action=\"update\", task_id=\"<task_id>\", status=\"cancelled\").\n"+
				"To view all tasks: use team_tasks(action=\"list\").")
	}

	// Step 3: Batch mark stale — pending tasks older than 2h.
	staleThreshold := time.Now().Add(-defaultStaleThreshold)
	stale, err := t.teams.MarkAllStaleTasks(ctx, staleThreshold)
	if err != nil {
		slog.Warn("task_ticker: batch mark stale", "error", err)
	}
	if len(stale) > 0 {
		slog.Info("task_ticker: marked stale", "count", len(stale))
		t.notifyLeaders(ctx, stale, "marked stale (no progress for 2+ hours)",
			"These tasks have been pending too long without being picked up.\n"+
				"To re-dispatch: use team_tasks(action=\"retry\", task_id=\"<task_id>\").\n"+
				"To cancel: use team_tasks(action=\"update\", task_id=\"<task_id>\", status=\"cancelled\").\n"+
				"To view current board: use team_tasks(action=\"list\").")
		t.broadcastStaleEvents(ctx, stale)
	}

	// Step 4: Prune old cooldown entries to prevent memory leak.
	t.pruneCooldowns()
}

// ============================================================
// Leader notifications (batched per scope)
// ============================================================

type taskScope struct {
	TeamID  uuid.UUID
	Channel string // from task's origin channel
	ChatID  string
}

// notifyLeaders sends a batched system message per (teamID, channel, chatID) scope to the leader.
func (t *TaskTicker) notifyLeaders(ctx context.Context, tasks []store.RecoveredTaskInfo, action, hint string) {
	if t.msgBus == nil {
		return
	}

	// Group by (team_id, channel, chat_id) → one message per scope.
	byScope := map[taskScope][]store.RecoveredTaskInfo{}
	for _, task := range tasks {
		key := taskScope{TeamID: task.TeamID, Channel: task.Channel, ChatID: task.ChatID}
		byScope[key] = append(byScope[key], task)
	}

	// Cache team+lead lookups (same team may have multiple scopes).
	teamCache := map[uuid.UUID]*store.TeamData{}
	leadCache := map[uuid.UUID]*store.AgentData{}

	for scope, scopeTasks := range byScope {
		team := teamCache[scope.TeamID]
		if team == nil {
			var err error
			team, err = t.teams.GetTeam(ctx, scope.TeamID)
			if err != nil {
				continue
			}
			teamCache[scope.TeamID] = team
		}
		lead := leadCache[team.LeadAgentID]
		if lead == nil {
			var err error
			lead, err = t.agents.GetByID(ctx, team.LeadAgentID)
			if err != nil {
				continue
			}
			leadCache[team.LeadAgentID] = lead
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
		if channel == "" || channel == "system" || channel == "delegate" {
			channel = "dashboard"
			chatID = scope.TeamID.String()
		}

		if !t.msgBus.TryPublishInbound(bus.InboundMessage{
			Channel:  channel,
			SenderID: "ticker:system",
			ChatID:   chatID,
			AgentID:  lead.AgentKey,
			UserID:   team.CreatedBy,
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
		t.msgBus.Broadcast(bus.Event{
			Name: protocol.EventTeamTaskStale,
			Payload: protocol.TeamTaskEventPayload{
				TeamID:    task.TeamID.String(),
				Status:    store.TeamTaskStatusStale,
				Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
				ActorType: "system",
				ActorID:   "task_ticker",
			},
		})
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
		team, err := t.teams.GetTeam(ctx, teamID)
		if err != nil {
			continue
		}
		interval := followupInterval(*team)
		t.processTeamFollowups(ctx, teamTasks, interval)
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

		if !t.msgBus.TryPublishOutbound(bus.OutboundMessage{
			Channel: task.FollowupChannel,
			ChatID:  task.FollowupChatID,
			Content: content,
		}) {
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
