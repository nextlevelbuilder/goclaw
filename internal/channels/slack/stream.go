package slack

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	slackapi "github.com/slack-go/slack"
)

const streamThrottleInterval = 1000 * time.Millisecond

// streamState tracks per-chat streaming state.
type streamState struct {
	channelID  string
	threadTS   string
	msgTS      string    // placeholder message timestamp
	lastUpdate time.Time // last chat.update call
	mu         sync.Mutex
}

// StreamEnabled reports whether streaming is active for DMs or groups.
func (c *Channel) StreamEnabled(isGroup bool) bool {
	if isGroup {
		return c.config.GroupStream != nil && *c.config.GroupStream
	}
	return c.config.DMStream != nil && *c.config.DMStream
}

// OnStreamStart is called when a new agent run starts.
// The placeholder "Thinking..." was already sent in handleMessage.
func (c *Channel) OnStreamStart(_ context.Context, _ string) error {
	return nil
}

// OnChunkEvent edits the placeholder with accumulated text, throttled to avoid rate limits.
func (c *Channel) OnChunkEvent(_ context.Context, chatID string, fullText string) error {
	stateVal, ok := c.streams.Load(chatID)
	if !ok {
		pTS, pOK := c.placeholders.Load(chatID)
		if !pOK {
			return nil
		}

		st := &streamState{
			channelID: extractChannelID(chatID),
			msgTS:     pTS.(string),
		}
		if threadTS := extractThreadTS(chatID); threadTS != "" {
			st.threadTS = threadTS
		}
		c.streams.Store(chatID, st)
		stateVal = st
	}

	st := stateVal.(*streamState)
	st.mu.Lock()
	defer st.mu.Unlock()

	if time.Since(st.lastUpdate) < streamThrottleInterval {
		return nil
	}

	formatted := markdownToSlackMrkdwn(fullText)
	if len(formatted) > maxMessageLen {
		formatted = formatted[:maxMessageLen] + "..."
	}

	opts := []slackapi.MsgOption{slackapi.MsgOptionText(formatted, false)}
	_, _, _, err := c.api.UpdateMessage(st.channelID, st.msgTS, opts...)
	if err != nil {
		slog.Debug("slack stream chunk update failed", "error", err)
		return nil
	}

	st.lastUpdate = time.Now()
	return nil
}

// OnStreamEnd sends final formatted text.
func (c *Channel) OnStreamEnd(_ context.Context, chatID string, finalText string) error {
	if finalText == "" {
		c.streams.Delete(chatID)
		return nil
	}

	channelID := extractChannelID(chatID)
	var msgTS, threadTS string

	if stateVal, ok := c.streams.Load(chatID); ok {
		st := stateVal.(*streamState)
		msgTS = st.msgTS
		threadTS = st.threadTS
	} else if pTS, ok := c.placeholders.Load(chatID); ok {
		msgTS = pTS.(string)
	}

	defer c.streams.Delete(chatID)

	if msgTS == "" {
		return nil
	}

	formatted := markdownToSlackMrkdwn(finalText)
	if len(formatted) <= maxMessageLen {
		_, _, _, err := c.api.UpdateMessage(channelID, msgTS, slackapi.MsgOptionText(formatted, false))
		return err
	}

	// Final text exceeds limit -- update placeholder with first chunk, send rest as follow-ups
	first, rest := splitAtLimit(formatted, maxMessageLen)
	_, _, _, _ = c.api.UpdateMessage(channelID, msgTS, slackapi.MsgOptionText(first, false))
	return c.sendChunked(channelID, rest, threadTS)
}

// extractChannelID gets the channel ID from a local_key.
func extractChannelID(localKey string) string {
	if idx := strings.Index(localKey, ":thread:"); idx > 0 {
		return localKey[:idx]
	}
	return localKey
}

// extractThreadTS gets the thread_ts from a local_key, or "" if not threaded.
func extractThreadTS(localKey string) string {
	const prefix = ":thread:"
	if idx := strings.Index(localKey, prefix); idx > 0 {
		return localKey[idx+len(prefix):]
	}
	return ""
}
