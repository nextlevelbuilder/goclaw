package whatsapp

import (
	"context"
	"fmt"
	"log/slog"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// checkGroupPolicy evaluates the group policy for a sender.
func (c *Channel) checkGroupPolicy(ctx context.Context, senderID, chatID string) bool {
	result := c.CheckGroupPolicy(ctx, senderID, chatID, c.config.GroupPolicy)
	switch result {
	case channels.PolicyAllow:
		return true
	case channels.PolicyNeedsPairing:
		groupSenderID := fmt.Sprintf("group:%s", chatID)
		c.sendPairingReply(ctx, groupSenderID, chatID)
		return false
	default:
		return false
	}
}

// checkDMPolicy evaluates the DM policy for a sender.
func (c *Channel) checkDMPolicy(ctx context.Context, senderID, chatID string) bool {
	dmPolicy := c.config.DMPolicy
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
		slog.Debug("whatsapp DM rejected by policy", "sender_id", senderID, "policy", dmPolicy)
		return false
	}
}

// sendPairingReply sends a pairing code to the user via WhatsApp.
func (c *Channel) sendPairingReply(ctx context.Context, senderID, chatID string) {
	ps := c.PairingService()
	if ps == nil {
		slog.Warn("whatsapp pairing: no pairing service configured")
		return
	}

	if !c.CanSendPairingNotif(senderID, pairingDebounceTime) {
		slog.Info("whatsapp pairing: debounced", "sender_id", senderID)
		return
	}

	code, err := ps.RequestPairing(ctx, senderID, c.Name(), chatID, "default", nil)
	if err != nil {
		slog.Warn("whatsapp pairing request failed", "sender_id", senderID, "channel", c.Name(), "error", err)
		return
	}

	replyText := fmt.Sprintf(
		"GoClaw: access not configured.\n\nYour WhatsApp ID: %s\n\nPairing code: %s\n\nAsk the account owner to approve with:\n  goclaw pairing approve %s",
		senderID, code, code,
	)

	if c.client == nil || !c.client.IsConnected() {
		slog.Warn("whatsapp not connected, cannot send pairing reply")
		return
	}

	chatJID, parseErr := types.ParseJID(chatID)
	if parseErr != nil {
		slog.Warn("whatsapp pairing: invalid chatID JID", "chatID", chatID, "error", parseErr)
		return
	}

	waMsg := &waE2E.Message{
		Conversation: proto.String(replyText),
	}
	if _, sendErr := c.client.SendMessage(c.ctx, chatJID, waMsg); sendErr != nil {
		slog.Warn("failed to send whatsapp pairing reply", "error", sendErr)
	} else {
		c.MarkPairingNotifSent(senderID)
		slog.Info("whatsapp pairing reply sent", "sender_id", senderID, "code", code)
	}
}
