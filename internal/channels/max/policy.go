package max

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// pairingReplyDebounce limits how often we send a pairing-code reply to
// the same unpaired sender. Without it, every inbound message from an
// unpaired user would generate a fresh pairing code, spamming the chat
// and rotating valid codes too quickly.
const pairingReplyDebounce = 60 * time.Second

// checkDMPolicy enforces dm_policy on a direct-message sender.
// Returns true if the message should proceed to the agent.
//
// Behavior matches whatsapp/discord/slack/feishu — when the sender is
// unpaired and dm_policy is "pairing", a pairing-code reply is sent and
// the message is dropped. The caller MUST NOT proceed to dispatch.
func (c *Channel) checkDMPolicy(ctx context.Context, senderID string, chatID int64) bool {
	dmPolicy := c.cfg.DMPolicy
	if dmPolicy == "" {
		dmPolicy = "open" // Max default per factory.go (consumer-friendly)
	}
	result := c.CheckDMPolicy(ctx, senderID, dmPolicy)
	switch result {
	case channels.PolicyAllow:
		return true
	case channels.PolicyNeedsPairing:
		c.sendPairingReply(ctx, senderID, chatID)
		return false
	default: // PolicyDeny
		slog.Debug("max: DM rejected by policy",
			"channel", c.Name(),
			"sender_id", senderID,
			"policy", dmPolicy)
		return false
	}
}

// checkGroupPolicy enforces group_policy on a group message.
// Returns true if the message should proceed to the agent.
//
// Note: group support in Max is limited at the time of writing — the
// platform does not yet expose adding bots to chats via the public API.
// This function exists so the enforcement path is in place when group
// support lands, and so misconfiguration (group_policy=open while groups
// are unsupported) cannot leak messages.
func (c *Channel) checkGroupPolicy(ctx context.Context, senderID string, chatID int64) bool {
	groupPolicy := c.cfg.GroupPolicy
	if groupPolicy == "" {
		groupPolicy = "disabled"
	}
	chatIDStr := strconv.FormatInt(chatID, 10)
	result := c.CheckGroupPolicy(ctx, senderID, chatIDStr, groupPolicy)
	switch result {
	case channels.PolicyAllow:
		return true
	case channels.PolicyNeedsPairing:
		// For groups, "pairing" means the *group* must be approved as
		// a whole. The pairing reply is addressed to the group chat.
		groupSenderID := "group:" + chatIDStr
		c.sendPairingReply(ctx, groupSenderID, chatID)
		return false
	default: // PolicyDeny
		slog.Debug("max: group message rejected by policy",
			"channel", c.Name(),
			"sender_id", senderID,
			"chat_id", chatIDStr,
			"policy", groupPolicy)
		return false
	}
}

// sendPairingReply requests a pairing code and sends it as a Max message
// to the chat the unpaired sender is writing from.
//
// Debounce: same sender won't get another pairing reply within
// pairingReplyDebounce. Without debounce, repeated inbound messages
// generate fresh codes that invalidate previous ones, breaking the UX
// for the operator who is trying to approve the first code.
func (c *Channel) sendPairingReply(ctx context.Context, senderID string, chatID int64) {
	ps := c.PairingService()
	if ps == nil {
		slog.Warn("max: pairing service not configured — cannot send pairing reply",
			"channel", c.Name(), "sender_id", senderID)
		return
	}

	if !c.CanSendPairingNotif(senderID, pairingReplyDebounce) {
		slog.Debug("max: pairing reply debounced",
			"channel", c.Name(), "sender_id", senderID)
		return
	}

	chatIDStr := strconv.FormatInt(chatID, 10)
	code, err := ps.RequestPairing(ctx, senderID, c.Name(), chatIDStr, "default", nil)
	if err != nil {
		slog.Warn("max: pairing request failed",
			"channel", c.Name(), "sender_id", senderID, "error", err)
		return
	}

	replyText := fmt.Sprintf(
		"🔗 This account hasn't been paired yet.\n\nPairing code: %s\n\nShare this code with the bot owner to get access.",
		code,
	)

	// Send as a plain text message via the Max API. This goes around the
	// agent loop deliberately — the user is unauthorized, so we must not
	// invoke any agent-driven processing.
	req := SendMessageParams{
		ChatID: chatID,
		Body: SendMessageRequest{
			Text:   replyText,
			Format: defaultFormat, // markdown — pairing UX is consistent across channels
		},
	}
	if _, err := c.client.SendMessage(ctx, req); err != nil {
		slog.Warn("max: failed to send pairing reply",
			"channel", c.Name(), "chat_id", chatID, "error", err)
		return
	}

	c.MarkPairingNotifSent(senderID)
	slog.Info("max: pairing reply sent",
		"channel", c.Name(), "sender_id", senderID, "code", code)
}
