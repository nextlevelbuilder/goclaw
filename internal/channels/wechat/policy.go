package wechat

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

const pairingDebounceTime = 60 * time.Second

// checkDMPolicy evaluates the DM policy for a sender.
func (c *Channel) checkDMPolicy(ctx context.Context, senderID, chatID string) bool {
	dmPolicy := c.cfg.DMPolicy
	if dmPolicy == "" {
		dmPolicy = "pairing"
	}
	result := c.CheckDMPolicy(ctx, senderID, dmPolicy)
	switch result {
	case channels.PolicyAllow:
		return true
	case channels.PolicyNeedsPairing:
		c.sendPairingReply(ctx, senderID, chatID)
		return false
	default:
		slog.Debug("wechat DM rejected by policy", "sender_id", senderID, "policy", dmPolicy)
		return false
	}
}

// sendPairingReply sends a pairing code to the user via WeChat.
func (c *Channel) sendPairingReply(ctx context.Context, senderID, chatID string) {
	ps := c.PairingService()
	if ps == nil {
		slog.Warn("wechat pairing: no pairing service configured")
		return
	}

	if !c.CanSendPairingNotif(senderID, pairingDebounceTime) {
		slog.Info("wechat pairing: debounced", "sender_id", senderID)
		return
	}

	code, err := ps.RequestPairing(ctx, senderID, c.Name(), chatID, "default", nil)
	if err != nil {
		slog.Warn("wechat pairing request failed", "sender_id", senderID, "channel", c.Name(), "error", err)
		return
	}

	replyText := fmt.Sprintf(
		"GoClaw: access not configured.\n\nYour WeChat ID: %s\n\nPairing code: %s\n\nAsk the account owner to approve with:\n  goclaw pairing approve %s",
		senderID, code, code,
	)

	contextToken := c.tokens.Get(c.Name(), chatID)
	
	if _, sendErr := sendTextMessage(ctx, c.api, chatID, replyText, contextToken); sendErr != nil {
		slog.Warn("failed to send wechat pairing reply", "error", sendErr)
	} else {
		c.MarkPairingNotifSent(senderID)
		slog.Info("wechat pairing reply sent", "sender_id", senderID, "code", code)
	}
}
