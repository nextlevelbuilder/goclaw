package feishu

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
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

func (c *Channel) checkGroupPolicy(ctx context.Context, senderID, chatID string) bool {
	groupPolicy := c.cfg.GroupPolicy
	if groupPolicy == "" {
		groupPolicy = "open"
	}

	switch groupPolicy {
	case "disabled":
		return false
	case "allowlist":
		if c.IsAllowed(senderID) {
			return true
		}
		for _, allowed := range c.groupAllowList {
			if senderID == allowed || strings.TrimPrefix(allowed, "@") == senderID {
				return true
			}
		}
		return false
	case "pairing":
		// Allowlist bypass (per-user)
		inAllowList := c.HasAllowList() && c.IsAllowed(senderID)
		inGroupAllowList := false
		for _, allowed := range c.groupAllowList {
			if senderID == allowed || strings.TrimPrefix(allowed, "@") == senderID {
				inGroupAllowList = true
				break
			}
		}
		if inAllowList || inGroupAllowList {
			return true
		}

		// Group-level pairing (one approval per group, matching Telegram pattern)
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

func (c *Channel) checkDMPolicy(ctx context.Context, senderID, chatID string) bool {
	dmPolicy := c.cfg.DMPolicy
	if dmPolicy == "" {
		dmPolicy = "pairing"
	}

	switch dmPolicy {
	case "disabled":
		slog.Debug("feishu DM rejected: disabled", "sender_id", senderID)
		return false
	case "open":
		return true
	case "allowlist":
		if !c.IsAllowed(senderID) {
			slog.Debug("feishu DM rejected by allowlist", "sender_id", senderID)
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
	if c.pairingService == nil {
		return
	}

	// Debounce
	if lastSent, ok := c.pairingDebounce.Load(senderID); ok {
		if t, ok := lastSent.(time.Time); ok && time.Since(t) < pairingDebounceTime {
			return
		}
	}

	code, err := c.pairingService.RequestPairing(ctx, senderID, c.Name(), chatID, "default", nil)
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
		c.pairingDebounce.Store(senderID, time.Now())
		slog.Info("feishu pairing reply sent", "sender_id", senderID, "code", code)
	}
}
