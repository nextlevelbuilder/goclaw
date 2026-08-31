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

	mu                sync.Mutex
	lastFollowupSent  map[uuid.UUID]time.Time // taskID → last followup sent time
	postTurn          tools.PostTurnProcessor
	workflowFinalizer func(context.Context, uuid.UUID)
	workflowRecoverer func(context.Context, store.EscalationClaim)
}

func (t *TaskTicker) SetWorkflowRuntime(postTurn tools.PostTurnProcessor, finalizer func(context.Context, uuid.UUID), recoverer func(context.Context, store.EscalationClaim)) {
	t.postTurn = postTurn
	t.workflowFinalizer = finalizer
	t.workflowRecoverer = recoverer
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

	// Step 2: Workflow recovery has its own budget. Finalizers are launched
	// asynchronously so model latency cannot consume generic task recovery time.
	workflowCtx, workflowCancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.recoverWorkflows(workflowCtx, forceRecover)
	workflowCancel()

	// Step 3: Generic team-task recovery keeps its original independent budget.
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

	// Step 4: Batch mark stale — pending tasks older than 2h.
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

	// Step 5: Mark in_review tasks stale after 4 hours.
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

	// Step 6: Fix orphaned blocked tasks where all blockers are terminal.
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

	// Step 7: Prune old cooldown entries to prevent memory leak.
	t.pruneCooldowns()
}

func (t *TaskTicker) recoverWorkflows(ctx context.Context, force bool) {
	workflowStore, ok := t.teams.(store.TeamWorkflowStore)
	if !ok {
		return
	}
	crossCtx := store.WithCrossTenant(ctx)
	now := time.Now()
	if _, err := workflowStore.RequeueExpiredWorkflowDispatches(crossCtx, now); err != nil {
		slog.Warn("task_ticker: requeue expired workflow dispatches", "error", err)
	}
	if _, err := workflowStore.RecoverWorkflowRuns(crossCtx, force, now); err != nil {
		slog.Warn("task_ticker: recover workflow runs", "force", force, "error", err)
	}
	pending, err := workflowStore.ListPendingAutoExpandWorkflows(crossCtx, now)
	if err != nil {
		slog.Warn("task_ticker: list pending workflow expansion", "error", err)
	} else {
		for i := range pending {
			workflow := &pending[i]
			tenantCtx := store.WithTenantID(ctx, workflow.TenantID)
			// Claim the expansion token FIRST, then validate under the lease. Every
			// failure after the claim consumes one bounded attempt via
			// FailWorkflowExpansion instead of the old silent `continue`, which
			// retried forever. transient=true schedules a capped-backoff retry;
			// transient=false (deterministic plan/roster/coordinator invalidation)
			// moves the workflow to failing so the finalizer emits a user-visible
			// summary. Even a misclassified transient error terminates once
			// MaxWorkflowExpansionAttempts is reached, so expansion is always bounded.
			expansionToken, claimErr := workflowStore.ClaimPendingWorkflowExpansion(tenantCtx, workflow.ID, workflow.CoordinatorAgentID, time.Now().Add(2*time.Minute))
			if claimErr != nil {
				slog.Debug("task_ticker: workflow expansion claim skipped", "workflow_id", workflow.ID, "error", claimErr)
				continue
			}
			failExpansion := func(reason string, transient bool) {
				if _, err := workflowStore.FailWorkflowExpansion(tenantCtx, workflow.ID, workflow.CoordinatorAgentID, expansionToken, reason, transient); err != nil {
					slog.Warn("task_ticker: fail workflow expansion", "workflow_id", workflow.ID, "reason", reason, "error", err)
				}
			}
			if t.postTurn != nil {
				if validationErr := t.postTurn.RevalidateWorkflow(tenantCtx, workflow); validationErr != nil {
					slog.Warn("task_ticker: pending workflow no longer executable", "workflow_id", workflow.ID, "error", validationErr)
					failExpansion("workflow plan is no longer executable: "+validationErr.Error(), false)
					continue
				}
			}
			team, teamErr := t.teams.GetTeam(tenantCtx, workflow.TeamID)
			if teamErr != nil {
				// Ambiguous lookup failure (likely transient DB) — back off and retry.
				slog.Warn("task_ticker: workflow team lookup failed", "workflow_id", workflow.ID, "error", teamErr)
				failExpansion("team lookup failed: "+teamErr.Error(), true)
				continue
			}
			if team == nil || team.LeadAgentID != workflow.CoordinatorAgentID {
				slog.Warn("task_ticker: workflow coordinator no longer valid", "workflow_id", workflow.ID)
				failExpansion("workflow coordinator is no longer the team lead", false)
				continue
			}
			tasksToCreate, buildErr := tools.BuildWorkflowTasksFromStoredPlan(workflow)
			if buildErr != nil {
				slog.Warn("task_ticker: invalid pending workflow plan", "workflow_id", workflow.ID, "error", buildErr)
				failExpansion("stored workflow plan is invalid: "+buildErr.Error(), false)
				continue
			}
			if workflow.AuditTaskID != nil {
				if auditTask, auditErr := t.teams.GetTask(tenantCtx, *workflow.AuditTaskID); auditErr == nil {
					tools.InheritWorkflowTaskContext(tasksToCreate, auditTask)
				}
			}
			members, memberErr := t.teams.ListMembers(tenantCtx, workflow.TeamID)
			if memberErr != nil {
				// Ambiguous lookup failure — back off and retry.
				slog.Warn("task_ticker: workflow roster lookup failed", "workflow_id", workflow.ID, "error", memberErr)
				failExpansion("roster lookup failed: "+memberErr.Error(), true)
				continue
			}
			memberIDs := make(map[uuid.UUID]struct{}, len(members))
			for _, member := range members {
				memberIDs[member.AgentID] = struct{}{}
			}
			validRoster := true
			for _, task := range tasksToCreate {
				if task.OwnerAgentID == nil {
					validRoster = false
					break
				}
				if _, exists := memberIDs[*task.OwnerAgentID]; !exists {
					validRoster = false
					break
				}
			}
			if !validRoster {
				slog.Warn("task_ticker: pending workflow roster changed", "workflow_id", workflow.ID)
				failExpansion("workflow roster changed; a planned owner is no longer a team member", false)
				continue
			}
			if err := workflowStore.ExpandPendingWorkflow(tenantCtx, workflow.ID, workflow.CoordinatorAgentID, expansionToken, tasksToCreate); err != nil {
				slog.Warn("task_ticker: workflow expansion failed", "workflow_id", workflow.ID, "error", err)
				failExpansion("workflow expansion failed: "+err.Error(), true)
			}
		}
	}
	if t.postTurn != nil {
		scopes, scopeErr := workflowStore.ListWorkflowDispatchScopes(crossCtx)
		if scopeErr != nil {
			slog.Warn("task_ticker: list workflow dispatch scopes", "error", scopeErr)
		} else {
			seen := make(map[store.TeamWorkflowDispatchScope]struct{}, len(scopes))
			for _, scope := range scopes {
				key := store.TeamWorkflowDispatchScope{TenantID: scope.TenantID, TeamID: scope.TeamID}
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				t.postTurn.DispatchUnblockedTasks(store.WithTenantID(ctx, scope.TenantID), scope.TeamID)
			}
		}
	}
	// Coordinator-escalation sweep. A blocked workflow work task arms a durable
	// escalation; here we find the due ones and claim each for enqueue. A won
	// claim (Claimed) enqueues a coordinator recovery run; an exhausted claim
	// (Exhausted) has already moved the escalation → dead and the workflow →
	// failing inside ClaimTaskEscalation, so the finalizer picks it up. This is
	// the durable retry that replaces the July-14 fire-and-forget publish: an
	// enqueue failure leaves the escalation pending/enqueuing so the next tick
	// re-claims it, and the per-task CAS guarantees at-most-one live recovery run.
	if t.workflowRecoverer != nil {
		dueTasks, dueErr := workflowStore.ListEscalationDueTasks(crossCtx, now)
		if dueErr != nil {
			slog.Warn("task_ticker: list escalation due tasks", "error", dueErr)
		} else {
			for i := range dueTasks {
				task := &dueTasks[i]
				tenantCtx := store.WithTenantID(ctx, task.TenantID)
				claim, claimErr := workflowStore.ClaimTaskEscalation(tenantCtx, task.ID, task.TeamID, now)
				if claimErr != nil {
					slog.Warn("task_ticker: claim task escalation", "task_id", task.ID, "error", claimErr)
					continue
				}
				if claim.Claimed {
					t.workflowRecoverer(tenantCtx, claim)
				}
			}
		}
	}
	if t.workflowFinalizer != nil {
		ready, readyErr := workflowStore.ListWorkflowsReadyToFinalize(crossCtx, now)
		if readyErr != nil {
			slog.Warn("task_ticker: list workflow finalizers", "error", readyErr)
		} else {
			for _, scope := range ready {
				t.workflowFinalizer(store.WithTenantID(ctx, scope.TenantID), scope.WorkflowID)
			}
		}
	}
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

		// Build metadata: local_key for forum routing + origin sender/role for permission checks.
		// Ticker context has no real sender, so propagate from task metadata (#915 deferred dispatch).
		tickerMeta := tools.TaskLocalKeyMetadata(fullTask)
		if tickerMeta == nil {
			tickerMeta = map[string]string{}
		}
		if fullTask != nil && fullTask.Metadata != nil {
			if s, ok := fullTask.Metadata["origin_sender_id"].(string); ok && s != "" {
				tickerMeta[tools.MetaOriginSenderID] = s
			}
			if r, ok := fullTask.Metadata["origin_role"].(string); ok && r != "" {
				tickerMeta[tools.MetaOriginRole] = r
			}
		}

		if !t.msgBus.TryPublishInbound(bus.InboundMessage{
			Channel:  channel,
			SenderID: "ticker:system",
			ChatID:   chatID,
			Metadata: tickerMeta,
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
	for _, task := range tasks {
		if task.ID == uuid.Nil {
			continue
		}
		tools.PublishTaskEvent(t.teams, t.msgBus, protocol.EventTeamTaskStale, tools.BuildTaskEventPayload(
			task.TeamID.String(), task.ID.String(),
			store.TeamTaskStatusStale,
			"system", "task_ticker",
			tools.WithTaskNumber(task.TaskNumber),
			tools.WithSubject(task.Subject),
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
