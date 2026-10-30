package mattermost

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const reactionDebounceInterval = 700 * time.Millisecond

// statusEmoji maps agent status to Mattermost system emoji names.
var statusEmoji = map[string]string{
	"thinking": "thinking_face",
	"tool":     "hammer_and_wrench",
	"done":     "white_check_mark",
	"error":    "x",
	"stall":    "hourglass_flowing_sand",
}

type reactionState struct {
	currentEmoji string
	lastUpdate   time.Time
	mu           sync.Mutex
}

// OnReactionEvent sets a status emoji on the user's post, replacing the
// previous one.
func (c *Channel) OnReactionEvent(ctx context.Context, chatID string, messageID string, status string) error {
	if c.cfg.ReactionLevel == "" || c.cfg.ReactionLevel == "off" || messageID == "" {
		return nil
	}

	emoji, ok := statusEmoji[status]
	if !ok {
		return nil
	}
	if c.cfg.ReactionLevel == "minimal" && status != "thinking" && status != "done" {
		return nil
	}

	stateVal, _ := c.reactions.LoadOrStore(chatID+":"+messageID, &reactionState{})
	st := stateVal.(*reactionState)

	st.mu.Lock()
	defer st.mu.Unlock()

	if st.currentEmoji == emoji || time.Since(st.lastUpdate) < reactionDebounceInterval {
		return nil
	}

	if st.currentEmoji != "" {
		if err := c.api.removeReaction(ctx, c.botUserID, messageID, st.currentEmoji); err != nil {
			slog.Debug("mattermost: remove reaction failed", "emoji", st.currentEmoji, "error", err)
		}
	}
	if err := c.api.addReaction(ctx, c.botUserID, messageID, emoji); err != nil {
		slog.Debug("mattermost: add reaction failed", "emoji", emoji, "error", err)
		st.currentEmoji = ""
		return nil
	}

	st.currentEmoji = emoji
	st.lastUpdate = time.Now()
	return nil
}

// ClearReaction removes the current status emoji from a post.
func (c *Channel) ClearReaction(ctx context.Context, chatID string, messageID string) error {
	stateVal, ok := c.reactions.LoadAndDelete(chatID + ":" + messageID)
	if !ok {
		return nil
	}

	st := stateVal.(*reactionState)
	st.mu.Lock()
	defer st.mu.Unlock()

	if st.currentEmoji != "" {
		if err := c.api.removeReaction(ctx, c.botUserID, messageID, st.currentEmoji); err != nil {
			slog.Debug("mattermost: clear reaction failed", "emoji", st.currentEmoji, "error", err)
		}
	}
	return nil
}
