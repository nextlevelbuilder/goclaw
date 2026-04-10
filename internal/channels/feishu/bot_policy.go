package feishu

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// --- Sender name resolution ---

func (c *Channel) resolveSenderName(ctx context.Context, openID string) string {
	if openID == "" {
		return ""
	}

	// Check cache
	if entry, ok := c.senderCache.Load(openID); ok {
		e := entry.(*senderCacheEntry)
		if time.Now().Before(e.expiresAt) {
			return e.name
		}
		c.senderCache.Delete(openID)
	}

	// Fetch from API
	name := c.fetchSenderName(ctx, openID)
	if name != "" {
		c.senderCache.Store(openID, &senderCacheEntry{
			name:      name,
			expiresAt: time.Now().Add(senderCacheTTL),
		})
	}
	return name
}

func (c *Channel) fetchSenderName(ctx context.Context, openID string) string {
	name, err := c.client.GetUser(ctx, openID, "open_id")
	if err != nil {
		slog.Debug("feishu fetch sender name failed", "open_id", openID, "error", err)
		return ""
	}
	return name
}

// --- Policy checks ---

// isInGroupAllowList checks whether senderID is in the Feishu-specific group allowlist.
func (c *Channel) isInGroupAllowList(senderID string) bool {
	for _, allowed := range c.groupAllowList {
		if senderID == allowed || strings.TrimPrefix(allowed, "@") == senderID {
			return true
		}
	}
	return false
}

func (c *Channel) checkGroupPolicy(ctx context.Context, senderID, chatID string) bool {
	groupPolicy := c.cfg.GroupPolicy
	if groupPolicy == "" {
		groupPolicy = "open"
	}

	switch groupPolicy {
	case "disabled":
		return false
	case "allowlist":
		return c.IsAllowed(senderID) || c.isInGroupAllowList(senderID)
	case "pairing":
		// Feishu groupAllowList bypass: per-user sender allowlist specific to Feishu.
		if c.isInGroupAllowList(senderID) {
			return true
		}
		// Delegate remaining pairing logic to BaseChannel (handles allowList, approvedGroups, DB check).
		result := c.CheckGroupPolicy(ctx, senderID, chatID, groupPolicy)
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
	default: // "open"
		return true
	}
}

func (c *Channel) checkDMPolicy(ctx context.Context, senderID, chatID string) bool {
	result := c.CheckDMPolicy(ctx, senderID, c.cfg.DMPolicy)
	switch result {
	case channels.PolicyAllow:
		return true
	case channels.PolicyNeedsPairing:
		c.sendPairingReply(ctx, senderID, chatID)
		return false
	default:
		slog.Debug("feishu DM rejected by policy", "sender_id", senderID, "policy", c.cfg.DMPolicy)
		return false
	}
}

func (c *Channel) sendPairingReply(ctx context.Context, senderID, chatID string) {
	ps := c.PairingService()
	if ps == nil {
		return
	}

	if !c.CanSendPairingNotif(senderID, pairingDebounceTime) {
		return
	}

	code, err := ps.RequestPairing(ctx, senderID, c.Name(), chatID, "default", nil)
	if err != nil {
		slog.Debug("feishu pairing request failed", "sender_id", senderID, "error", err)
		return
	}

	replyText := fmt.Sprintf(
		"GoClaw: access not configured.\n\nYour Feishu open_id: %s\n\nPairing code: %s\n\nAsk the bot owner to approve with:\n  goclaw pairing approve %s",
		senderID, code, code,
	)

	receiveIDType := resolveReceiveIDType(chatID)
	if err := c.sendText(context.Background(), chatID, receiveIDType, replyText); err != nil {
		slog.Warn("failed to send feishu pairing reply", "error", err)
	} else {
		c.MarkPairingNotifSent(senderID)
		slog.Info("feishu pairing reply sent", "sender_id", senderID, "code", code)
	}
}
