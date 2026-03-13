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
)

const (
	defaultRecoveryInterval = 5 * time.Minute
	dispatchCooldown        = 10 * time.Minute
	leadNotifyCooldown      = 30 * time.Minute
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
	lastDispatched   map[uuid.UUID]time.Time // taskID → last dispatch time
	lastLeadNotified map[uuid.UUID]time.Time // teamID → last lead notify time
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
		lastDispatched:   make(map[uuid.UUID]time.Time),
		lastLeadNotified: make(map[uuid.UUID]time.Time),
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

	// Step 2: List all recoverable tasks (pending + stale in_progress with expired locks).
	tasks, err := t.teams.ListRecoverableTasks(ctx, team.ID)
	if err != nil {
		slog.Warn("task_ticker: list recoverable", "team_id", team.ID, "error", err)
		return
	}
	if len(tasks) == 0 {
		return
	}

	now := time.Now()
	var unassigned []store.TeamTaskData

	for i := range tasks {
		task := &tasks[i]
		if task.OwnerAgentID != nil {
			// Pending task with an assigned owner — re-dispatch it.
			t.mu.Lock()
			last, exists := t.lastDispatched[task.ID]
			t.mu.Unlock()
			if exists && now.Sub(last) < dispatchCooldown {
				continue
			}

			t.dispatchTask(ctx, task, team.ID)

			t.mu.Lock()
			t.lastDispatched[task.ID] = now
			t.mu.Unlock()
		} else {
			unassigned = append(unassigned, *task)
		}
	}

	// Step 3: Unassigned tasks — only notify lead if there are idle agents available.
	if len(unassigned) == 0 {
		return
	}

	idleMembers, err := t.teams.ListIdleMembers(ctx, team.ID)
	if err != nil {
		slog.Warn("task_ticker: list idle members", "team_id", team.ID, "error", err)
		return
	}
	if len(idleMembers) == 0 {
		// All agents busy — skip, will retry next tick.
		return
	}

	t.mu.Lock()
	last, exists := t.lastLeadNotified[team.ID]
	t.mu.Unlock()
	if !exists || now.Sub(last) >= leadNotifyCooldown {
		t.notifyLead(ctx, team, unassigned, idleMembers)
		t.mu.Lock()
		t.lastLeadNotified[team.ID] = now
		t.mu.Unlock()
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

func (t *TaskTicker) dispatchTask(ctx context.Context, task *store.TeamTaskData, teamID uuid.UUID) {
	ag, err := t.agents.GetByID(ctx, *task.OwnerAgentID)
	if err != nil {
		slog.Warn("task_ticker: resolve agent", "agent_id", task.OwnerAgentID, "error", err)
		return
	}

	content := fmt.Sprintf("[Assigned task #%d]: %s", task.TaskNumber, task.Subject)
	if task.Description != "" {
		content += "\n\n" + task.Description
	}

	if !t.msgBus.TryPublishInbound(bus.InboundMessage{
		Channel:  "system",
		SenderID: "teammate:dashboard",
		ChatID:   teamID.String(),
		Content:  content,
		UserID:   task.UserID,
		AgentID:  ag.AgentKey,
		Metadata: map[string]string{
			"origin_channel":   "dashboard",
			"origin_peer_kind": "direct",
			"from_agent":       "dashboard",
			"to_agent":         ag.AgentKey,
			"team_task_id":     task.ID.String(),
			"team_id":          teamID.String(),
		},
	}) {
		slog.Warn("task_ticker: inbound buffer full, skipping dispatch", "task_id", task.ID)
		return
	}
	slog.Info("task_ticker: re-dispatched task",
		"task_id", task.ID,
		"task_number", task.TaskNumber,
		"agent_key", ag.AgentKey,
		"team_id", teamID,
	)
}

func (t *TaskTicker) notifyLead(ctx context.Context, team store.TeamData, tasks []store.TeamTaskData, idleMembers []store.TeamMemberData) {
	ag, err := t.agents.GetByID(ctx, team.LeadAgentID)
	if err != nil {
		slog.Warn("task_ticker: resolve lead agent", "agent_id", team.LeadAgentID, "error", err)
		return
	}

	var content strings.Builder
	content.WriteString(fmt.Sprintf("[System] %d unassigned task(s) need attention. %d agent(s) available:", len(tasks), len(idleMembers)))
	content.WriteString("\n\nAvailable agents:")
	for _, m := range idleMembers {
		name := m.DisplayName
		if name == "" {
			name = m.AgentKey
		}
		content.WriteString(fmt.Sprintf("\n- %s (%s)", name, m.AgentKey))
	}
	content.WriteString("\n\nUnassigned tasks:")
	for _, task := range tasks {
		content.WriteString(fmt.Sprintf("\n- #%d %s (created %s ago)", task.TaskNumber, task.Subject, time.Since(task.CreatedAt).Truncate(time.Minute)))
	}
	content.WriteString("\n\nPlease review and assign these tasks to the appropriate available agents using the team_tasks tool.")

	if !t.msgBus.TryPublishInbound(bus.InboundMessage{
		Channel:  "system",
		SenderID: "teammate:system",
		ChatID:   team.ID.String(),
		Content:  content.String(),
		UserID:   team.CreatedBy,
		AgentID:  ag.AgentKey,
		Metadata: map[string]string{
			"origin_channel":   "system",
			"origin_peer_kind": "direct",
			"from_agent":       "system",
			"to_agent":         ag.AgentKey,
			"team_id":          team.ID.String(),
		},
	}) {
		slog.Warn("task_ticker: inbound buffer full, skipping lead notify", "team_id", team.ID)
		return
	}
	slog.Info("task_ticker: notified lead about unassigned tasks",
		"team_id", team.ID,
		"lead_agent_key", ag.AgentKey,
		"count", len(tasks),
	)
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
	for id, ts := range t.lastDispatched {
		if now.Sub(ts) > 2*dispatchCooldown {
			delete(t.lastDispatched, id)
		}
	}
	for id, ts := range t.lastLeadNotified {
		if now.Sub(ts) > 2*leadNotifyCooldown {
			delete(t.lastLeadNotified, id)
		}
	}
	for id, ts := range t.lastFollowupSent {
		if now.Sub(ts) > 2*followupCooldown {
			delete(t.lastFollowupSent, id)
		}
	}
}
