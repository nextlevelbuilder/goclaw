package dingtalk

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/systemmessages"
)

// pairingDebounceTime keeps a bot from repeating "you need to pair" at every
// message from an unpaired sender.
const pairingDebounceTime = 60 * time.Second

// isInGroupAllowList reports whether senderID is listed in group_allow_from.
// This is a per-sender bypass, distinct from the base allow_from list.
func (c *Channel) isInGroupAllowList(senderID string) bool {
	for _, allowed := range c.cfg.GroupAllowFrom {
		if senderID == allowed || strings.TrimPrefix(allowed, "@") == senderID {
			return true
		}
	}
	return false
}

// checkGroupPolicy reports whether a group message may proceed.
func (c *Channel) checkGroupPolicy(ctx context.Context, in *inbound, chatID string) bool {
	switch c.cfg.GroupPolicy {
	case "disabled":
		return false

	case "allowlist":
		return c.IsAllowed(in.SenderID) || c.isInGroupAllowList(in.SenderID)

	case "pairing":
		if c.isInGroupAllowList(in.SenderID) {
			return true
		}
		// The group, not the speaker, is what gets paired.
		groupSenderID := fmt.Sprintf("group:%s", chatID)
		switch c.CheckGroupPolicy(ctx, in.SenderID, chatID, "pairing") {
		case channels.PolicyAllow:
			return true
		case channels.PolicyNeedsPairing:
			c.sendPairingReply(ctx, groupSenderID, chatID, in)
			return false
		default:
			return false
		}

	default: // "open"
		return true
	}
}

// checkDMPolicy reports whether a direct message may proceed.
func (c *Channel) checkDMPolicy(ctx context.Context, in *inbound) bool {
	switch c.CheckDMPolicy(ctx, in.SenderID, c.cfg.DMPolicy) {
	case channels.PolicyAllow:
		return true
	case channels.PolicyNeedsPairing:
		c.sendPairingReply(ctx, in.SenderID, in.SenderID, in)
		return false
	default:
		slog.Debug("dingtalk DM rejected by policy",
			"channel", c.Name(), "sender_id", in.SenderID, "policy", c.cfg.DMPolicy)
		return false
	}
}

// sendPairingReply issues a pairing code and tells the sender how to use it.
//
// It fails closed: if the pairing store cannot answer, nobody is let through and
// nothing is sent.
func (c *Channel) sendPairingReply(ctx context.Context, senderID, chatID string, in *inbound) {
	ps := c.PairingService()
	if ps == nil {
		return
	}
	channelName := c.Name()

	paired, err := ps.IsPaired(ctx, senderID, channelName)
	if err != nil {
		slog.Warn("security.pairing_check_failed, denying pairing request (fail-closed)",
			"channel", channelName, "sender_id", senderID, "chat_id", chatID,
			"tenant_id", c.TenantID(), "error", err)
		return
	}
	if paired {
		if strings.HasPrefix(senderID, "group:") {
			c.MarkGroupApproved(chatID)
		}
		return
	}

	if !c.CanSendPairingNotif(senderID, pairingDebounceTime) {
		return
	}

	code, err := ps.RequestPairing(ctx, senderID, channelName, chatID, "default", nil)
	if err != nil {
		slog.Debug("dingtalk pairing request failed",
			"channel", channelName, "sender_id", senderID, "error", err)
		return
	}

	text := c.SystemMessage("", systemmessages.KeyPairingAccountRequired, systemmessages.Vars{
		"platform":  "DingTalk",
		"sender_id": senderID,
		"code":      code,
	})

	if err := c.replyWebhookText(ctx, in.SessionWebhook, text); err != nil {
		slog.Warn("failed to send dingtalk pairing reply",
			"channel", channelName, "sender_id", senderID, "chat_id", chatID, "error", err)
		return
	}

	c.MarkPairingNotifSent(senderID)
	slog.Info("dingtalk pairing reply sent",
		"channel", channelName, "sender_id", senderID, "chat_id", chatID, "code", code)
}
