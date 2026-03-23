package personal

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
)

const pairingDebounce = 60 * time.Second

// checkDMPolicy enforces DM policy for incoming messages.
func (c *Channel) checkDMPolicy(ctx context.Context, senderID, chatID string) bool {
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

func (c *Channel) sendPairingReply(ctx context.Context, senderID, chatID string) {
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

	code, err := c.pairingService.RequestPairing(ctx, senderID, c.Name(), chatID, "default", nil)
	if err != nil {
		slog.Debug("zalo_personal pairing request failed", "sender_id", senderID, "error", err)
		return
	}

	replyText := fmt.Sprintf(
		"GoClaw: access not configured.\n\nYour Zalo user id: %s\n\nPairing code: %s\n\nAsk the bot owner to approve with:\n  goclaw pairing approve %s",
		senderID, code, code,
	)

	threadType := protocol.ThreadTypeUser
	if strings.HasPrefix(senderID, "group:") {
		threadType = protocol.ThreadTypeGroup
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := protocol.SendMessage(ctx, sess, chatID, threadType, replyText); err != nil {
		slog.Warn("zalo_personal: failed to send pairing reply", "error", err)
	} else {
		c.pairingDebounce.Store(senderID, time.Now())
		slog.Info("zalo_personal pairing reply sent", "sender_id", senderID, "code", code)
	}
}

// checkGroupPolicy enforces group access policy (allowlist/pairing).
// Returns false if the group is blocked by policy; does NOT check @mention gating.
func (c *Channel) checkGroupPolicy(ctx context.Context, senderID, groupID string) bool {
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
		if c.HasAllowList() && c.IsAllowed(groupID) {
			// pass — allowlist bypass
		} else if _, cached := c.approvedGroups.Load(groupID); cached {
			// pass — already approved
		} else {
			groupSenderID := fmt.Sprintf("group:%s", groupID)
			paired := false
			if c.pairingService != nil {
				p, err := c.pairingService.IsPaired(ctx, groupSenderID, c.Name())
				if err != nil {
					slog.Warn("security.pairing_check_failed, assuming paired (fail-open)",
						"group_sender", groupSenderID, "channel", c.Name(), "error", err)
					p = true
				}
				paired = p
			}
			if paired {
				c.approvedGroups.Store(groupID, true)
			} else {
				c.sendPairingReply(ctx, groupSenderID, groupID)
				return false
			}
		}
	}

	return true
}

// checkBotMentioned reports whether the bot is @mentioned in the message.
func (c *Channel) checkBotMentioned(mentions []*protocol.TMention) bool {
	sess := c.session()
	if sess == nil {
		return false
	}
	return isBotMentioned(sess.UID, mentions)
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
