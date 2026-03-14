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
	pTS, pOK := c.placeholders.Load(chatID)
	if !pOK {
		// No placeholder — stream will be a no-op
		return &slackStream{
			api:       c.api,
			channelID: extractChannelID(chatID),
			threadTS:  extractThreadTS(chatID),
		}, nil
	}

	return &slackStream{
		api:       c.api,
		channelID: extractChannelID(chatID),
		threadTS:  extractThreadTS(chatID),
		msgTS:     pTS.(string),
	}, nil
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
	c.placeholders.Store(chatID, ss.msgTS)
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
