package mattermost

import (
	"context"
	"log/slog"
	"time"
)

// reactionState tracks the current emoji reaction on a specific post.
type reactionState struct {
	emoji string // current emoji name (without colons)
}

// statusEmoji maps agent status to Mattermost emoji names.
var statusEmoji = map[string]string{
	"thinking":  "hourglass_flowing_sand",
	"tool_call": "wrench",
	"done":      "white_check_mark",
	"error":     "x",
	"stall":     "warning",
}

// OnReactionEvent adds a status emoji reaction to a post and manages typing indicators.
func (c *Channel) OnReactionEvent(ctx context.Context, chatID string, messageID string, status string) error {
	// Typing indicator: start on thinking/tool, stop on done/error
	switch status {
	case "thinking", "tool", "web", "coding":
		c.startTyping(chatID)
	case "done", "error":
		c.stopTyping(chatID)
	}

	if c.config.ReactionLevel == "off" || c.config.ReactionLevel == "" {
		return nil
	}

	emoji, ok := statusEmoji[status]
	if !ok {
		return nil
	}

	reactionKey := chatID + ":" + messageID

	// Clear previous reaction first
	if prev, ok := c.reactions.Load(reactionKey); ok {
		prevState := prev.(*reactionState)
		if prevState.emoji == emoji {
			return nil // same reaction, skip
		}
		c.removeReaction(ctx, messageID, prevState.emoji)
	}

	// Add new reaction
	reaction := &MMReaction{
		UserId:    c.botUserID,
		PostId:    messageID,
		EmojiName: emoji,
	}
	if err := c.client.SaveReaction(ctx, reaction); err != nil {
		slog.Debug("mattermost: failed to add reaction",
			"post_id", messageID, "emoji", emoji, "error", err)
		return err
	}

	c.reactions.Store(reactionKey, &reactionState{emoji: emoji})
	return nil
}

// ClearReaction removes all status reactions from a post.
func (c *Channel) ClearReaction(ctx context.Context, chatID string, messageID string) error {
	reactionKey := chatID + ":" + messageID

	if prev, ok := c.reactions.LoadAndDelete(reactionKey); ok {
		prevState := prev.(*reactionState)
		c.removeReaction(ctx, messageID, prevState.emoji)
	}

	return nil
}

// removeReaction removes a specific emoji reaction from a post.
func (c *Channel) removeReaction(ctx context.Context, postID, emoji string) {
	if err := c.client.DeleteReaction(ctx, c.botUserID, postID, emoji); err != nil {
		slog.Debug("mattermost: failed to remove reaction",
			"post_id", postID, "emoji", emoji, "error", err)
	}
}

// startTyping starts a periodic typing indicator for the given channel.
// Mattermost typing indicators expire after ~6s, so we refresh every 4s.
func (c *Channel) startTyping(channelID string) {
	// Cancel any existing typing goroutine for this channel
	c.stopTyping(channelID)

	ctx, cancel := context.WithCancel(context.Background())
	c.typingCancels.Store(channelID, cancel)

	go func() {
		// Send immediately
		c.sendTyping(channelID)

		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.sendTyping(channelID)
			}
		}
	}()
}

// stopTyping cancels the periodic typing indicator for the given channel.
func (c *Channel) stopTyping(channelID string) {
	if cancelVal, ok := c.typingCancels.LoadAndDelete(channelID); ok {
		cancelVal.(context.CancelFunc)()
	}
}

// sendTyping sends a single typing indicator via the Mattermost REST API.
func (c *Channel) sendTyping(channelID string) {
	if err := c.client.PostTyping(context.Background(), c.botUserID, channelID); err != nil {
		slog.Debug("mattermost: typing indicator failed", "channel_id", channelID, "error", err)
	}
}
