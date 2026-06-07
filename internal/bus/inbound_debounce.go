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
	delayFn func(InboundMessage) time.Duration
	mu      sync.Mutex
	buffers map[string]*debounceBuffer
	flushFn func(InboundMessage)
}

type debounceBuffer struct {
	messages []InboundMessage
	timer    *time.Timer
}

// NewInboundDebouncer creates a debouncer with the given window and flush callback.
// If debounceMs <= 0, messages are passed through immediately (debouncing disabled).
func NewInboundDebouncer(debounceMs time.Duration, flushFn func(InboundMessage)) *InboundDebouncer {
	return NewInboundDebouncerFunc(func(InboundMessage) time.Duration {
		return debounceMs
	}, flushFn)
}

// NewInboundDebouncerFunc creates a debouncer whose window can vary per message.
func NewInboundDebouncerFunc(delayFn func(InboundMessage) time.Duration, flushFn func(InboundMessage)) *InboundDebouncer {
	if delayFn == nil {
		delayFn = func(InboundMessage) time.Duration { return 0 }
	}
	return &InboundDebouncer{
		delayFn: delayFn,
		buffers: make(map[string]*debounceBuffer),
		flushFn: flushFn,
	}
}

// Push adds a message to the debounce buffer.
//
// Behavior:
//   - If delayFn returns > 0: append to per-key buffer and (re)set the silence timer.
//   - If delayFn returns <= 0 AND a buffer already exists for the key: append the
//     incoming message to the buffer and flush immediately (merge-then-flush).
//     This is required so a no-media follow-up cannot bypass a buffered media
//     message and trigger a duplicate agent run (issue #63).
//   - If delayFn returns <= 0 AND no buffer exists: pass through immediately.
//
// There is no media-specific bypass — media-bearing messages go through the same
// path as text. The per-message delay decision lives in the caller's delayFn.
func (d *InboundDebouncer) Push(msg InboundMessage) {
	debounceMs := d.delayFn(msg)
	key := debounceKey(msg)

	if debounceMs <= 0 {
		// Disabled-path: merge into existing buffer if any, else pass through.
		d.mu.Lock()
		buf, exists := d.buffers[key]
		if exists && len(buf.messages) > 0 {
			buf.messages = append(buf.messages, msg)
			d.mu.Unlock()
			// flushKey re-locks, drains buffer, merges, and calls flushFn.
			d.flushKey(key)
			return
		}
		d.mu.Unlock()
		d.flushFn(msg)
		return
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
	buf.timer = time.AfterFunc(debounceMs, func() {
		d.flushKey(key)
	})

	if len(buf.messages) == 1 {
		slog.Debug("inbound debounce: buffering",
			"key", key, "debounce_ms", debounceMs.Milliseconds())
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

// debounceKey builds the buffer key: channel:chatID:senderID:agentID.
func debounceKey(msg InboundMessage) string {
	return msg.Channel + ":" + msg.ChatID + ":" + msg.SenderID + ":" + msg.AgentID
}

// mergeInboundMessages combines multiple messages into one.
//
// Behavior:
//   - Content: joined with newlines (matches TS upstream entries.map(e => e.body).join("\n")).
//   - Media: concatenated in arrival order.
//   - Metadata: starts from the last message (preserves legacy "latest wins" for
//     channel-specific fields), then overlays metadata["merged_message_ids"] with
//     a deduplicated arrival-ordered comma-separated list of all source message_ids.
//     The consumer-level dedup uses this list to short-circuit retransmits of any
//     sibling member (cmd/gateway_consumer.go).
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

	// Aggregate merged_message_ids — preserves arrival order, dedups.
	merged := collectMergedMessageIDs(msgs)
	if merged != "" {
		if last.Metadata == nil {
			last.Metadata = map[string]string{}
		} else {
			// Copy-on-write: don't mutate the input message's map.
			cloned := make(map[string]string, len(last.Metadata)+1)
			for k, v := range last.Metadata {
				cloned[k] = v
			}
			last.Metadata = cloned
		}
		last.Metadata["merged_message_ids"] = merged
	}

	return last
}

// collectMergedMessageIDs returns a comma-separated, arrival-ordered, deduplicated
// list of all source message_ids. Handles already-merged inputs by splitting any
// existing merged_message_ids entries back into individual IDs.
func collectMergedMessageIDs(msgs []InboundMessage) string {
	seen := make(map[string]struct{}, len(msgs))
	ordered := make([]string, 0, len(msgs))

	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ordered = append(ordered, id)
	}

	for _, m := range msgs {
		if m.Metadata == nil {
			continue
		}
		// Already-merged messages bring their full ID list.
		if existing := m.Metadata["merged_message_ids"]; existing != "" {
			for _, id := range strings.Split(existing, ",") {
				add(id)
			}
		}
		if mid := m.Metadata["message_id"]; mid != "" {
			add(mid)
		}
	}

	return strings.Join(ordered, ",")
}

// truncateStr truncates a string to maxLen characters.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
