package whatsapp

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// MediaCaptionBuffer delays media messages briefly so that follow-up caption
// text (sent by WhatsApp as a separate event) can be merged before the message
// reaches the agent pipeline.
//
// Flow:
//   1. Media arrives with no caption  → buffer for delayMs, keyed by chatID:senderID
//   2. Text arrives within window     → merge as caption, flush immediately
//   3. Timer expires with no text     → flush media as-is (no caption)
type MediaCaptionBuffer struct {
	mu      sync.Mutex
	entries map[string]*bufferedEntry
	delay   time.Duration

	// publishFn is called when the buffer flushes a message.
	publishFn func(msg bus.InboundMessage)
	// cleanupFn schedules temp media file cleanup after publish.
	cleanupFn func(paths []string, delay time.Duration)
}

type bufferedEntry struct {
	msg        bus.InboundMessage
	mediaPaths []string
	timer      *time.Timer
}

// NewMediaCaptionBuffer creates a buffer with the given delay.
func NewMediaCaptionBuffer(delay time.Duration, publishFn func(bus.InboundMessage), cleanupFn func([]string, time.Duration)) *MediaCaptionBuffer {
	return &MediaCaptionBuffer{
		entries:   make(map[string]*bufferedEntry),
		delay:     delay,
		publishFn: publishFn,
		cleanupFn: cleanupFn,
	}
}

// PushMedia buffers a media message for the given chatID+senderID.
// Returns true if the message was buffered (caller should NOT publish).
// Returns false if buffering is disabled or an error occurred.
func (b *MediaCaptionBuffer) PushMedia(chatID, senderID string, msg bus.InboundMessage, mediaPaths []string) bool {
	key := chatID + ":" + senderID

	b.mu.Lock()
	// If an entry already exists for this key (e.g. rapid media burst),
	// flush the old one first to avoid stale messages.
	if old, exists := b.entries[key]; exists {
		old.timer.Stop()
		delete(b.entries, key)
		b.mu.Unlock()
		b.publishFn(old.msg)
		if b.cleanupFn != nil {
			b.cleanupFn(old.mediaPaths, 5*time.Minute)
		}
		b.mu.Lock()
	}

	entry := &bufferedEntry{
		msg:        msg,
		mediaPaths: mediaPaths,
	}
	entry.timer = time.AfterFunc(b.delay, func() {
		b.flushEntry(key)
	})
	b.entries[key] = entry
	b.mu.Unlock()

	slog.Debug("media caption buffer: media buffered",
		"key", key, "delay_ms", b.delay.Milliseconds(),
		"media_count", len(mediaPaths))
	return true
}

// PushText checks if there is a buffered media message for chatID:senderID.
// If found, the text is merged as caption content and the entry is flushed
// immediately. Returns (mergedContent, true) if a match was found.
// Returns ("", false) if no match (caller should process normally).
func (b *MediaCaptionBuffer) PushText(chatID, senderID, text string) (string, bool) {
	key := chatID + ":" + senderID

	b.mu.Lock()
	entry, exists := b.entries[key]
	if !exists {
		b.mu.Unlock()
		return "", false
	}

	// Stop timer and take ownership.
	entry.timer.Stop()
	delete(b.entries, key)
	b.mu.Unlock()

	// Merge caption text into the buffered message.
	msg := entry.msg
	if msg.Content == emptyMessageSentinel || msg.Content == "" {
		msg.Content = text
	} else {
		// Content has media tags — append caption after them.
		msg.Content = msg.Content + "\n\n" + text
	}

	slog.Info("media caption buffer: caption merged",
		"key", key, "content_preview", truncateBufferStr(msg.Content, 80))

	// Publish the merged message.
	b.publishFn(msg)
	if b.cleanupFn != nil {
		b.cleanupFn(entry.mediaPaths, 5*time.Minute)
	}

	return msg.Content, true
}

// FlushAll flushes all pending entries. Called during graceful shutdown.
func (b *MediaCaptionBuffer) FlushAll() {
	b.mu.Lock()
	keys := make([]string, 0, len(b.entries))
	for k := range b.entries {
		keys = append(keys, k)
	}
	b.mu.Unlock()

	for _, key := range keys {
		b.flushEntry(key)
	}
}

// flushEntry publishes a single buffered entry and removes it from the map.
func (b *MediaCaptionBuffer) flushEntry(key string) {
	b.mu.Lock()
	entry, exists := b.entries[key]
	if !exists {
		b.mu.Unlock()
		return
	}
	delete(b.entries, key)
	b.mu.Unlock()

	slog.Debug("media caption buffer: timer expired, flushing without caption",
		"key", key)

	b.publishFn(entry.msg)
	if b.cleanupFn != nil {
		b.cleanupFn(entry.mediaPaths, 5*time.Minute)
	}
}

// truncateBufferStr truncates a string for log previews.
func truncateBufferStr(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
