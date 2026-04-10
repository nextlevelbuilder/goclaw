package facebook

import (
	"fmt"
	"log/slog"
	"strings"
)

// handleCommentEvent processes a feed webhook change where item == "comment".
func (ch *Channel) handleCommentEvent(entry WebhookEntry, change ChangeValue) {
	// Feature gate.
	if !ch.config.Features.CommentReply {
		return
	}

	// Only process new comments (not edits or deletions).
	if change.Verb != "add" {
		return
	}

	// Page routing: ensure this event belongs to our page instance (before dedup write).
	if entry.ID != ch.pageID {
		return
	}

	// Self-reply prevention: skip comments posted by the page itself.
	if change.From.ID == ch.pageID {
		return
	}

	// Dedup: Facebook may deliver the same event more than once.
	if ch.isDup("comment:" + change.CommentID) {
		slog.Debug("facebook: duplicate comment event skipped", "comment_id", change.CommentID)
		return
	}

	// Build message content — optionally enriched with post + thread context.
	// Use stopCtx so inflight Graph API calls are cancelled when the channel stops.
	content := change.Message
	if ch.config.CommentReplyOptions.IncludePostContext && change.PostID != "" {
		content = ch.buildEnrichedContent(change)
	}

	senderID := change.From.ID
	// Session key groups all comments by the same user on the same post.
	chatID := fmt.Sprintf("%s:%s", change.PostID, senderID)

	metadata := map[string]string{
		"comment_id":          change.CommentID,
		"post_id":             change.PostID,
		"parent_id":           change.ParentID,
		"sender_name":         change.From.Name,
		"sender_id":           senderID,
		"fb_mode":             "comment",
		"reply_to_comment_id": change.CommentID,
	}

	ch.HandleMessage(senderID, chatID, content, nil, metadata, "direct")
}

// buildEnrichedContent fetches post content and comment thread, assembles a rich
// context string for the agent. Uses stopCtx so it is cancelled on channel Stop().
func (ch *Channel) buildEnrichedContent(change ChangeValue) string {
	ctx := ch.stopCtx

	var sb strings.Builder

	// Post content.
	post, err := ch.postFetcher.GetPost(ctx, change.PostID)
	if err == nil && post != nil && post.Message != "" {
		sb.WriteString("[Bài đăng] ")
		sb.WriteString(post.Message)
		sb.WriteString("\n\n")
	}

	// Comment thread (when this is a nested reply).
	if change.ParentID != "" {
		depth := ch.config.CommentReplyOptions.MaxThreadDepth
		if depth <= 0 {
			depth = 10
		}
		thread, err := ch.postFetcher.GetCommentThread(ctx, change.ParentID, depth)
		if err == nil && len(thread) > 0 {
			sb.WriteString("[Thread]\n")
			for _, c := range thread {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", c.From.Name, c.Message))
			}
			sb.WriteString("\n")
		}
	}

	// Current comment.
	sb.WriteString("[Comment mới] ")
	sb.WriteString(change.From.Name)
	sb.WriteString(": ")
	sb.WriteString(change.Message)

	return sb.String()
}
