package slack

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	slackapi "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (c *Channel) handleAppMention(ev *slackevents.AppMentionEvent) {
	ctx := context.Background()
	ctx = store.WithTenantID(ctx, c.TenantID())
	if ev.User == c.botUserID || ev.User == "" {
		return
	}

	// If requireMention is false, message handler already processes all channel messages.
	// Return BEFORE storing dedup key so we don't block the message handler.
	if !c.requireMention {
		return
	}

	// Dedup: app_mention may arrive alongside a message event.
	// Shares the same key format as handleMessage so whichever arrives first
	// processes the mention; the other is safely dropped.
	dedupKey := ev.Channel + ":" + ev.TimeStamp
	if _, loaded := c.dedup.LoadOrStore(dedupKey, time.Now()); loaded {
		return
	}

	senderID := ev.User
	channelID := ev.Channel
	content := ev.Text

	displayName := strings.ReplaceAll(c.resolveDisplayName(senderID), "|", "_")
	compoundSenderID := fmt.Sprintf("%s|%s", senderID, displayName)

	if !c.checkGroupPolicy(ctx, senderID, channelID) {
		return
	}

	content = c.stripBotMention(content)
	content = strings.TrimSpace(content)

	if content == "" {
		return
	}

	localKey := channelID
	threadTS := ev.ThreadTimeStamp
	if threadTS != "" {
		localKey = fmt.Sprintf("%s:thread:%s", channelID, threadTS)
	}

	slog.Debug("slack app_mention received",
		"sender_id", senderID, "channel_id", channelID,
		"preview", channels.Truncate(content, 50))

	replyThreadTS := threadTS
	if replyThreadTS == "" {
		replyThreadTS = ev.TimeStamp
	}

	placeholderOpts := []slackapi.MsgOption{
		slackapi.MsgOptionText("Thinking...", false),
	}
	if replyThreadTS != "" {
		placeholderOpts = append(placeholderOpts, slackapi.MsgOptionTS(replyThreadTS))
	}

	_, placeholderTS, err := c.api.PostMessage(channelID, placeholderOpts...)
	if err == nil {
		c.placeholders.Store(localKey, placeholderTS)
	}

	annotated := fmt.Sprintf("[From: %s]\n%s", displayName, content)
	finalContent := annotated
	if c.historyLimit > 0 {
		finalContent = c.groupHistory.BuildContext(localKey, annotated, c.historyLimit)
	}

	metadata := map[string]string{
		"message_id":      ev.TimeStamp,
		"user_id":         senderID,
		"username":        displayName,
		"channel_id":      channelID,
		"is_dm":           "false",
		"local_key":       localKey,
		"placeholder_key": localKey,
	}
	if replyThreadTS != "" {
		metadata["message_thread_id"] = replyThreadTS
	}

	c.HandleMessage(compoundSenderID, channelID, finalContent, nil, metadata, "group")

	// Record thread participation
	if replyThreadTS != "" {
		participKey := channelID + ":particip:" + replyThreadTS
		c.threadParticip.Store(participKey, time.Now())
	}

	c.groupHistory.Clear(localKey)
}

// isBotMentioned checks if the message text contains <@botUserID>.
func (c *Channel) isBotMentioned(text string) bool {
	return strings.Contains(text, "<@"+c.botUserID+">")
}

// stripBotMention removes <@botUserID> from message text.
func (c *Channel) stripBotMention(text string) string {
	return strings.ReplaceAll(text, "<@"+c.botUserID+">", "")
}

// --- Policy checks ---

func (c *Channel) checkDMPolicy(ctx context.Context, senderID, channelID string) bool {
	dmPolicy := c.config.DMPolicy
	if dmPolicy == "" {
		dmPolicy = "pairing"
	}

	switch dmPolicy {
	case "disabled":
		return false
	case "open":
		return true
	case "allowlist":
		return c.HasAllowList() && c.IsAllowed(senderID)
	default: // "pairing"
		if c.pairingService != nil {
			paired, err := c.pairingService.IsPaired(ctx, senderID, c.Name())
			if err != nil {
				slog.Warn("security.pairing_check_failed, assuming paired (fail-open)",
					"sender_id", senderID, "channel", c.Name(), "error", err)
				return true
			}
			if paired {
				return true
			}
		}
		if c.HasAllowList() && c.IsAllowed(senderID) {
			return true
		}
		c.sendPairingReply(ctx, senderID, channelID)
		return false
	}
}

func (c *Channel) checkGroupPolicy(ctx context.Context, senderID, channelID string) bool {
	groupPolicy := c.config.GroupPolicy
	if groupPolicy == "" {
		groupPolicy = "open"
	}

	switch groupPolicy {
	case "disabled":
		return false
	case "allowlist":
		if !c.HasAllowList() {
			return false
		}
		// Allow if user ID or channel ID is in the allowlist
		return c.IsAllowed(senderID) || c.IsAllowed(channelID)
	case "pairing":
		if c.HasAllowList() && c.IsAllowed(senderID) {
			return true
		}
		if _, cached := c.approvedGroups.Load(channelID); cached {
			return true
		}
		groupSenderID := fmt.Sprintf("group:%s", channelID)
		if c.pairingService != nil {
			paired, err := c.pairingService.IsPaired(ctx, groupSenderID, c.Name())
			if err != nil {
				slog.Warn("security.pairing_check_failed, assuming paired (fail-open)",
					"group_sender", groupSenderID, "channel", c.Name(), "error", err)
				paired = true
			}
			if paired {
				c.approvedGroups.Store(channelID, true)
				return true
			}
		}
		c.sendPairingReply(ctx, groupSenderID, channelID)
		return false
	default: // "open"
		return true
	}
}

func (c *Channel) sendPairingReply(ctx context.Context, senderID, channelID string) {
	if c.pairingService == nil {
		return
	}

	if lastSent, ok := c.pairingDebounce.Load(senderID); ok {
		if time.Since(lastSent.(time.Time)) < pairingDebounceTime {
			return
		}
	}

	code, err := c.pairingService.RequestPairing(ctx, senderID, c.Name(), channelID, "default", nil)
	if err != nil {
		slog.Warn("slack: failed to request pairing code", "error", err)
		return
	}

	// Security: do not expose pairing code in group channels (visible to all members).
	// Instead, direct admin to CLI or web UI where pending codes are listed.
	var msg string
	if strings.HasPrefix(senderID, "group:") {
		msg = fmt.Sprintf("This channel is not authorized to use this bot.\n\n"+
			"An admin can approve via CLI:\n  goclaw pairing approve %s\n\n"+
			"Or approve via the GoClaw web UI (Pairing section).", code)
	} else {
		msg = fmt.Sprintf("GoClaw: access not configured.\n\nYour Slack user ID: %s\n\nPairing code: %s\n\nAsk the bot owner to approve with:\n  goclaw pairing approve %s",
			senderID, code, code)
	}
	if _, _, err := c.api.PostMessage(channelID, slackapi.MsgOptionText(msg, false)); err != nil {
		slog.Warn("slack: failed to send pairing reply",
			"channel_id", channelID, "error", err)
	}
	c.pairingDebounce.Store(senderID, time.Now())
}
