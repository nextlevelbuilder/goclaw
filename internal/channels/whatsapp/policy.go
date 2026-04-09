package whatsapp

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// checkGroupPolicy evaluates the group policy for a sender.
func (c *Channel) checkGroupPolicy(ctx context.Context, senderID, chatID string) bool {
	groupPolicy := c.config.GroupPolicy
	if groupPolicy == "" {
		groupPolicy = "open"
	}

	switch groupPolicy {
	case "disabled":
		return false
	case "allowlist":
		return c.IsAllowed(senderID)
	case "pairing":
		if c.HasAllowList() && c.IsAllowed(senderID) {
			return true
		}
		if _, cached := c.approvedGroups.Load(chatID); cached {
			return true
		}
		groupSenderID := fmt.Sprintf("group:%s", chatID)
		if c.pairingService != nil {
			paired, err := c.pairingService.IsPaired(ctx, groupSenderID, c.Name())
			if err != nil {
				slog.Warn("security.pairing_check_failed, assuming paired (fail-open)",
					"group_sender", groupSenderID, "channel", c.Name(), "error", err)
				paired = true
			}
			if paired {
				c.approvedGroups.Store(chatID, true)
				return true
			}
		}
		c.sendPairingReply(ctx, groupSenderID, chatID)
		return false
	default: // "open"
		return true
	}
}

// checkDMPolicy evaluates the DM policy for a sender.
func (c *Channel) checkDMPolicy(ctx context.Context, senderID, chatID string) bool {
	dmPolicy := c.config.DMPolicy
	if dmPolicy == "" {
		dmPolicy = "pairing"
	}

	switch dmPolicy {
	case "disabled":
		slog.Debug("whatsapp DM rejected: disabled", "sender_id", senderID)
		return false
	case "open":
		return true
	case "allowlist":
		if !c.IsAllowed(senderID) {
			slog.Debug("whatsapp DM rejected by allowlist", "sender_id", senderID)
			return false
		}
		return true
	default: // "pairing"
		paired := false
		if c.pairingService != nil {
			p, err := c.pairingService.IsPaired(ctx, senderID, c.Name())
			if err != nil {
				slog.Warn("security.pairing_check_failed, assuming paired (fail-open)",
					"sender_id", senderID, "channel", c.Name(), "error", err)
				paired = true
			} else {
				paired = p
			}
		}
		inAllowList := c.HasAllowList() && c.IsAllowed(senderID)

		if paired || inAllowList {
			return true
		}

		c.sendPairingReply(ctx, senderID, chatID)
		return false
	}
}

// sendPairingReply sends a pairing code to the user via WhatsApp.
func (c *Channel) sendPairingReply(ctx context.Context, senderID, chatID string) {
	if c.pairingService == nil {
		slog.Warn("whatsapp pairing: no pairing service configured")
		return
	}

	// Debounce.
	if lastSent, ok := c.pairingDebounce.Load(senderID); ok {
		if time.Since(lastSent.(time.Time)) < pairingDebounceTime {
			slog.Info("whatsapp pairing: debounced", "sender_id", senderID)
			return
		}
	}

	code, err := c.pairingService.RequestPairing(ctx, senderID, c.Name(), chatID, "default", nil)
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
		c.pairingDebounce.Store(senderID, time.Now())
		slog.Info("whatsapp pairing reply sent", "sender_id", senderID, "code", code)
	}
}
