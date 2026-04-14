package pancake

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// handleCommentEvent processes a Pancake COMMENT webhook event.
// Mirrors the inbox handler pattern with additional comment-specific guards.
func (ch *Channel) handleCommentEvent(data MessagingData) {
	// Feature gate.
	if !ch.config.Features.CommentReply {
		return
	}

	// Self-reply prevention: skip messages from the page itself.
	if data.Message.SenderID == ch.pageID {
		slog.Debug("pancake: skipping own page comment",
			"page_id", ch.pageID, "sender_id", data.Message.SenderID)
		return
	}

	// Skip assigned staff comments.
	if isAssignedStaff(data.AssigneeIDs, data.Message.SenderID) {
		slog.Debug("pancake: skipping assigned staff comment",
			"page_id", ch.pageID, "sender_id", data.Message.SenderID)
		return
	}

	if data.Message.SenderID == "" {
		slog.Warn("pancake: comment missing sender_id", "msg_id", data.Message.ID)
		return
	}

	if data.ConversationID == "" {
		slog.Warn("pancake: comment missing conversation_id", "msg_id", data.Message.ID)
		return
	}

	// Dedup by message ID (skip when empty to avoid shared slot).
	var dedupKey string
	if data.Message.ID != "" {
		dedupKey = fmt.Sprintf("comment:%s", data.Message.ID)
		if ch.isDup(dedupKey) {
			slog.Debug("pancake: duplicate comment skipped", "msg_id", data.Message.ID)
			return
		}
	}

	// Comment filter.
	if !ch.filterComment(data.Message.Content) {
		slog.Debug("pancake: comment filtered out",
			"page_id", ch.pageID, "msg_id", data.Message.ID)
		return
	}

	// Echo check before content enrichment.
	if ch.isRecentOutboundEcho(data.ConversationID, data.Message.Content) {
		slog.Debug("pancake: skipping comment outbound echo",
			"page_id", ch.pageID, "msg_id", data.Message.ID)
		return
	}

	// Build content — optionally enriched with post context.
	content := ch.buildCommentContent(data)

	metadata := map[string]string{
		"pancake_mode":        "comment",
		"conversation_type":   data.Type,
		"reply_to_comment_id": data.Message.ID,
		"sender_id":           data.Message.SenderID,
		"platform":            data.Platform,
		"conversation_id":     data.ConversationID,
		"message_id":          dedupKey,
		"display_name":        channels.SanitizeDisplayName(data.Message.SenderName),
		"page_name":           ch.pageName,
	}
	if data.PostID != "" {
		metadata["post_id"] = data.PostID
	}

	// ChatID = ConversationID: Pancake groups COMMENT conversations per sender per post.
	ch.HandleMessage(
		data.Message.SenderID,
		data.ConversationID,
		content,
		nil,
		metadata,
		"direct",
	)

	slog.Debug("pancake: comment event published to bus",
		"page_id", ch.pageID,
		"conv_id", data.ConversationID,
		"sender_id", data.Message.SenderID,
		"platform", data.Platform,
	)
}

// buildCommentContent assembles the comment content, optionally enriched with post context.
// Uses display name only (no senderID in content — senderID stays in metadata).
func (ch *Channel) buildCommentContent(data MessagingData) string {
	commentText := stripHTML(data.Message.Content)
	senderName := channels.SanitizeDisplayName(data.Message.SenderName)
	senderPrefix := fmt.Sprintf("[From: %s]", senderName)

	if !ch.config.CommentReplyOptions.IncludePostContext || ch.postFetcher == nil {
		if commentText != "" {
			return senderPrefix + " " + commentText
		}
		return senderPrefix
	}

	var sb strings.Builder

	// Fetch post context best-effort — on failure, fall back to comment text only.
	if data.PostID != "" {
		post, err := ch.postFetcher.GetPost(ch.stopCtx, data.PostID)
		if err != nil {
			slog.Debug("pancake: post context fetch failed, using comment only",
				"page_id", ch.pageID, "post_id", data.PostID, "err", err)
		}
		if err == nil && post != nil && post.Message != "" {
			sb.WriteString("[Bai dang] ")
			sb.WriteString(post.Message)
			sb.WriteString("\n\n")
		}
	}

	sb.WriteString("[Comment moi] ")
	sb.WriteString(senderPrefix)
	if commentText != "" {
		sb.WriteString(" ")
		sb.WriteString(commentText)
	}

	return sb.String()
}

// filterComment checks if the comment matches the configured filter.
// Returns true if the comment should be processed.
func (ch *Channel) filterComment(content string) bool {
	switch ch.config.CommentReplyOptions.Filter {
	case "keyword":
		if len(ch.config.CommentReplyOptions.Keywords) == 0 {
			// No keywords configured = block all (safe default).
			slog.Warn("pancake: keyword filter active but no keywords configured, blocking all comments",
				"page_id", ch.pageID)
			return false
		}
		lower := strings.ToLower(content)
		for _, kw := range ch.config.CommentReplyOptions.Keywords {
			if strings.Contains(lower, strings.ToLower(kw)) {
				return true
			}
		}
		return false
	default: // "all" or empty — process all comments
		return true
	}
}
