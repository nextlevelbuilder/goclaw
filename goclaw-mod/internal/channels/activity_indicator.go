package channels

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Activity status codes are Bitrix24 imbot InputAction.notify codes. They are kept
// generic in the channel layer; a channel that cannot use them may translate or ignore.
// See imbot.v2.Chat.InputAction.notify (statusMessageCode).
const (
	ActivityStatusThinking   = "IMBOT_AGENT_ACTION_THINKING"
	ActivityStatusSearching  = "IMBOT_AGENT_ACTION_SEARCHING"
	ActivityStatusGenerating = "IMBOT_AGENT_ACTION_GENERATING"
	ActivityStatusAnalyzing  = "IMBOT_AGENT_ACTION_ANALYZING"
	ActivityStatusProcessing = "IMBOT_AGENT_ACTION_PROCESSING"
	ActivityStatusReadingDoc = "IMBOT_AGENT_ACTION_READING_DOCS"
	ActivityStatusConnecting = "IMBOT_AGENT_ACTION_CONNECTING"
)

// Activity indicator timing. The indicator auto-expires after activityDuration on the
// platform side; the heartbeat re-sends the current status while the run is idle (no
// events) so it never disappears mid-run. The throttle caps the call rate per run so
// bursts of fast tool calls cannot spam the platform REST API.
const (
	activityThrottle       = 5 * time.Second  // min interval between notifies per run
	activityTickerInterval = 12 * time.Second // heartbeat check cadence
	activityHeartbeatIdle  = 20 * time.Second // re-send only if idle longer than this
)

// resolveToolActivityStatus maps a tool name to an activity status code.
// Static mapping (no DB); case-insensitive substring match. Mirrors the pattern of
// resolveToolReactionStatus but targets the richer Bitrix activity vocabulary.
func resolveToolActivityStatus(toolName string) string {
	n := strings.ToLower(toolName)
	switch {
	case containsAnySubstr(n, "web", "search", "browser", "fetch"):
		return ActivityStatusSearching
	case containsAnySubstr(n, "read_file", "list_files", "vault", "memory", "docs", "skill"):
		return ActivityStatusReadingDoc
	case containsAnySubstr(n, "image", "tts", "speech", "video", "music", "generate"):
		return ActivityStatusGenerating
	case containsAnySubstr(n, "mcp", "bitrix", "crm"):
		return ActivityStatusConnecting
	case containsAnySubstr(n, "delegate", "subagent", "team"):
		return ActivityStatusProcessing
	default:
		return ActivityStatusProcessing
	}
}

func containsAnySubstr(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// fireActivity sends an activity notify best-effort, subject to the per-run throttle.
// If status is non-empty it becomes the current status; an empty status re-sends the
// current one (used by the heartbeat). The actual REST call runs in a detached goroutine
// so a slow/failed indicator never blocks agent event routing.
func (m *Manager) fireActivity(rc *RunContext, ch ActivityIndicatorChannel, status string) {
	rc.mu.Lock()
	// Update the current status BEFORE the throttle gate on purpose: latest status
	// wins even when this particular call is throttled, so the next heartbeat/notify
	// reflects the most recent phase.
	if status != "" {
		rc.activityStatus = status
	}
	if time.Since(rc.lastActivityAt) < activityThrottle {
		rc.mu.Unlock()
		return
	}
	rc.lastActivityAt = time.Now()
	cur := rc.activityStatus
	chatID := rc.ChatID
	tenant := rc.TenantID
	channelName := rc.ChannelName
	rc.mu.Unlock()

	go func() {
		// Fresh ctx (the event ctx may be cancelled by the time this runs). Tenant scope
		// is attached for parity with other channel event handlers / channels that need
		// it; Bitrix24 derives portal auth from its client instance, not from ctx.
		ctx := context.Background()
		if tenant != uuid.Nil {
			ctx = store.WithTenantID(ctx, tenant)
		}
		if err := ch.OnActivityEvent(ctx, chatID, cur); err != nil {
			// Best-effort: drop-on-limit. Never surface indicator failures.
			slog.Debug("activity indicator failed", "channel", channelName, "status", cur, "error", err)
		}
	}()
}

// startActivityTicker starts the per-run heartbeat that keeps the indicator alive across
// LLM-inference windows (where no agent events fire). Start-once per run. The ticker only
// re-sends when the run has been idle longer than activityHeartbeatIdle, so tool-active
// runs generate no extra calls. Must be paired with stopActivityTicker on terminal events.
func (m *Manager) startActivityTicker(rc *RunContext, ch ActivityIndicatorChannel) {
	rc.mu.Lock()
	if rc.activityStarted {
		rc.mu.Unlock()
		return
	}
	rc.activityStarted = true
	stop := make(chan struct{})
	ticker := time.NewTicker(activityTickerInterval)
	rc.activityStop = stop
	rc.activityTicker = ticker
	rc.mu.Unlock()

	go func() {
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				rc.mu.Lock()
				idle := time.Since(rc.lastActivityAt)
				rc.mu.Unlock()
				if idle >= activityHeartbeatIdle {
					m.fireActivity(rc, ch, "") // re-send current status
				}
			}
		}
	}()
}

// stopActivityTicker stops the heartbeat goroutine and ticker. Idempotent — safe to call
// on any terminal event even if the ticker was never started.
func (m *Manager) stopActivityTicker(rc *RunContext) {
	rc.mu.Lock()
	if rc.activityStop != nil {
		close(rc.activityStop)
		rc.activityStop = nil
	}
	if rc.activityTicker != nil {
		rc.activityTicker.Stop()
		rc.activityTicker = nil
	}
	rc.mu.Unlock()
}
