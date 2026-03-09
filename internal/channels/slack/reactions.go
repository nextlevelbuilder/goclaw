package slack

import (
	"context"
	"log/slog"
	"sync"
	"time"

	slackapi "github.com/slack-go/slack"
)

const reactionDebounceInterval = 700 * time.Millisecond

// statusEmoji maps GoClaw agent status to Slack emoji names (without colons).
var statusEmoji = map[string]string{
	"thinking": "thinking_face",
	"tool":     "hammer_and_wrench",
	"done":     "white_check_mark",
	"error":    "x",
	"stall":    "hourglass_flowing_sand",
}

// reactionState tracks per-message reaction state.
type reactionState struct {
	currentEmoji string
	lastUpdate   time.Time
	mu           sync.Mutex
}

// OnReactionEvent adds a status emoji reaction to the user's message.
func (c *Channel) OnReactionEvent(_ context.Context, chatID string, messageID string, status string) error {
	if c.config.ReactionLevel == "" || c.config.ReactionLevel == "off" {
		return nil
	}

	emoji, ok := statusEmoji[status]
	if !ok {
		return nil
	}

	// For "minimal" level, only show thinking and done
	if c.config.ReactionLevel == "minimal" && status != "thinking" && status != "done" {
		return nil
	}

	channelID := extractChannelID(chatID)

	stateKey := chatID + ":" + messageID
	stateVal, _ := c.reactions.LoadOrStore(stateKey, &reactionState{})
	st := stateVal.(*reactionState)

	st.mu.Lock()
	defer st.mu.Unlock()

	if time.Since(st.lastUpdate) < reactionDebounceInterval {
		return nil
	}

	// Remove previous reaction (if different)
	if st.currentEmoji != "" && st.currentEmoji != emoji {
		if err := c.api.RemoveReaction(st.currentEmoji,
			slackapi.ItemRef{Channel: channelID, Timestamp: messageID}); err != nil {
			slog.Debug("slack: remove reaction failed", "emoji", st.currentEmoji, "error", err)
		}
	}

	if err := c.api.AddReaction(emoji,
		slackapi.ItemRef{Channel: channelID, Timestamp: messageID}); err != nil {
		slog.Debug("slack: add reaction failed", "emoji", emoji, "error", err)
		return nil
	}

	st.currentEmoji = emoji
	st.lastUpdate = time.Now()
	return nil
}

// ClearReaction removes the current status emoji from a message.
func (c *Channel) ClearReaction(_ context.Context, chatID string, messageID string) error {
	stateKey := chatID + ":" + messageID
	stateVal, ok := c.reactions.LoadAndDelete(stateKey)
	if !ok {
		return nil
	}

	st := stateVal.(*reactionState)
	st.mu.Lock()
	defer st.mu.Unlock()

	if st.currentEmoji != "" {
		channelID := extractChannelID(chatID)
		if err := c.api.RemoveReaction(st.currentEmoji,
			slackapi.ItemRef{Channel: channelID, Timestamp: messageID}); err != nil {
			slog.Debug("slack: clear reaction failed", "emoji", st.currentEmoji, "error", err)
		}
	}

	return nil
}
