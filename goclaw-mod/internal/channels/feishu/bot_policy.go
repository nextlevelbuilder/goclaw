package feishu

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/systemmessages"
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
	channelName := c.channelNameForLog()

	switch groupPolicy {
	case "disabled":
		return false
	case "allowlist":
		return c.IsAllowed(senderID) || c.isInGroupAllowList(senderID)
	case "pairing":
		// Feishu groupAllowList bypass: per-user sender allowlist specific to Feishu.
		if c.isInGroupAllowList(senderID) {
			slog.Debug("feishu group policy allowed by group allowlist",
				"channel", channelName,
				"sender_id", senderID,
				"chat_id", chatID,
				"group_policy", groupPolicy,
			)
			return true
		}
		// Delegate remaining pairing logic to BaseChannel (handles allowList, approvedGroups, DB check).
		result := c.CheckGroupPolicy(ctx, senderID, chatID, groupPolicy)
		groupSenderID := fmt.Sprintf("group:%s", chatID)
		slog.Debug("feishu group policy checked",
			"channel", channelName,
			"sender_id", senderID,
			"group_sender_id", groupSenderID,
			"chat_id", chatID,
			"group_policy", groupPolicy,
			"result", policyResultLabel(result),
			"tenant_id", c.TenantID(),
		)
		switch result {
		case channels.PolicyAllow:
			return true
		case channels.PolicyNeedsPairing:
			c.sendPairingReply(ctx, groupSenderID, chatID)
			return false
		default:
			return false
		}
	default: // "open"
		return true
	}
}

func (c *Channel) canRecordUnmentionedGroupMessage(ctx context.Context, senderID, chatID string) bool {
	groupPolicy := c.cfg.GroupPolicy
	if groupPolicy == "" {
		groupPolicy = "open"
	}
	channelName := c.channelNameForLog()

	switch groupPolicy {
	case "disabled":
		return false
	case "allowlist":
		return c.IsAllowed(senderID) || c.isInGroupAllowList(senderID)
	case "pairing":
		if c.isInGroupAllowList(senderID) {
			return true
		}
		result := c.CheckGroupPolicy(ctx, senderID, chatID, groupPolicy)
		slog.Debug("feishu unmentioned group history policy checked",
			"channel", channelName,
			"sender_id", senderID,
			"group_sender_id", fmt.Sprintf("group:%s", chatID),
			"chat_id", chatID,
			"group_policy", groupPolicy,
			"result", policyResultLabel(result),
			"tenant_id", c.TenantID(),
		)
		return result == channels.PolicyAllow
	default:
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
	channelName := c.channelNameForLog()

	paired, err := ps.IsPaired(ctx, senderID, channelName)
	if err != nil {
		slog.Warn("security.pairing_check_failed, denying pairing request (fail-closed)",
			"sender_id", senderID,
			"channel", channelName,
			"chat_id", chatID,
			"tenant_id", c.TenantID(),
			"error", err,
		)
		return
	}
	if paired {
		if strings.HasPrefix(senderID, "group:") {
			c.MarkGroupApproved(chatID)
		}
		slog.Debug("feishu pairing reply skipped; sender already paired",
			"sender_id", senderID,
			"channel", channelName,
			"chat_id", chatID,
			"tenant_id", c.TenantID(),
			"source", pairingReplySource(senderID),
		)
		return
	}
	slog.Debug("feishu pairing reply gate needs pairing",
		"sender_id", senderID,
		"channel", channelName,
		"chat_id", chatID,
		"tenant_id", c.TenantID(),
		"source", pairingReplySource(senderID),
	)

	if !c.CanSendPairingNotif(senderID, pairingDebounceTime) {
		slog.Debug("feishu pairing reply skipped by debounce",
			"sender_id", senderID,
			"channel", channelName,
			"chat_id", chatID,
			"tenant_id", c.TenantID(),
			"source", pairingReplySource(senderID),
		)
		return
	}

	code, err := ps.RequestPairing(ctx, senderID, channelName, chatID, "default", nil)
	if err != nil {
		slog.Debug("feishu pairing request failed",
			"sender_id", senderID,
			"channel", channelName,
			"chat_id", chatID,
			"tenant_id", c.TenantID(),
			"source", pairingReplySource(senderID),
			"error", err,
		)
		return
	}

	replyText := c.SystemMessage("", systemmessages.KeyPairingAccountRequired, systemmessages.Vars{
		"platform":  "Feishu",
		"sender_id": senderID,
		"code":      code,
	})

	receiveIDType := resolveReceiveIDType(chatID)
	if err := c.sendText(context.Background(), chatID, receiveIDType, replyText, ""); err != nil {
		slog.Warn("failed to send feishu pairing reply",
			"sender_id", senderID,
			"channel", channelName,
			"chat_id", chatID,
			"tenant_id", c.TenantID(),
			"source", pairingReplySource(senderID),
			"error", err,
		)
	} else {
		c.MarkPairingNotifSent(senderID)
		slog.Info("feishu pairing reply sent",
			"sender_id", senderID,
			"channel", channelName,
			"chat_id", chatID,
			"tenant_id", c.TenantID(),
			"source", pairingReplySource(senderID),
			"code", code,
		)
	}
}

func (c *Channel) channelNameForLog() string {
	if c == nil || c.BaseChannel == nil {
		return ""
	}
	return c.Name()
}

func pairingReplySource(senderID string) string {
	if strings.HasPrefix(senderID, "group:") {
		return "group_policy"
	}
	return "dm_policy"
}

func policyResultLabel(result channels.PolicyResult) string {
	switch result {
	case channels.PolicyAllow:
		return "allow"
	case channels.PolicyDeny:
		return "deny"
	case channels.PolicyNeedsPairing:
		return "needs_pairing"
	default:
		return fmt.Sprintf("unknown_%d", result)
	}
}
