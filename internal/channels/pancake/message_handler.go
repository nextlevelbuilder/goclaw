package pancake

import (
	"fmt"
	"html"
	"log/slog"
	"slices"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// handleMessagingEvent converts a Pancake "messaging" webhook event to bus.InboundMessage.
func (ch *Channel) handleMessagingEvent(data MessagingData) {
	slog.Debug("pancake: handleMessagingEvent called",
		"page_id", ch.pageID,
		"sender_id", data.Message.SenderID,
		"conversation_id", data.ConversationID,
		"type", data.Type,
		"platform", data.Platform,
		"msg_id", data.Message.ID,
		"content_len", len(data.Message.Content))

	// Dedup by message ID to handle Pancake's at-least-once delivery.
	// Skip dedup when message ID is empty — prevents shared slot "msg:" from
	// silently dropping all subsequent empty-ID messages across conversations.
	var dedupKey string
	if data.Message.ID != "" {
		dedupKey = fmt.Sprintf("msg:%s", data.Message.ID)
		if ch.isDup(dedupKey) {
			slog.Info("pancake: duplicate message skipped", "msg_id", data.Message.ID)
			return
		}
	}

	// Prevent reply loops: skip messages sent by the page itself.
	if data.Message.SenderID == ch.pageID {
		slog.Info("pancake: skipping own page message",
			"page_id", ch.pageID,
			"sender_id", data.Message.SenderID)
		return
	}
	if isAssignedStaff(data.AssigneeIDs, data.Message.SenderID) {
		slog.Info("pancake: skipping assigned staff message",
			"page_id", ch.pageID,
			"sender_id", data.Message.SenderID,
			"conversation_id", data.ConversationID)
		return
	}

	if data.Message.SenderID == "" {
		slog.Warn("pancake: message missing sender_id, skipping", "msg_id", data.Message.ID)
		return
	}

	// Check echo BEFORE buildMessageContent adds the [From: ...] prefix.
	// rememberOutboundEcho stores the raw outbound text; the prefix would cause a
	// key mismatch and silently break loop detection.
	if ch.isRecentOutboundEcho(data.ConversationID, data.Message.Content) {
		slog.Info("pancake: skipping recent outbound echo",
			"page_id", ch.pageID,
			"conversation_id", data.ConversationID,
			"msg_id", data.Message.ID)
		return
	}

	content := buildMessageContent(data)

	metadata := map[string]string{
		"pancake_mode":      strings.ToLower(data.Type), // "inbox" or "comment"
		"conversation_type": data.Type,
		"platform":          data.Platform,
		"conversation_id":   data.ConversationID,
		"message_id":        dedupKey,
		"display_name":      channels.SanitizeDisplayName(data.Message.SenderName),
		"page_name":         ch.pageName,
	}

	ch.HandleMessage(
		data.Message.SenderID,
		data.ConversationID, // ChatID = conversation_id for reply routing
		content,
		nil, // media handled inline via content URLs
		metadata,
		"direct", // Pancake inbox conversations are always treated as direct messages
	)

	slog.Debug("pancake: inbound message published to bus",
		"page_id", ch.pageID,
		"conv_id", data.ConversationID,
		"sender_id", data.Message.SenderID,
		"platform", data.Platform,
		"type", data.Type,
		"channel_name", ch.Name(),
	)
}

// buildMessageContent combines text content and attachment URLs into a single string.
// Format: [From: {SenderID} ({SenderName})] {content}
func buildMessageContent(data MessagingData) string {
	parts := []string{}

	if data.Message.Content != "" {
		parts = append(parts, stripHTML(data.Message.Content))
	}

	for _, att := range data.Message.Attachments {
		if att.URL != "" {
			parts = append(parts, att.URL)
		}
	}

	body := strings.Join(parts, "\n")

	if data.Message.SenderID != "" {
		prefix := fmt.Sprintf("[From: %s (%s)]", data.Message.SenderID, data.Message.SenderName)
		if body != "" {
			return prefix + " " + body
		}
		return prefix
	}
	return body
}

// stripHTML removes HTML tags and unescapes HTML entities from s.
func stripHTML(s string) string {
	s = htmlTagRe.ReplaceAllString(s, "")
	return html.UnescapeString(strings.TrimSpace(s))
}

func isAssignedStaff(assigneeIDs []string, senderID string) bool {
	if senderID == "" {
		return false
	}
	return slices.Contains(assigneeIDs, senderID)
}
