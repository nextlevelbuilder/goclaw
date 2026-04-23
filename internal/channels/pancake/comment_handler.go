package pancake

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// handleCommentEvent processes a Pancake COMMENT webhook event.
// Mirrors the inbox handler pattern with additional comment-specific guards.
func (ch *Channel) handleCommentEvent(data MessagingData) {
	// Feature gate — exit only if BOTH reply and auto-react are disabled.
	if !ch.config.Features.CommentReply && !ch.config.Features.AutoReact {
		ch.commentReplyDisabledOnce.Do(func() {
			slog.Info("pancake: comment ignored because comment_reply and auto_react are both disabled",
				"page_id", ch.pageID,
				"channel_name", ch.Name(),
				"hint", "enable config.features.comment_reply or config.features.auto_react")
		})
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

	// Auto-react BEFORE keyword filter — fires on all valid non-duplicate comments.
	// Independent of comment_reply: reacts even if reply is disabled.
	if ch.config.Features.AutoReact && ch.platform == "facebook" && data.Message.ID != "" {
		if filterAutoReact(&ch.config, data.PostID, data.Message.SenderID) {
			select {
			case ch.reactSem <- struct{}{}:
				go ch.reactCommentAsync(data.ConversationID, data.Message.ID)
			default:
				slog.Debug("pancake: react semaphore full, dropping reaction",
					"page_id", ch.pageID, "comment_id", data.Message.ID)
			}
		} else {
			// Rollout-phase Info log for misconfiguration diagnosis.
			// TODO: downgrade to slog.Debug after ~2 weeks post-release.
			slog.Info("pancake: auto-react filtered by allow/deny list",
				"page_id", ch.pageID,
				"comment_id", data.Message.ID,
				"post_id", data.PostID,
				"sender_id", data.Message.SenderID)
		}
	}

	// Comment reply gate — independent of auto_react above.
	if !ch.config.Features.CommentReply {
		return
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

// reactCommentAsync likes the comment on Facebook asynchronously.
// Respects channel shutdown via stopCtx with 5s cap; releases the semaphore slot on exit.
func (ch *Channel) reactCommentAsync(conversationID, messageID string) {
	defer func() { <-ch.reactSem }()

	ctx, cancel := context.WithTimeout(ch.stopCtx, 5*time.Second)
	defer cancel()

	if err := ch.apiClient.ReactComment(ctx, conversationID, messageID); err != nil {
		slog.Warn("pancake: auto-react comment failed",
			"comment_id", messageID, "conv_id", conversationID, "page_id", ch.pageID, "err", err)
		return
	}
	slog.Debug("pancake: auto-reacted to comment",
		"comment_id", messageID, "page_id", ch.pageID)
}

// filterAutoReact returns true when allow/deny lists permit reacting.
// Only called after the fast-path gate (Features.AutoReact, platform, msgID) passes.
// Nil AutoReactOptions = no scope filter = allow.
// Deny lists evaluated before allow lists.
func filterAutoReact(cfg *pancakeInstanceConfig, postID, senderID string) bool {
	opts := cfg.AutoReactOptions
	if opts == nil {
		return true
	}
	if containsString(opts.DenyUserIDs, senderID) {
		return false
	}
	if containsString(opts.DenyPostIDs, postID) {
		return false
	}
	if len(opts.AllowUserIDs) > 0 && !containsString(opts.AllowUserIDs, senderID) {
		return false
	}
	if len(opts.AllowPostIDs) > 0 && !containsString(opts.AllowPostIDs, postID) {
		return false
	}
	return true
}

// containsString checks if target is present in ss after whitespace trim.
// Returns false on empty target to prevent false-positive match on blank sender.
func containsString(ss []string, target string) bool {
	if target == "" {
		return false
	}
	for _, s := range ss {
		if strings.TrimSpace(s) == target {
			return true
		}
	}
	return false
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
