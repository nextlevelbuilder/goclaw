package instagram

import (
	"log/slog"
	"strconv"
	"time"
)

// handleMessagingEvent processes incoming Instagram Direct Messages.
func (ch *Channel) handleMessagingEvent(entry WebhookEntry, event MessagingEvent) {
	// Feature gate.
	if !ch.config.Features.AutoReply {
		return
	}

	// Tenant / IG-account routing guard — reject events for other Instagram
	// accounts that happen to land on this handler (shared webhook endpoint).
	if entry.ID != ch.instagramID {
		return
	}

	// Reject non-content events (reactions, read receipts, seen, etc.).
	if event.Message == nil {
		return
	}

	// Echo branch: sender is this IG account.
	// - Within botEchoWindow of our last Send() → our own message, drop silently.
	// - Outside window → admin manually replied in the IG app, start cooldown.
	if event.Message.IsMessageEcho || event.Sender.ID == ch.instagramID {
		if event.Recipient.ID != "" {
			eventAt := messagingEventTime(event.Timestamp)
			if ch.isBotEcho(event.Recipient.ID, eventAt) {
				slog.Debug("instagram: bot echo ignored", "chat_id", event.Recipient.ID)
				return
			}
			ch.adminReplied.Store(event.Recipient.ID, eventAt)
			slog.Debug("instagram: admin reply tracked", "chat_id", event.Recipient.ID)
		}
		return
	}

	// Dedup by message mid when present, fall back to sender+timestamp.
	// NOTE: never use string(rune(int64)) — it maps large timestamps to U+FFFD
	// and collapses every event from the same sender to the same key.
	var dedupKey string
	if event.Message.ID != "" {
		dedupKey = "msg:" + event.Message.ID
	} else {
		dedupKey = entry.ID + ":" + event.Sender.ID + ":" + strconv.FormatInt(event.Timestamp, 10)
	}
	if ch.isDup(dedupKey) {
		slog.Debug("instagram: skipping duplicate event", "dedup_key", dedupKey)
		return
	}

	senderID := event.Sender.ID
	chatID := event.Sender.ID // Instagram DMs use sender ID as chat ID

	// Skip when admin just replied manually — defer to human.
	if ch.adminRepliedRecently(chatID, time.Now()) {
		slog.Info("instagram: skipping auto-reply (admin replied recently)", "chat_id", chatID)
		return
	}

	content := event.Message.Text

	var media []string
	for _, att := range event.Message.Attachments {
		if att.URL != "" {
			media = append(media, att.URL)
		} else if att.Payload.URL != "" {
			media = append(media, att.Payload.URL)
		}
	}

	if len(event.Message.Reactions) > 0 {
		slog.Debug("instagram: message with reactions", "reactions", len(event.Message.Reactions))
	}

	// Skip empty messages.
	if content == "" && len(media) == 0 {
		slog.Debug("instagram: empty message, skipping")
		return
	}

	// Check policy.
	if !ch.CheckPolicy("direct", "pairing", "open", senderID) {
		slog.Debug("instagram: message blocked by policy", "sender_id", senderID)
		return
	}

	ch.HandleMessage(senderID, chatID, content, media, map[string]string{
		"instagram_user_id": entry.ID,
		"message_id":        event.Message.ID,
		"timestamp":         strconv.FormatInt(event.Timestamp, 10),
	}, "direct")

	slog.Info("instagram: message received",
		"sender_id", senderID,
		"chat_id", chatID,
		"content_len", len(content))
}
