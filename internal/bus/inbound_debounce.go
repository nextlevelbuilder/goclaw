// Package bus — Inbound message debouncer.
// Matching TS src/auto-reply/inbound-debounce.ts createInboundDebouncer().
//
// Buffers rapid consecutive messages from the same sender and merges them
// into a single InboundMessage before processing. This prevents multiple
// agent runs when a user sends several short messages in quick succession.
package bus

import (
	"log/slog"
	"strings"
	"sync"
	"time"
)

// InboundDebouncer buffers rapid inbound messages from the same sender
// and merges them into a single message before calling flushFn.
type InboundDebouncer struct {
	debounceMs time.Duration
	mu         sync.Mutex
	buffers    map[string]*debounceBuffer
	flushFn    func(InboundMessage)
}

type debounceBuffer struct {
	messages []InboundMessage
	timer    *time.Timer
}

// NewInboundDebouncer creates a debouncer with the given window and flush callback.
// If debounceMs <= 0, messages are passed through immediately (debouncing disabled).
func NewInboundDebouncer(debounceMs time.Duration, flushFn func(InboundMessage)) *InboundDebouncer {
	return &InboundDebouncer{
		debounceMs: debounceMs,
		buffers:    make(map[string]*debounceBuffer),
		flushFn:    flushFn,
	}
}

// Push adds a message to the debounce buffer.
// If debouncing is disabled or the message should bypass (media), it is flushed immediately.
func (d *InboundDebouncer) Push(msg InboundMessage) {
	// Disabled: pass through immediately.
	if d.debounceMs <= 0 {
		d.flushFn(msg)
		return
	}

	key := debounceKey(msg)

	// Media messages go through the same debounce buffer as text.
	// This allows follow-up caption text to be merged with the media message
	// (WhatsApp and similar channels may send media and caption as separate events).
	// When a media message arrives, flush any previously buffered messages for this key
	// first to avoid merging unrelated text with media, then buffer the media.
	if len(msg.Media) > 0 {
		d.flushKey(key)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	buf, exists := d.buffers[key]
	if !exists {
		buf = &debounceBuffer{}
		d.buffers[key] = buf
	}

	buf.messages = append(buf.messages, msg)

	// Reset debounce timer — fires after debounceMs of silence.
	if buf.timer != nil {
		buf.timer.Stop()
	}
	buf.timer = time.AfterFunc(d.debounceMs, func() {
		d.flushKey(key)
	})

	if len(buf.messages) == 1 {
		slog.Debug("inbound debounce: buffering",
			"key", key, "debounce_ms", d.debounceMs.Milliseconds())
	} else {
		slog.Debug("inbound debounce: message appended",
			"key", key, "buffered", len(buf.messages))
	}
}

// Stop flushes all pending buffers immediately (graceful shutdown).
func (d *InboundDebouncer) Stop() {
	d.mu.Lock()
	keys := make([]string, 0, len(d.buffers))
	for k := range d.buffers {
		keys = append(keys, k)
	}
	d.mu.Unlock()

	for _, key := range keys {
		d.flushKey(key)
	}
}

// flushKey merges and flushes all buffered messages for a key.
func (d *InboundDebouncer) flushKey(key string) {
	d.mu.Lock()
	buf, exists := d.buffers[key]
	if !exists || len(buf.messages) == 0 {
		d.mu.Unlock()
		return
	}

	// Stop timer if still pending.
	if buf.timer != nil {
		buf.timer.Stop()
	}

	// Take ownership of messages and remove buffer.
	msgs := buf.messages
	delete(d.buffers, key)
	d.mu.Unlock()

	merged := mergeInboundMessages(msgs)

	if len(msgs) > 1 {
		slog.Info("inbound debounce: merged messages",
			"key", key, "count", len(msgs),
			"content_preview", truncateStr(merged.Content, 80))
	}

	d.flushFn(merged)
}

// debounceKey builds the buffer key: channel:chatID:senderID.
func debounceKey(msg InboundMessage) string {
	return msg.Channel + ":" + msg.ChatID + ":" + msg.SenderID
}

// mergeInboundMessages combines multiple messages into one.
// Content is joined with newlines; media paths are concatenated;
// metadata and other fields come from the last message.
func mergeInboundMessages(msgs []InboundMessage) InboundMessage {
	if len(msgs) == 1 {
		return msgs[0]
	}

	last := msgs[len(msgs)-1]

	// Join content with newlines (matching TS: entries.map(e => e.body).join("\n"))
	parts := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if m.Content != "" {
			parts = append(parts, m.Content)
		}
	}
	last.Content = strings.Join(parts, "\n")

	// Merge media from all messages.
	var allMedia []MediaFile
	for _, m := range msgs {
		allMedia = append(allMedia, m.Media...)
	}
	last.Media = allMedia

	return last
}

// truncateStr truncates a string to maxLen characters.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
