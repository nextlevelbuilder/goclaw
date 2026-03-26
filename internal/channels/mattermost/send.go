package mattermost

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// Send delivers an outbound message to Mattermost.
func (c *Channel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("mattermost bot not running")
	}

	channelID := msg.ChatID
	if channelID == "" {
		return fmt.Errorf("empty chat ID for mattermost send")
	}

	placeholderKey := channelID
	if pk := msg.Metadata["placeholder_key"]; pk != "" {
		placeholderKey = pk
	}
	rootID := msg.Metadata["message_thread_id"]

	// Placeholder update (LLM retry notification)
	if msg.Metadata["placeholder_update"] == "true" {
		if pID, ok := c.placeholders.Load(placeholderKey); ok {
			postID := pID.(string)
			patch := &MMPostPatch{
				Message: strPtr(msg.Content),
			}
			_ = c.client.PatchPost(ctx, postID, patch)
		}
		return nil
	}

	content := stripSignalLines(msg.Content)

	// Extract inline ```attachment blocks from content
	content, inlineAtts := extractInlineAttachments(content)

	// Build attachments from metadata (or shorthand keys)
	metaAtts := buildAttachments(msg)

	// Merge all attachments
	allAtts := append(inlineAtts, metaAtts...)

	// NO_REPLY: delete placeholder, return
	if content == "" && len(allAtts) == 0 {
		if pID, ok := c.placeholders.Load(placeholderKey); ok {
			c.placeholders.Delete(placeholderKey)
			postID := pID.(string)
			_ = c.client.DeletePost(ctx, postID)
		}
		return nil
	}

	// Edit placeholder with first chunk, send rest as follow-ups
	if pID, ok := c.placeholders.Load(placeholderKey); ok {
		c.placeholders.Delete(placeholderKey)
		postID := pID.(string)

		editContent, remaining := splitAtLimit(content, maxMessageLen)

		patch := &MMPostPatch{
			Message: strPtr(editContent),
		}
		if err := c.client.PatchPost(ctx, postID, patch); err == nil {
			if remaining != "" {
				return c.sendChunked(ctx, channelID, remaining, rootID)
			}
			return nil
		}
		slog.Warn("mattermost placeholder edit failed, sending new message",
			"channel_id", channelID, "error", "patch failed")
	}

	// Handle media attachments
	for _, attachment := range msg.Media {
		if err := c.uploadFile(ctx, channelID, rootID, attachment); err != nil {
			slog.Warn("mattermost: file upload failed",
				"file", attachment.URL, "error", err)
			_ = c.sendChunked(ctx, channelID, fmt.Sprintf("[File upload failed: %s]", attachment.URL), rootID)
		}
	}

	// Send with rich attachments if present
	if len(allAtts) > 0 {
		return c.sendWithAttachments(ctx, channelID, content, rootID, allAtts)
	}

	return c.sendChunked(ctx, channelID, content, rootID)
}

// sendWithAttachments creates a post with Mattermost rich message attachments.
func (c *Channel) sendWithAttachments(ctx context.Context, channelID, content, rootID string, attachments []*MMMessageAttachment) error {
	post := &MMPost{
		ChannelId: channelID,
		Message:   content,
		RootId:    rootID,
	}
	post.setAttachments(attachments)

	if _, err := c.client.CreatePost(ctx, post); err != nil {
		return fmt.Errorf("send mattermost attachment message: %w", err)
	}
	return nil
}

// stripSignalLines removes internal "SIGNAL:" lines from outbound content
// and logs them. These are LLM/tool-system errors not meant for end users.
func stripSignalLines(content string) string {
	lines := strings.Split(content, "\n")
	var clean []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "SIGNAL:") {
			slog.Debug("mattermost: stripped internal signal from response", "signal", trimmed)
			continue
		}
		clean = append(clean, line)
	}
	return strings.TrimSpace(strings.Join(clean, "\n"))
}

// sendChunked splits a long message and sends each chunk as a separate post.
func (c *Channel) sendChunked(ctx context.Context, channelID, content, rootID string) error {
	for len(content) > 0 {
		chunk, rest := splitAtLimit(content, maxMessageLen)
		content = rest

		post := &MMPost{
			ChannelId: channelID,
			Message:   chunk,
			RootId:    rootID,
		}

		if _, err := c.client.CreatePost(ctx, post); err != nil {
			return fmt.Errorf("send mattermost message: %w", err)
		}
	}
	return nil
}

// splitAtLimit splits content at maxLen runes, preferring newline boundaries.
func splitAtLimit(content string, maxLen int) (chunk, remaining string) {
	runes := []rune(content)
	if len(runes) <= maxLen {
		return content, ""
	}
	cutAt := maxLen
	// Try to break at a newline in the second half
	candidate := string(runes[:maxLen])
	if idx := strings.LastIndex(candidate, "\n"); idx > len(candidate)/2 {
		return content[:idx+1], content[idx+1:]
	}
	return string(runes[:cutAt]), string(runes[cutAt:])
}

// strPtr returns a pointer to a string value.
func strPtr(s string) *string { return &s }
