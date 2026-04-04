package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

// Reaction timing defaults (matching TS src/channels/status-reactions.ts).
const (
	reactionDebounceMs = 700 * time.Millisecond
	stallSoftMs        = 10 * time.Second
	stallHardMs        = 30 * time.Second
)

// telegramSupportedEmojis is the set of emoji reactions supported by Telegram.
// Source: Telegram Bot API docs + telego ReactionTypeEmoji.Emoji comment.
var telegramSupportedEmojis = map[string]bool{
	"❤": true, "👍": true, "👎": true, "🔥": true, "🥰": true, "👏": true,
	"😁": true, "🤔": true, "🤯": true, "😱": true, "🤬": true, "😢": true,
	"🎉": true, "🤩": true, "🤮": true, "💩": true, "🙏": true, "👌": true,
	"🕊": true, "🤡": true, "🥱": true, "🥴": true, "😍": true, "🐳": true,
	"❤\u200d🔥": true, "🌚": true, "🌭": true, "💯": true, "🤣": true, "⚡": true,
	"🍌": true, "🏆": true, "💔": true, "🤨": true, "😐": true, "🍓": true,
	"🍾": true, "💋": true, "🖕": true, "😈": true, "😴": true, "😭": true,
	"🤓": true, "👻": true, "👨\u200d💻": true, "👀": true, "🎃": true, "🙈": true,
	"😇": true, "😨": true, "🤝": true, "✍": true, "🤗": true, "🫡": true,
	"🎅": true, "🎄": true, "☃": true, "💅": true, "🤪": true, "🗿": true,
	"🆒": true, "💘": true, "🙉": true, "🦄": true, "😘": true, "💊": true,
	"🙊": true, "😎": true, "👾": true, "🤷\u200d♂": true, "🤷": true,
	"🤷\u200d♀": true, "😡": true,
}

// statusReactionVariants maps agent status to ordered emoji variants.
// First emoji is preferred; fallback to next if chat restricts it.
// Ref: TS src/telegram/status-reaction-variants.ts
var statusReactionVariants = map[string][]string{
	"queued":    {"👀", "🙏", "🤝"},
	"thinking":  {"🤔", "🤓", "👀"},
	"tool":      {"✍", "⚡", "🔥"},
	"coding":    {"👨\u200d💻", "✍", "🤓"},
	"web":       {"⚡", "🌚", "🔥"},
	"done":      {"👍", "🎉", "💯"},
	"error":     {"💔", "😱", "😨"},
	"stallSoft": {"🥱", "😴", "😐"},
	"stallHard": {"😨", "🤯", "😱"},
}

// ReactionLegend returns the primary emoji for each status, used by the /reactions command.
var reactionLegend = []struct {
	Status string
	Emoji  string
	Desc   string
}{
	{"queued", "👀", "Queued — waiting to process"},
	{"thinking", "🤔", "Thinking — processing your request"},
	{"tool", "✍", "Tool — executing a tool"},
	{"coding", "👨\u200d💻", "Coding — running code"},
	{"web", "⚡", "Web — browsing / API call"},
	{"done", "👍", "Done — completed"},
	{"error", "💔", "Error — something went wrong"},
	{"stallSoft", "🥱", "Stall — no activity for 10s"},
	{"stallHard", "😨", "Stall — no activity for 30s"},
}

// resolveReactionEmoji picks the first supported emoji for a given status.
// If allowedEmojis is non-nil, only emojis in that set are considered.
// Falls back through variants until a match is found.
func resolveReactionEmoji(status string, allowedEmojis map[string]bool) string {
	variants, ok := statusReactionVariants[status]
	if !ok {
		return ""
	}

	for _, emoji := range variants {
		if !telegramSupportedEmojis[emoji] {
			continue
		}
		if allowedEmojis != nil && !allowedEmojis[emoji] {
			continue
		}
		return emoji
	}
	return ""
}

// StatusReactionController manages status emoji reactions on a user message.
// It debounces intermediate states and detects stalls.
// Ref: TS src/channels/status-reactions.ts → StatusReactionController
type StatusReactionController struct {
	bot       *telego.Bot
	chatID    int64
	messageID int

	mu           sync.Mutex
	currentEmoji string
	lastStatus   string
	terminal     bool // true once done/error is set
	debounceTimer *time.Timer
	stallTimer    *time.Timer
}

// newStatusReactionController creates a controller for a specific message.
func newStatusReactionController(bot *telego.Bot, chatID int64, messageID int) *StatusReactionController {
	return &StatusReactionController{
		bot:       bot,
		chatID:    chatID,
		messageID: messageID,
	}
}

// SetStatus updates the reaction emoji based on agent status.
// Intermediate states (thinking, tool) are debounced. Terminal states (done, error) are immediate.
func (rc *StatusReactionController) SetStatus(ctx context.Context, status string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if rc.terminal {
		return
	}

	rc.lastStatus = status

	// Reset stall timer on any activity
	rc.resetStallTimer(ctx)

	// Terminal states: apply immediately
	if status == "done" || status == "error" {
		rc.terminal = true
		rc.cancelDebounce()
		rc.cancelStall()
		emoji := resolveReactionEmoji(status, nil)
		if emoji != "" {
			rc.applyReaction(ctx, emoji)
		}
		return
	}

	// Intermediate states: debounce
	rc.cancelDebounce()
	rc.debounceTimer = time.AfterFunc(reactionDebounceMs, func() {
		rc.mu.Lock()
		defer rc.mu.Unlock()

		if rc.terminal {
			return
		}

		emoji := resolveReactionEmoji(rc.lastStatus, nil)
		if emoji != "" {
			rc.applyReaction(context.Background(), emoji)
		}
	})
}

// Stop cancels all timers. Call when the reaction controller is no longer needed.
func (rc *StatusReactionController) Stop() {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	rc.cancelDebounce()
	rc.cancelStall()
}

// applyReaction sets the emoji reaction on the message (must hold mu lock).
func (rc *StatusReactionController) applyReaction(ctx context.Context, emoji string) {
	if emoji == rc.currentEmoji {
		return
	}

	var reactions []telego.ReactionType
	if emoji != "" {
		reactions = []telego.ReactionType{
			&telego.ReactionTypeEmoji{
				Type:  telego.ReactionEmoji,
				Emoji: emoji,
			},
		}
	}

	if err := rc.bot.SetMessageReaction(ctx, &telego.SetMessageReactionParams{
		ChatID:    tu.ID(rc.chatID),
		MessageID: rc.messageID,
		Reaction:  reactions,
	}); err != nil {
		slog.Debug("reaction: failed to set", "emoji", emoji, "chat_id", rc.chatID, "error", err)
		return
	}

	rc.currentEmoji = emoji
}

// resetStallTimer resets the stall detection timer (must hold mu lock).
func (rc *StatusReactionController) resetStallTimer(_ context.Context) {
	rc.cancelStall()

	rc.stallTimer = time.AfterFunc(stallSoftMs, func() {
		rc.mu.Lock()
		defer rc.mu.Unlock()

		if rc.terminal {
			return
		}

		emoji := resolveReactionEmoji("stallSoft", nil)
		if emoji != "" {
			rc.applyReaction(context.Background(), emoji)
		}

		// Schedule hard stall
		rc.stallTimer = time.AfterFunc(stallHardMs-stallSoftMs, func() {
			rc.mu.Lock()
			defer rc.mu.Unlock()

			if rc.terminal {
				return
			}

			emoji := resolveReactionEmoji("stallHard", nil)
			if emoji != "" {
				rc.applyReaction(context.Background(), emoji)
			}
		})
	})
}

// cancelDebounce cancels the debounce timer (must hold mu lock).
func (rc *StatusReactionController) cancelDebounce() {
	if rc.debounceTimer != nil {
		rc.debounceTimer.Stop()
		rc.debounceTimer = nil
	}
}

// cancelStall cancels the stall timer (must hold mu lock).
func (rc *StatusReactionController) cancelStall() {
	if rc.stallTimer != nil {
		rc.stallTimer.Stop()
		rc.stallTimer = nil
	}
}

// --- ReactionChannel implementation ---

// OnReactionEvent handles agent status change events and updates the reaction emoji.
// messageID is the original user message that triggered the agent run (string for cross-platform compat).
// chatID here is the localKey (composite key with :topic:N suffix for forum topics).
func (c *Channel) OnReactionEvent(ctx context.Context, chatID string, messageID string, status string) error {
	if c.config.ReactionLevel == "" || c.config.ReactionLevel == "off" {
		return nil
	}

	// Minimal mode: only show terminal reactions (done/error), skip intermediate statuses.
	if c.config.ReactionLevel == "minimal" && status != "done" && status != "error" {
		return nil
	}

	msgID, err := strconv.Atoi(messageID)
	if err != nil || msgID == 0 {
		return nil // not a Telegram message ID
	}

	id, err := parseRawChatID(chatID)
	if err != nil {
		return err
	}

	// Get or create reaction controller for this message.
	// Key by messageID so concurrent runs in the same chat don't clash.
	key := fmt.Sprintf("%s:%s", chatID, messageID)
	val, _ := c.reactions.LoadOrStore(key, newStatusReactionController(c.bot, id, msgID))
	rc, ok := val.(*StatusReactionController)
	if !ok {
		return nil
	}

	rc.SetStatus(ctx, status)

	// Clean up controller on terminal states
	if status == "done" || status == "error" {
		c.reactions.Delete(key)
	}

	return nil
}

// ClearReaction removes the reaction from a message.
func (c *Channel) ClearReaction(ctx context.Context, chatID string, messageID string) error {
	msgID, err := strconv.Atoi(messageID)
	if err != nil || msgID == 0 {
		return nil
	}

	id, err := parseRawChatID(chatID)
	if err != nil {
		return err
	}

	// Stop and remove controller if exists
	key := fmt.Sprintf("%s:%s", chatID, messageID)
	if val, ok := c.reactions.LoadAndDelete(key); ok {
		if rc, ok := val.(*StatusReactionController); ok {
			rc.Stop()
		}
	}

	// Clear reaction on the message
	return c.bot.SetMessageReaction(ctx, &telego.SetMessageReactionParams{
		ChatID:    tu.ID(id),
		MessageID: msgID,
		Reaction:  []telego.ReactionType{},
	})
}
