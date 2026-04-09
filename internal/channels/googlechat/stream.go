package googlechat

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// chatStream manages streaming preview state for a single Google Chat conversation.
// Ref: Telegram DraftStream in telegram/stream.go — same throttle/dedup pattern,
// adapted for Google Chat PATCH API (updateMask=text).
type chatStream struct {
	ch          *Channel      // back-reference for auth + HTTP client
	messageName string        // "spaces/xxx/messages/yyy"
	lastText    string        // dedup: skip edit if unchanged
	lastEdit    time.Time     // throttle tracking
	throttle    time.Duration // default 1500ms
	mu          sync.Mutex
	stopped     bool
	pending     string      // buffered text during throttle window
	flushTimer  *time.Timer // fires after throttle to send pending
}

// newChatStream creates a streaming state manager for a conversation.
func newChatStream(ch *Channel, messageName string) *chatStream {
	return &chatStream{
		ch:          ch,
		messageName: messageName,
		throttle:    defaultStreamThrottle,
	}
}

// update sends or buffers new streaming text. Throttled and deduped.
func (cs *chatStream) update(ctx context.Context, fullText string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.stopped {
		return
	}

	// Dedup
	if fullText == cs.lastText {
		return
	}

	cs.pending = fullText

	// Throttle: buffer if too soon
	if time.Since(cs.lastEdit) < cs.throttle {
		cs.resetFlushTimer()
		return
	}

	cs.cancelFlushTimer()
	cs.doFlush(ctx)
}

// stop finalizes the stream: cancel timer, flush pending, mark stopped.
func (cs *chatStream) stop(ctx context.Context) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.stopped = true
	cs.cancelFlushTimer()
	cs.doFlush(ctx)
}

// doFlush sends pending text via PATCH edit. Must hold mu lock.
func (cs *chatStream) doFlush(ctx context.Context) {
	if cs.pending == "" || cs.pending == cs.lastText {
		return
	}

	text := cs.pending
	formatted := markdownToGoogleChat(text)

	// Truncate to fit Google Chat limit
	if len([]byte(formatted)) > googleChatMaxMessageBytes {
		formatted = truncateBytes(formatted, googleChatMaxMessageBytes-len([]byte("…"))) + "…"
	}

	token, err := cs.ch.auth.Token(ctx)
	if err != nil {
		slog.Warn("googlechat: stream flush auth failed", "error", err)
		return
	}

	if err := editMessage(ctx, cs.ch.apiBase, token, cs.ch.httpClient, cs.messageName, formatted); err != nil {
		slog.Warn("googlechat: stream edit failed", "error", err, "name", cs.messageName)
		return
	}

	cs.lastText = text
	cs.lastEdit = time.Now()
}

// resetFlushTimer starts or resets the timer to flush pending text after
// the remaining throttle interval. Must hold mu lock.
func (cs *chatStream) resetFlushTimer() {
	if cs.flushTimer != nil {
		cs.flushTimer.Stop()
	}
	remaining := cs.throttle - time.Since(cs.lastEdit)
	if remaining <= 0 {
		remaining = cs.throttle
	}
	// Timer callback runs on a separate goroutine after the caller releases mu.
	// Uses context.Background() intentionally — the originating request context
	// may be cancelled by then; a best-effort flush is acceptable for streaming previews.
	cs.flushTimer = time.AfterFunc(remaining, func() {
		cs.mu.Lock()
		defer cs.mu.Unlock()
		if !cs.stopped {
			cs.doFlush(context.Background())
		}
	})
}

// cancelFlushTimer stops any pending flush timer. Must hold mu lock.
func (cs *chatStream) cancelFlushTimer() {
	if cs.flushTimer != nil {
		cs.flushTimer.Stop()
		cs.flushTimer = nil
	}
}

// truncateBytes truncates a string to maxBytes without cutting mid-UTF8.
func truncateBytes(s string, maxBytes int) string {
	b := []byte(s)
	if len(b) <= maxBytes {
		return s
	}
	if maxBytes <= 0 {
		return ""
	}
	// Don't cut in the middle of a UTF-8 sequence
	for maxBytes > 0 && maxBytes < len(b) && b[maxBytes]>>6 == 0b10 {
		maxBytes--
	}
	return string(b[:maxBytes])
}

// --- ChannelStream interface implementation (chatStream) ---

// Update sends or edits the streaming message with the latest accumulated text.
// Implements channels.ChannelStream.
func (cs *chatStream) Update(ctx context.Context, text string) {
	cs.update(ctx, text)
}

// Stop finalizes the stream with a final flush.
// Implements channels.ChannelStream.
func (cs *chatStream) Stop(ctx context.Context) error {
	cs.stop(ctx)
	return nil
}

// MessageID returns 0 — Google Chat uses string messageName, not int IDs.
// FinalizeStream handles the Google Chat-specific placeholder handoff via type assertion.
// Implements channels.ChannelStream.
func (cs *chatStream) MessageID() int { return 0 }

// MessageName returns the Google Chat message resource name for FinalizeStream handoff.
func (cs *chatStream) MessageName() string {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.messageName
}

// --- StreamingChannel interface implementation (Channel) ---

// StreamEnabled returns whether streaming is enabled for DMs or groups.
func (c *Channel) StreamEnabled(isGroup bool) bool {
	if isGroup {
		return c.groupStream
	}
	return c.dmStream
}

// CreateStream creates a per-run streaming handle for the given chatID.
// Implements channels.StreamingChannel.
//
// Reuses existing placeholder message if available (from sendPlaceholder or
// a previous FinalizeStream), otherwise creates a new "⏳" message.
func (c *Channel) CreateStream(ctx context.Context, chatID string, _ bool) (channels.ChannelStream, error) {
	var messageName string

	// Check for existing placeholder (from sendPlaceholder or previous FinalizeStream)
	if v, ok := c.placeholders.Load(chatID); ok {
		c.placeholders.Delete(chatID)
		messageName = v.(string)
		slog.Info("googlechat: stream reusing placeholder", "chat_id", chatID, "name", messageName)
	} else {
		// Create new stream message
		token, err := c.auth.Token(ctx)
		if err != nil {
			return nil, err
		}
		resp, err := postChatMessage(ctx, c.apiBase, token, c.httpClient, chatID,
			map[string]any{"text": "⏳"}, "")
		if err != nil {
			return nil, fmt.Errorf("googlechat: create stream message: %w", err)
		}
		messageName = resp.Name
		slog.Info("googlechat: stream created new message", "chat_id", chatID, "name", messageName)
	}

	cs := newChatStream(c, messageName)
	return cs, nil
}

// FinalizeStream hands the stream's messageName back to the placeholders map so that
// Send() can edit it with the properly formatted final response.
// Implements channels.StreamingChannel.
func (c *Channel) FinalizeStream(_ context.Context, chatID string, stream channels.ChannelStream) {
	cs, ok := stream.(*chatStream)
	if !ok || cs.MessageName() == "" {
		return
	}
	c.placeholders.Store(chatID, cs.MessageName())
	slog.Info("googlechat: stream ended, handing off to Send()", "chat_id", chatID, "name", cs.MessageName())
}

// ReasoningStreamEnabled returns false — Google Chat doesn't support reasoning lanes yet.
// The PATCH-based streaming doesn't support dual-lane preview like Telegram DraftStream.
func (c *Channel) ReasoningStreamEnabled() bool { return false }
