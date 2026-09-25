package oa

import (
	"context"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/systemmessages"
)

const pairingDebounce = 60 * time.Second

func (c *Channel) dmPolicy() string {
	if c.cfg.DMPolicy == "" {
		return "pairing"
	}
	return c.cfg.DMPolicy
}

func (c *Channel) authorizeInbound(ctx context.Context, senderID, chatID string) bool {
	result := c.CheckDMPolicy(ctx, senderID, c.dmPolicy())
	switch result {
	case channels.PolicyAllow:
		return true
	case channels.PolicyNeedsPairing:
		c.sendPairingReply(ctx, senderID, chatID)
		return false
	default:
		slog.Debug("zalo_oa message rejected by policy", "sender_id", senderID, "policy", c.dmPolicy())
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
		slog.Debug("zalo_oa pairing request failed", "sender_id", senderID, "error", err)
		return
	}
	replyText := c.SystemMessage("", systemmessages.KeyPairingAccountRequired, systemmessages.Vars{
		"platform":  "Zalo OA",
		"sender_id": senderID,
		"code":      code,
	})
	if _, err := c.SendText(ctx, chatID, replyText, ""); err != nil {
		slog.Warn("failed to send zalo_oa pairing reply", "error", err)
		return
	}
	c.MarkPairingNotifSent(senderID)
	slog.Info("zalo_oa pairing reply sent", "sender_id", senderID, "code", code)
}
