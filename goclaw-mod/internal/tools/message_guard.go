package tools

import (
	"context"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// MessageTargetEnforced reports whether the message tool must enforce
// (channel, target) == ctx (channel, target) for this session kind.
//
// FREE kinds (cron, heartbeat, subagent, team) compose targets per job spec
// and are allowed to address arbitrary chats. Everything else (DM, group,
// default, unknown, empty) enforces the bound origin — fail-closed.
func MessageTargetEnforced(sessionKey string) bool {
	if sessionKey == "" {
		return true
	}
	if sessions.IsCronSession(sessionKey) {
		return false
	}
	if sessions.IsHeartbeatSession(sessionKey) {
		return false
	}
	if sessions.IsSubagentSession(sessionKey) {
		return false
	}
	if sessions.IsTeamSession(sessionKey) {
		return false
	}
	return true
}

// postCrossTargetNotice posts a short notice back to the origin chat after
// an explicit, SUCCESSFUL cross-target forward. Call inline post-send — not
// via defer — so that failed forwards don't announce fake deliveries.
//
// Prefers msgBus (async, group-aware). Falls back to t.sender so deployments
// that wire only a direct sender still get the audit breadcrumb. Last resort:
// slog.Warn so the skip is still traceable.
func (t *MessageTool) postCrossTargetNotice(ctx context.Context, target, reason string) {
	origCh := ToolChannelFromCtx(ctx)
	origChat := ToolChatIDFromCtx(ctx)
	if origCh == "" || origChat == "" {
		return
	}
	locale := store.LocaleFromContext(ctx)
	text := i18n.T(locale, i18n.MessageCrossTargetForwarded, target, reason)

	if t.msgBus != nil {
		msg := bus.OutboundMessage{Channel: origCh, ChatID: origChat, Content: text}
		if isGroupContext(ctx) {
			msg.Metadata = map[string]string{"group_id": origChat}
		}
		t.msgBus.PublishOutbound(msg)
		return
	}
	if t.sender != nil {
		if err := t.sender(ctx, origCh, origChat, text); err == nil {
			return
		} else {
			slog.Warn("message.cross_target_notice_send_failed",
				"channel", origCh, "chat", origChat, "err", err)
			return
		}
	}
	slog.Warn("message.cross_target_notice_skipped",
		"reason", "no_bus_or_sender", "channel", origCh, "chat", origChat)
}
