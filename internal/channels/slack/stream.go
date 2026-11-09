package slack

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	slackapi "github.com/slack-go/slack"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

const streamThrottleInterval = 1000 * time.Millisecond

// slackStream implements channels.ChannelStream for Slack.
// It edits the placeholder "Thinking..." message as chunks arrive.
type slackStream struct {
	api        *slackapi.Client
	channelID  string
	threadTS   string
	msgTS      string    // placeholder message timestamp
	key        string    // c.placeholders key the message is registered under
	lastUpdate time.Time // last chat.update call
	mu         sync.Mutex
}

// Update edits the placeholder with accumulated text, throttled to avoid rate limits.
func (s *slackStream) Update(_ context.Context, fullText string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if time.Since(s.lastUpdate) < streamThrottleInterval {
		return
	}

	formatted := markdownToSlackMrkdwn(fullText)
	if len(formatted) > maxMessageLen {
		formatted = formatted[:maxMessageLen] + "..."
	}

	opts := []slackapi.MsgOption{slackapi.MsgOptionText(formatted, false)}
	_, _, _, err := s.api.UpdateMessage(s.channelID, s.msgTS, opts...)
	if err != nil {
		slog.Debug("slack stream chunk update failed", "error", err)
		return
	}

	s.lastUpdate = time.Now()
}

// Stop finalizes the stream. For Slack, Send() handles the final edit via the placeholder map,
// so Stop() is a no-op here — FinalizeStream stores the msgTS into c.placeholders.
func (s *slackStream) Stop(_ context.Context) error {
	return nil
}

// MessageID returns 0 — Slack uses string timestamps, not int message IDs.
// FinalizeStream handles the Slack-specific placeholder handoff via type assertion.
func (s *slackStream) MessageID() int {
	return 0
}

// MsgTS returns the Slack message timestamp (placeholder TS) for FinalizeStream handoff.
func (s *slackStream) MsgTS() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.msgTS
}

// StreamEnabled reports whether streaming is active for DMs or groups.
func (c *Channel) StreamEnabled(isGroup bool) bool {
	if isGroup {
		return c.config.GroupStream != nil && *c.config.GroupStream
	}
	return c.config.DMStream != nil && *c.config.DMStream
}

// CreateStream creates a per-run streaming handle for the given chatID.
// Implements channels.StreamingChannel.
// The placeholder "Thinking..." was already sent in handleMessage.
func (c *Channel) CreateStream(_ context.Context, chatID string, _ bool) (channels.ChannelStream, error) {
	stream := &slackStream{
		api:       c.api,
		channelID: extractChannelID(chatID),
		threadTS:  extractThreadTS(chatID),
	}
	if key, pTS, ok := c.streamPlaceholder(chatID); ok {
		stream.msgTS = pTS
		stream.key = key
	}
	return stream, nil
}

// streamPlaceholder resolves the placeholder a run should stream into. The
// streaming transport only receives the run's local key, while a placeholder may
// be keyed with a thread suffix (each top-level mention owns its own thread —
// see threadPlaceholderKey). An exact hit wins. Otherwise a single thread-scoped
// entry for the same local key is reused, so one in-flight mention keeps its live
// preview; when several mentions are in flight the lookup is ambiguous and the
// stream stays a no-op rather than editing another thread's placeholder.
func (c *Channel) streamPlaceholder(chatID string) (key, ts string, ok bool) {
	if v, found := c.placeholders.Load(chatID); found {
		if s, isString := v.(string); isString {
			return chatID, s, true
		}
	}

	prefix := chatID + ":thread:"
	matches := 0
	c.placeholders.Range(func(k, v any) bool {
		ks, kOK := k.(string)
		s, vOK := v.(string)
		if !kOK || !vOK || !strings.HasPrefix(ks, prefix) {
			return true
		}
		matches++
		key, ts = ks, s
		return matches < 2
	})
	if matches != 1 {
		return "", "", false
	}
	return key, ts, true
}

// FinalizeStream stores the stream's placeholder TS back into c.placeholders so that
// Send() can edit it with the properly formatted final response.
// Implements channels.StreamingChannel.
// ReasoningStreamEnabled returns false — Slack lane support is deferred to a separate PR.
// Slack streaming uses thread replies which have different UX from Telegram in-place edit.
func (c *Channel) ReasoningStreamEnabled() bool { return false }

func (c *Channel) FinalizeStream(_ context.Context, chatID string, stream channels.ChannelStream) {
	ss, ok := stream.(*slackStream)
	if !ok || ss.msgTS == "" {
		return
	}
	// Store the placeholder back under the key it was resolved from, so Send()
	// finds it again through metadata["placeholder_key"].
	key := ss.key
	if key == "" {
		key = chatID
	}
	c.placeholders.Store(key, ss.msgTS)
}

// threadPlaceholderKey returns the c.placeholders key for a run's "Thinking..."
// placeholder.
//
// A group session is keyed per channel, but in Slack every top-level @mention
// opens its own reply thread, so answering one mention never replaces another.
// Keying the placeholder by the session key alone therefore makes concurrent
// mentions in the same channel overwrite each other's entry: the second mention
// takes over the first one's placeholder timestamp, the first answer is edited
// into the newer thread, and the first placeholder is orphaned as "Thinking...".
// Carrying the reply thread in the key gives each mention its own entry while
// leaving local_key (and group session/history scoping) untouched.
func threadPlaceholderKey(localKey, replyThreadTS string) string {
	if replyThreadTS == "" || strings.Contains(localKey, ":thread:") {
		return localKey
	}
	return localKey + ":thread:" + replyThreadTS
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
