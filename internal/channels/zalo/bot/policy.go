package bot

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

func (c *Channel) checkDMPolicy(ctx context.Context, senderID, chatID string) bool {
	result := c.CheckDMPolicy(ctx, senderID, c.dmPolicy)
	switch result {
	case channels.PolicyAllow:
		return true
	case channels.PolicyNeedsPairing:
		c.sendPairingReply(ctx, senderID, chatID)
		return false
	default:
		slog.Debug("zalo message rejected by policy", "sender_id", senderID, "policy", c.dmPolicy)
		return false
	}
}

func (c *Channel) sendPairingReply(ctx context.Context, senderID, chatID string) {
	ps := c.PairingService()
	if ps == nil {
		return
	}

	if !c.CanSendPairingNotif(senderID, pairingDebounce) {
		return
	}

	code, err := ps.RequestPairing(ctx, senderID, c.Name(), chatID, "default", nil)
	if err != nil {
		slog.Debug("zalo pairing request failed", "sender_id", senderID, "error", err)
		return
	}

	replyText := fmt.Sprintf(
		"GoClaw: access not configured.\n\nYour Zalo user id: %s\n\nPairing code: %s\n\nAsk the bot owner to approve with:\n  goclaw pairing approve %s",
		senderID, code, code,
	)

	if err := c.sendMessage(chatID, replyText); err != nil {
		slog.Warn("failed to send zalo pairing reply", "error", err)
	} else {
		c.MarkPairingNotifSent(senderID)
		slog.Info("zalo pairing reply sent", "sender_id", senderID, "code", code)
	}
}
