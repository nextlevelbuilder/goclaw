package personal

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
)

const pairingDebounce = 60 * time.Second

// checkDMPolicy enforces DM policy for incoming messages.
func (c *Channel) checkDMPolicy(senderID, chatID string) bool {
	dmPolicy := c.config.DMPolicy
	if dmPolicy == "" {
		dmPolicy = "allowlist"
	}

	switch dmPolicy {
	case "disabled":
		slog.Debug("zalo_personal DM rejected: DMs disabled", "sender_id", senderID)
		return false

	case "open":
		return true

	case "allowlist":
		if !c.IsAllowed(senderID) {
			slog.Debug("zalo_personal DM rejected by allowlist", "sender_id", senderID)
			return false
		}
		return true

	default: // "pairing"
		paired := false
		if c.pairingService != nil {
			paired = c.pairingService.IsPaired(senderID, c.Name())
		}
		inAllowList := c.HasAllowList() && c.IsAllowed(senderID)

		if paired || inAllowList {
			return true
		}

		c.sendPairingReply(senderID, chatID)
		return false
	}
}

func (c *Channel) sendPairingReply(senderID, chatID string) {
	sess := c.session()
	if c.pairingService == nil || sess == nil {
		return
	}

	// Debounce: one reply per sender per 60s.
	if lastSent, ok := c.pairingDebounce.Load(senderID); ok {
		if time.Since(lastSent.(time.Time)) < pairingDebounce {
			return
		}
	}

	code, err := c.pairingService.RequestPairing(senderID, c.Name(), chatID, "default")
	if err != nil {
		slog.Debug("zalo_personal pairing request failed", "sender_id", senderID, "error", err)
		return
	}

	replyText := fmt.Sprintf(
		"GoClaw: access not configured.\n\nYour Zalo user id: %s\n\nPairing code: %s\n\nAsk the bot owner to approve with:\n  goclaw pairing approve %s",
		senderID, code, code,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := protocol.SendMessage(ctx, sess, chatID, protocol.ThreadTypeUser, replyText); err != nil {
		slog.Warn("zalo_personal: failed to send pairing reply", "error", err)
	} else {
		c.pairingDebounce.Store(senderID, time.Now())
		slog.Info("zalo_personal pairing reply sent", "sender_id", senderID, "code", code)
	}
}

// checkGroupPolicy enforces group policy and @mention gating.
func (c *Channel) checkGroupPolicy(senderID, groupID string, mentions []*protocol.TMention) bool {
	groupPolicy := c.config.GroupPolicy
	if groupPolicy == "" {
		groupPolicy = "allowlist"
	}

	switch groupPolicy {
	case "disabled":
		slog.Debug("zalo_personal group message rejected: groups disabled", "group_id", groupID)
		return false

	case "allowlist":
		if !c.IsAllowed(groupID) {
			slog.Debug("zalo_personal group message rejected by allowlist", "group_id", groupID)
			return false
		}

	case "pairing":
		if c.IsAllowed(groupID) {
			// pass — allowlist bypass
		} else if _, cached := c.approvedGroups.Load(groupID); cached {
			// pass — already approved
		} else {
			groupSenderID := fmt.Sprintf("group:%s", groupID)
			if c.pairingService != nil && c.pairingService.IsPaired(groupSenderID, c.Name()) {
				c.approvedGroups.Store(groupID, true)
			} else {
				c.sendPairingReply(groupSenderID, groupID)
				return false
			}
		}
	}

	// @mention gating: only process group messages that @mention the bot.
	if c.requireMention {
		sess := c.session()
		botUID := ""
		if sess != nil {
			botUID = sess.UID
		}
		if !isBotMentioned(botUID, mentions) {
			slog.Debug("zalo_personal group message skipped: not mentioned",
				"group_id", groupID,
				"sender_id", senderID,
			)
			return false
		}
	}

	return true
}

// isBotMentioned checks if the bot's UID is @mentioned in the message.
// Filters out @all mentions (Type=1, UID="-1") — only targeted @bot counts.
func isBotMentioned(botUID string, mentions []*protocol.TMention) bool {
	if botUID == "" {
		return false
	}

	for _, m := range mentions {
		if m == nil {
			continue
		}
		if m.Type == protocol.MentionAll || m.UID == protocol.MentionAllUID {
			continue
		}
		if m.UID == botUID {
			return true
		}
	}
	return false
}
