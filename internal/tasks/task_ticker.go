package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const (
	defaultRecoveryInterval = 5 * time.Minute
	followupCooldown        = 5 * time.Minute
	defaultFollowupInterval = 30 * time.Minute
)

// isTeamV2 delegates to tools.IsTeamV2 for version checking.
var isTeamV2 = tools.IsTeamV2

// TaskTicker periodically recovers stale tasks and re-dispatches pending work.
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

	teams, err := t.teams.ListTeams(ctx)
	if err != nil {
		slog.Warn("task_ticker: list teams", "error", err)
		return
	}

	for _, team := range teams {
		if team.Status != store.TeamStatusActive {
			continue
		}
		// Skip v1 teams — ticker features (locking, followup, recovery) are v2 only.
		if !isTeamV2(&team) {
			continue
		}
		// Process followups BEFORE recovery: recovery resets in_progress→pending,
		// which would make followup tasks invisible to ListFollowupDueTasks
		// (it only queries status='in_progress').
		t.processFollowups(ctx, team)
		t.recoverTeam(ctx, team, forceRecover)
	}

	// Prune old cooldown entries to prevent memory leak.
	t.pruneCooldowns()
}

func (t *TaskTicker) recoverTeam(ctx context.Context, team store.TeamData, forceRecover bool) {
	// Step 1: Reset in_progress tasks back to pending.
	// On startup (forceRecover=true): reset ALL in_progress — no agent is running after restart.
	// On periodic tick: only reset tasks with expired locks.
	var recovered int
	var err error
	if forceRecover {
		recovered, err = t.teams.ForceRecoverAllTasks(ctx, team.ID)
	} else {
		recovered, err = t.teams.RecoverStaleTasks(ctx, team.ID)
	}
	if err != nil {
		slog.Warn("task_ticker: recover tasks", "team_id", team.ID, "force", forceRecover, "error", err)
		return
	}
	if recovered > 0 {
		slog.Info("task_ticker: recovered tasks", "team_id", team.ID, "count", recovered, "force", forceRecover)
	}

	// Step 2: Mark old pending tasks (>1 day) as stale.
	// Recent pending tasks are handled by post-turn processing, not the ticker.
	staleThreshold := time.Now().Add(-24 * time.Hour)
	staleCount, err := t.teams.MarkStaleTasks(ctx, team.ID, staleThreshold)
	if err != nil {
		slog.Warn("task_ticker: mark stale", "team_id", team.ID, "error", err)
	} else if staleCount > 0 {
		slog.Info("task_ticker: marked stale tasks", "team_id", team.ID, "count", staleCount)
		if t.msgBus != nil {
			t.msgBus.Broadcast(bus.Event{
				Name: protocol.EventTeamTaskStale,
				Payload: protocol.TeamTaskEventPayload{
					TeamID:    team.ID.String(),
					Status:    store.TeamTaskStatusStale,
					Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
					ActorType: "system",
					ActorID:   "task_ticker",
				},
			})
		}
	}
}

// processFollowups sends follow-up reminders for tasks awaiting user reply.
// Called at the end of each recoverAll cycle.
func (t *TaskTicker) processFollowups(ctx context.Context, team store.TeamData) {
	tasks, err := t.teams.ListFollowupDueTasks(ctx, team.ID)
	if err != nil {
		slog.Warn("task_ticker: list followup tasks", "team_id", team.ID, "error", err)
		return
	}

	now := time.Now()
	interval := followupInterval(team)

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
			"team_id", team.ID,
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
