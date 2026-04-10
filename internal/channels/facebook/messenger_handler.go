package facebook

import (
	"fmt"
	"log/slog"
)

// handleMessagingEvent processes a Messenger inbox event.
func (ch *Channel) handleMessagingEvent(entry WebhookEntry, event MessagingEvent) {
	// Feature gate.
	if !ch.config.Features.MessengerAutoReply {
		return
	}

	// Page routing guard (before dedup write).
	if entry.ID != ch.pageID {
		return
	}

	// Self-message prevention: skip messages sent by the page itself.
	if event.Sender.ID == ch.pageID {
		return
	}

	// Skip delivery/read receipts and other non-content events.
	if event.Message == nil && event.Postback == nil {
		return
	}

	// Dedup by message MID or postback signature (include payload to reduce collision risk).
	var eventKey string
	switch {
	case event.Message != nil:
		eventKey = "msg:" + event.Message.MID
	case event.Postback != nil:
		eventKey = fmt.Sprintf("postback:%s:%d:%s", event.Sender.ID, event.Timestamp, event.Postback.Payload)
	}
	if ch.isDup(eventKey) {
		slog.Debug("facebook: duplicate messaging event skipped", "key", eventKey)
		return
	}

	// Extract text content.
	var content string
	switch {
	case event.Message != nil && event.Message.Text != "":
		content = event.Message.Text
	case event.Postback != nil:
		content = event.Postback.Title
	default:
		// Attachment-only message — skip for now.
		return
	}

	senderID := event.Sender.ID
	// Messenger sessions are 1:1: chatID = senderID (channel name scopes the session).
	chatID := senderID

	metadata := map[string]string{
		"fb_mode":    "messenger",
		"message_id": eventKey,
		"page_id":    ch.pageID,
		"sender_id":  senderID,
	}
	if ch.config.MessengerOptions.SessionTimeout != "" {
		metadata["session_timeout"] = ch.config.MessengerOptions.SessionTimeout
	}

	ch.HandleMessage(senderID, chatID, content, nil, metadata, "direct")
}
