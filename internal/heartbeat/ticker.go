// Package heartbeat implements a background ticker that runs periodic
// agent heartbeat check-ins. Each enabled agent gets a scheduled turn
// where it reads HEARTBEAT.md and reports if anything needs attention.
package heartbeat

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	minIntervalSec = 300 // 5 minutes minimum
	pollInterval   = 30 * time.Second
	maxSummaryLen  = 500 // truncate summary in logs
)

// TickerConfig holds dependencies for the heartbeat ticker.
type TickerConfig struct {
	Store    store.HeartbeatStore
	Agents   store.AgentStore
	Sessions store.SessionStore // optional: for cleaning up isolated heartbeat sessions
	MsgBus   *bus.MessageBus
	Sched    *scheduler.Scheduler
	RunAgent func(ctx context.Context, req agent.RunRequest) <-chan scheduler.RunOutcome
}

// Ticker polls for due heartbeats and runs them through the agent loop.
type Ticker struct {
	store    store.HeartbeatStore
	agents   store.AgentStore
	sessions store.SessionStore
	msgBus   *bus.MessageBus
	sched    *scheduler.Scheduler
	runAgent func(ctx context.Context, req agent.RunRequest) <-chan scheduler.RunOutcome
	onEvent  func(store.HeartbeatEvent)

	wakeCh chan uuid.UUID
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewTicker creates a new heartbeat ticker.
func NewTicker(cfg TickerConfig) *Ticker {
	return &Ticker{
		store:    cfg.Store,
		agents:   cfg.Agents,
		sessions: cfg.Sessions,
		msgBus:   cfg.MsgBus,
		sched:    cfg.Sched,
		runAgent: cfg.RunAgent,
		wakeCh:   make(chan uuid.UUID, 16),
		stopCh:   make(chan struct{}),
	}
}

// Start begins the background poll loop.
func (t *Ticker) Start() {
	t.wg.Add(1)
	go t.loop()
	slog.Info("heartbeat ticker started")
}

// Stop signals the poll loop to exit and waits for completion.
func (t *Ticker) Stop() {
	close(t.stopCh)
	t.wg.Wait()
	slog.Info("heartbeat ticker stopped")
}

// SetOnEvent sets the event callback (called for lifecycle events like running/completed/error).
func (t *Ticker) SetOnEvent(fn func(store.HeartbeatEvent)) {
	t.onEvent = fn
}

func (t *Ticker) emitEvent(event store.HeartbeatEvent) {
	if t.onEvent != nil {
		t.onEvent(event)
	}
}

// Wake triggers an immediate heartbeat run for a specific agent (wakeMode).
func (t *Ticker) Wake(agentID uuid.UUID) {
	select {
	case t.wakeCh <- agentID:
	default: // channel full, skip
	}
}

func (t *Ticker) loop() {
	defer t.wg.Done()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			t.runDueHeartbeats()
		case agentID := <-t.wakeCh:
			go t.runOneByAgentID(agentID)
		}
	}
}

func (t *Ticker) runDueHeartbeats() {
	ctx := context.Background()
	now := time.Now()
	due, err := t.store.ListDue(ctx, now)
	if err != nil {
		slog.Warn("heartbeat.list_due_failed", "error", err)
		return
	}
	if len(due) == 0 {
		return
	}

	var wg sync.WaitGroup
	for _, hb := range due {
		wg.Add(1)
		go func(hb store.AgentHeartbeat) {
			defer wg.Done()
			t.runOne(ctx, hb)
		}(hb)
	}
	wg.Wait()
}

func (t *Ticker) runOneByAgentID(agentID uuid.UUID) {
	ctx := context.Background()
	hb, err := t.store.Get(ctx, agentID)
	if err != nil {
		slog.Warn("heartbeat.wake_get_failed", "agent_id", agentID, "error", err)
		return
	}
	t.runOne(ctx, *hb)
}

func (t *Ticker) runOne(ctx context.Context, hb store.AgentHeartbeat) {
	start := time.Now()
	agentIDStr := hb.AgentID.String()

	// Resolve agent to get tenant scope + display key.
	// Unscoped lookup since ticker is a global scheduler.
	agentKey := agentIDStr
	ag, agErr := t.agents.GetByIDUnscoped(context.Background(), hb.AgentID)
	if agErr != nil {
		slog.Warn("heartbeat.agent_not_found", "agent_id", agentIDStr, "error", agErr)
		return
	}
	agentKey = ag.AgentKey

	// Inject agent's tenant into context so all store operations
	// (context files, sessions, etc.) are tenant-scoped.
	if ag.TenantID != uuid.Nil {
		ctx = store.WithTenantID(ctx, ag.TenantID)
	} else {
		ctx = store.WithTenantID(ctx, store.MasterTenantID)
	}

	// [1] Active hours filter.
	if !isWithinActiveHours(hb) {
		t.logSkipped(ctx, hb, "active_hours", agentKey)
		t.advanceNextRun(ctx, hb)
		return
	}

	// [2] Queue-aware: skip if agent is busy (active runs in scheduler).
	if t.sched != nil && t.sched.HasActiveSessionsForAgent(agentIDStr) {
		t.logSkipped(ctx, hb, "queue_busy", agentKey)
		// Don't advance next_run_at — retry on next poll.
		return
	}

	// [3] Read HEARTBEAT.md from agent context files.
	checklistContent := t.readChecklist(ctx, hb.AgentID)
	if checklistContent == "" {
		t.logSkipped(ctx, hb, "empty_checklist", agentKey)
		t.advanceNextRun(ctx, hb)
		return
	}

	// [4] Emit running event.
	t.emitEvent(store.HeartbeatEvent{
		Action: "running", AgentID: agentIDStr, AgentKey: agentKey,
	})

	// [4] Build prompt.
	prompt := "Execute your heartbeat checklist now."
	if hb.Prompt != nil && *hb.Prompt != "" {
		prompt = *hb.Prompt
	}

	extraSystem := fmt.Sprintf(
		"[Heartbeat Check-in]\nThis is a periodic heartbeat run for agent %s.\n"+
			"Your checklist:\n---\n%s\n---\n"+
			"RULES:\n"+
			"- EXECUTE the tasks in the checklist using your tools. Do NOT just read or quote the checklist back.\n"+
			"- Your response will be delivered to the configured channel as-is.\n"+
			"- HEARTBEAT_OK suppression: If your response contains the token HEARTBEAT_OK anywhere, "+
			"the ENTIRE response is suppressed and NOT delivered to the channel.\n"+
			"- Use HEARTBEAT_OK ONLY when there is nothing to deliver (e.g. monitoring checks all passed, no news).\n"+
			"- Do NOT include HEARTBEAT_OK if the checklist asks you to send content (jokes, greetings, reports, etc.).",
		agentKey, checklistContent,
	)

	sessionKey := sessions.BuildHeartbeatSessionKey(agentIDStr, hb.IsolatedSession)

	channel := "heartbeat"
	if hb.Channel != nil && *hb.Channel != "" {
		channel = *hb.Channel
	}
	chatID := ""
	if hb.ChatID != nil {
		chatID = *hb.ChatID
	}

	// [5] Run through agent loop via scheduler.
	var lastErr error
	var result *agent.RunResult
	maxAttempts := hb.MaxRetries + 1

	// Model override: use heartbeat-specific model if configured.
	var modelOverride string
	if hb.Model != nil && *hb.Model != "" {
		modelOverride = *hb.Model
	}

	for attempt := range maxAttempts {
		outCh := t.runAgent(ctx, agent.RunRequest{
			SessionKey:        sessionKey,
			Message:           prompt,
			Channel:           channel,
			ChatID:            chatID,
			RunID:             fmt.Sprintf("heartbeat:%s", agentIDStr),
			Stream:            false,
			ExtraSystemPrompt: extraSystem,
			ModelOverride:     modelOverride,
			LightContext:      hb.LightContext,
			TraceName:         fmt.Sprintf("Heartbeat [%s]", agentKey),
			TraceTags:         []string{"heartbeat"},
		})

		outcome := <-outCh
		if outcome.Err == nil {
			result = outcome.Result
			lastErr = nil
			break
		}
		lastErr = outcome.Err
		if attempt < maxAttempts-1 {
			// Exponential backoff between retries.
			time.Sleep(time.Duration(1<<uint(attempt)) * time.Second)
		}
	}

	duration := time.Since(start)
	durationMS := int(duration.Milliseconds())

	// [6] Process result.
	if lastErr != nil {
		t.finishRun(ctx, hb, sessionKey, agentKey, "error", lastErr.Error(), "", durationMS, 0, 0)
		return
	}

	// [7] Smart suppression.
	deliver, cleaned := processResponse(result.Content, hb.AckMaxChars)

	var inputTokens, outputTokens int
	if result.Usage != nil {
		inputTokens = result.Usage.PromptTokens
		outputTokens = result.Usage.CompletionTokens
	}

	if !deliver {
		t.finishRun(ctx, hb, sessionKey, agentKey, "suppressed", "", truncate(result.Content, maxSummaryLen), durationMS, inputTokens, outputTokens)
		return
	}

	// [8] Deliver to channel.
	if hb.Channel != nil && *hb.Channel != "" && hb.ChatID != nil && *hb.ChatID != "" {
		t.msgBus.PublishOutbound(bus.OutboundMessage{
			Channel: *hb.Channel,
			ChatID:  *hb.ChatID,
			Content: cleaned,
		})
	}

	t.finishRun(ctx, hb, sessionKey, agentKey, "ok", "", truncate(cleaned, maxSummaryLen), durationMS, inputTokens, outputTokens)
}

func (t *Ticker) finishRun(ctx context.Context, hb store.AgentHeartbeat, sessionKey, agentKey, status, errMsg, summary string, durationMS, inputTokens, outputTokens int) {
	agentIDStr := hb.AgentID.String()
	now := time.Now()

	// Insert log.
	logEntry := &store.HeartbeatRunLog{
		HeartbeatID:  hb.ID,
		AgentID:      hb.AgentID,
		Status:       status,
		DurationMS:   &durationMS,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		RanAt:        now,
	}
	if summary != "" {
		logEntry.Summary = &summary
	}
	if errMsg != "" {
		logEntry.Error = &errMsg
	}
	if err := t.store.InsertLog(ctx, logEntry); err != nil {
		slog.Warn("heartbeat.insert_log_failed", "agent_id", agentIDStr, "error", err)
	}

	// Update state.
	newState := store.HeartbeatState{
		LastRunAt:  &now,
		LastStatus: status,
		RunCount:   hb.RunCount + 1,
	}
	if status == "suppressed" {
		newState.SuppressCount = hb.SuppressCount + 1
	} else {
		newState.SuppressCount = hb.SuppressCount
	}
	if errMsg != "" {
		newState.LastError = errMsg
	}
	nextRun := now.Add(time.Duration(hb.IntervalSec) * time.Second)
	newState.NextRunAt = &nextRun

	if err := t.store.UpdateState(ctx, hb.ID, newState); err != nil {
		slog.Warn("heartbeat.update_state_failed", "agent_id", agentIDStr, "error", err)
	}

	// Cleanup isolated session — data is already in heartbeat_run_logs.
	if hb.IsolatedSession && t.sessions != nil && sessionKey != "" {
		if err := t.sessions.Delete(ctx, sessionKey); err != nil {
			slog.Debug("heartbeat.session_cleanup_failed", "session_key", sessionKey, "error", err)
		}
	}

	// Emit event.
	t.emitEvent(store.HeartbeatEvent{
		Action:   status,
		AgentID:  agentIDStr,
		AgentKey: agentKey,
		Status:   status,
		Error:    errMsg,
	})
}

func (t *Ticker) logSkipped(ctx context.Context, hb store.AgentHeartbeat, reason, agentKey string) {
	now := time.Now()
	logEntry := &store.HeartbeatRunLog{
		HeartbeatID: hb.ID,
		AgentID:     hb.AgentID,
		Status:      "skipped",
		SkipReason:  &reason,
		RanAt:       now,
	}
	if err := t.store.InsertLog(ctx, logEntry); err != nil {
		slog.Warn("heartbeat.insert_skip_log_failed", "agent_id", hb.AgentID, "error", err)
	}

	t.emitEvent(store.HeartbeatEvent{
		Action:   "skipped",
		AgentID:  hb.AgentID.String(),
		AgentKey: agentKey,
		Reason:   reason,
	})
}

func (t *Ticker) advanceNextRun(ctx context.Context, hb store.AgentHeartbeat) {
	nextRun := time.Now().Add(time.Duration(hb.IntervalSec) * time.Second)
	state := store.HeartbeatState{
		NextRunAt:     &nextRun,
		LastStatus:    deref(hb.LastStatus),
		RunCount:      hb.RunCount,
		SuppressCount: hb.SuppressCount,
	}
	if hb.LastRunAt != nil {
		state.LastRunAt = hb.LastRunAt
	}
	if err := t.store.UpdateState(ctx, hb.ID, state); err != nil {
		slog.Warn("heartbeat.advance_next_run_failed", "agent_id", hb.AgentID, "error", err)
	}
}

func (t *Ticker) readChecklist(ctx context.Context, agentID uuid.UUID) string {
	if t.agents == nil {
		return ""
	}
	files, err := t.agents.GetAgentContextFiles(ctx, agentID)
	if err != nil {
		return ""
	}
	for _, f := range files {
		if f.FileName == "HEARTBEAT.md" && f.Content != "" {
			return f.Content
		}
	}
	return ""
}

// processResponse implements smart suppression.
// If response contains HEARTBEAT_OK, agent confirms everything is fine — always suppress.
// Only deliver when HEARTBEAT_OK is absent (agent found something needing attention).
func processResponse(response string, _ int) (deliver bool, cleaned string) {
	const ackToken = "HEARTBEAT_OK"
	if strings.Contains(response, ackToken) {
		return false, "" // agent says OK → suppress regardless of extra content
	}
	return true, response // no OK token → something needs attention, deliver
}

// isWithinActiveHours checks if current time falls within the configured active hours.
func isWithinActiveHours(hb store.AgentHeartbeat) bool {
	if hb.ActiveHoursStart == nil || hb.ActiveHoursEnd == nil {
		return true // no active hours = 24/7
	}
	startStr := *hb.ActiveHoursStart
	endStr := *hb.ActiveHoursEnd
	if startStr == "" || endStr == "" {
		return true
	}

	loc := time.UTC
	if hb.Timezone != nil && *hb.Timezone != "" {
		if parsed, err := time.LoadLocation(*hb.Timezone); err == nil {
			loc = parsed
		}
	}

	now := time.Now().In(loc)
	startMin := parseHHMM(startStr)
	endMin := parseHHMM(endStr)
	nowMin := now.Hour()*60 + now.Minute()

	if startMin <= endMin {
		return nowMin >= startMin && nowMin < endMin
	}
	// Wraps midnight (e.g. 22:00 - 06:00).
	return nowMin >= startMin || nowMin < endMin
}

func parseHHMM(s string) int {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0
	}
	h, m := 0, 0
	fmt.Sscanf(parts[0], "%d", &h)
	fmt.Sscanf(parts[1], "%d", &m)
	return h*60 + m
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
