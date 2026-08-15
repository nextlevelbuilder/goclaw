package bitrix24

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// Compile-time guarantee the Bitrix24 channel provides status reactions.
var _ channels.ReactionChannel = (*Channel)(nil)

// reactionDebounceInterval throttles intermediate status-reaction updates so a
// fast think→tool→tool sequence doesn't hammer the REST API. Terminal states
// (done/error) bypass the debounce so the final outcome always lands.
const reactionDebounceInterval = 700 * time.Millisecond

// statusReaction maps a GoClaw agent status to a Bitrix24 v2 reaction code.
// Codes come from imbot.v2.Chat.Message.Reaction.add's documented set.
var statusReaction = map[string]string{
	"queued":    "eyes",                // 👀 waiting to process
	"thinking":  "thinkingFace",        // 🤔 processing
	"tool":      "fire",                // 🔥 running a tool
	"coding":    "fire",                // 🔥 executing code
	"web":       "fire",                // 🔥 browsing / API call
	"done":      "whiteHeavyCheckMark", // ✅ completed
	"error":     "crossMark",           // ❌ failed
	"stall":     "sleepingSymbol",      // 😴 no activity
	"stallSoft": "sleepingSymbol",      // 😴
	"stallHard": "flushedFace",         // 😳
}

// reactionState tracks the current reaction code set on one user message so
// OnReactionEvent can replace it as the agent's status progresses.
type reactionState struct {
	mu         sync.Mutex
	current    string
	lastUpdate time.Time
}

// OnReactionEvent sets a status-reaction on the user's message to show agent
// progress (thinking → tool → done/error). Implements channels.ReactionChannel.
// messageID is the Bitrix MESSAGE_ID (numeric) of the message that started the run.
func (c *Channel) OnReactionEvent(ctx context.Context, chatID, messageID, status string) error {
	level := c.cfg.ReactionLevel
	if level == "" || level == "off" {
		return nil
	}
	code, ok := statusReaction[status]
	if !ok {
		return nil
	}
	terminal := status == "done" || status == "error"
	// "minimal" shows only the final outcome (done/error), not intermediate steps.
	if level == "minimal" && !terminal {
		return nil
	}

	msgID, err := strconv.Atoi(strings.TrimSpace(messageID))
	if err != nil || msgID <= 0 {
		return nil // not a Bitrix message id (e.g. connector synthetic id)
	}
	botID := c.BotID()
	if botID <= 0 || c.Client() == nil {
		return nil
	}

	key := chatID + ":" + messageID
	stVal, _ := c.reactions.LoadOrStore(key, &reactionState{})
	st := stVal.(*reactionState)

	st.mu.Lock()
	defer st.mu.Unlock()

	if !terminal && time.Since(st.lastUpdate) < reactionDebounceInterval {
		return nil
	}
	if st.current == code {
		return nil
	}

	// Bitrix has no "replace" — delete the previous reaction, then add the new.
	if st.current != "" {
		c.removeReaction(ctx, botID, msgID, st.current)
	}
	if err := c.addReaction(ctx, botID, msgID, code); err != nil {
		slog.Debug("bitrix24 reaction: add failed",
			"code", code, "message_id", msgID, "err", err)
		return nil
	}
	st.current = code
	st.lastUpdate = time.Now()

	// The run is over on terminal states — stop tracking this message.
	if terminal {
		c.reactions.Delete(key)
	}
	return nil
}

// ClearReaction removes the current status-reaction from a message.
// Implements channels.ReactionChannel.
func (c *Channel) ClearReaction(ctx context.Context, chatID, messageID string) error {
	key := chatID + ":" + messageID
	stVal, ok := c.reactions.LoadAndDelete(key)
	if !ok {
		return nil
	}
	st := stVal.(*reactionState)
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.current == "" {
		return nil
	}
	msgID, err := strconv.Atoi(strings.TrimSpace(messageID))
	if err != nil || msgID <= 0 {
		return nil
	}
	if botID := c.BotID(); botID > 0 && c.Client() != nil {
		c.removeReaction(ctx, botID, msgID, st.current)
	}
	st.current = ""
	return nil
}

// addReaction adds a bot reaction via imbot.v2.Chat.Message.Reaction.add.
// REACTION_ALREADY_SET is treated as success (the reaction is already present).
func (c *Channel) addReaction(ctx context.Context, botID, messageID int, code string) error {
	_, err := c.Client().Call(ctx, "imbot.v2.Chat.Message.Reaction.add", map[string]any{
		"botId":     botID,
		"messageId": messageID,
		"reaction":  code,
	})
	if err != nil && isReactionAlreadySet(err) {
		return nil
	}
	return err
}

// removeReaction removes a bot reaction via imbot.v2.Chat.Message.Reaction.delete.
// Best-effort: a failed delete is logged and swallowed (worst case a stale
// reaction lingers, which is cosmetic).
func (c *Channel) removeReaction(ctx context.Context, botID, messageID int, code string) {
	if _, err := c.Client().Call(ctx, "imbot.v2.Chat.Message.Reaction.delete", map[string]any{
		"botId":     botID,
		"messageId": messageID,
		"reaction":  code,
	}); err != nil {
		slog.Debug("bitrix24 reaction: delete failed",
			"code", code, "message_id", messageID, "err", err)
	}
}

// isReactionAlreadySet reports whether err is the benign REACTION_ALREADY_SET
// application error from Bitrix (the bot already reacted with this code).
func isReactionAlreadySet(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code == "REACTION_ALREADY_SET"
	}
	return false
}
