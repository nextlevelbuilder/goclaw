package bitrix24

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// Compile-time guard: *Channel must satisfy ActivityIndicatorChannel so
// HandleAgentEvent's type assertion routes activity events to Bitrix24.
var _ channels.ActivityIndicatorChannel = (*Channel)(nil)

// Activity indicator tuning. The platform auto-expires the indicator after
// activityDurationSeconds; the channel layer (Manager heartbeat) re-sends before it
// lapses. The call uses a short timeout so a slow REST round-trip cannot pile up.
const (
	activityDurationSeconds = 30
	activityCallTimeout     = 4 * time.Second
)

// activityIndicatorEnabled reports whether the ephemeral activity indicator is on.
// Default is on (nil config value).
func (c *Channel) activityIndicatorEnabled() bool {
	return c.cfg.ActivityIndicator == nil || *c.cfg.ActivityIndicator
}

// OnActivityEvent implements channels.ActivityIndicatorChannel. It shows a native
// Bitrix24 "agent is working" indicator via imbot.v2.Chat.InputAction.notify.
//
// This is COSMETIC and BEST-EFFORT: it deliberately does NOT use
// callWithRateLimitRetry. On QUERY_LIMIT_EXCEEDED (or any error) the notify is dropped
// silently so it never retries into — and never steals leaky-bucket capacity from — the
// real message sends. Rate limits are per-portal/shared-per-IP, so yielding here keeps
// imbot.v2.Chat.Message.send unaffected.
//
// statusCode is a Bitrix action code (IMBOT_AGENT_ACTION_*). Empty → default typing.
//
// MCP-DERIVED: imbot.v2.Chat.InputAction.notify param casing (botId/dialogId/
// statusMessageCode/duration) mirrors imbot.v2.Chat.Message.send; verify live vs the
// portal before release (Hard Rule #13).
func (c *Channel) OnActivityEvent(ctx context.Context, chatID, statusCode string) error {
	if !c.activityIndicatorEnabled() {
		return nil
	}
	botID := c.BotID()
	// Liveness guard — mirror Send (send.go): skip when not fully started.
	if c.Client() == nil || botID <= 0 || strings.TrimSpace(chatID) == "" {
		return nil
	}

	params := map[string]any{
		"botId":    botID,
		"dialogId": chatID,
		"duration": activityDurationSeconds,
	}
	if statusCode != "" {
		params["statusMessageCode"] = statusCode
	}

	cctx, cancel := context.WithTimeout(ctx, activityCallTimeout)
	defer cancel()
	if _, err := c.Client().Call(cctx, "imbot.v2.Chat.InputAction.notify", params); err != nil {
		// Drop-on-limit: cosmetic indicator must never disturb real traffic.
		slog.Debug("bitrix24.activity.notify dropped",
			"chat_id", chatID, "code", statusCode, "err", err)
	}
	return nil
}
