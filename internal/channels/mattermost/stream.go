package mattermost

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

const streamThrottleInterval = 1000 * time.Millisecond

// mmStream edits the "Thinking..." placeholder post as chunks arrive.
type mmStream struct {
	api        *apiClient
	postID     string // placeholder post; empty = no-op stream
	lastUpdate time.Time
	mu         sync.Mutex
}

// Update edits the placeholder with the accumulated text, throttled.
func (s *mmStream) Update(ctx context.Context, fullText string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.postID == "" || time.Since(s.lastUpdate) < streamThrottleInterval {
		return
	}

	// Finished attachment blocks are hidden while streaming; Send renders them.
	text, _ := extractAttachments(fullText)
	if text == "" {
		return
	}
	if len(text) > maxMessageLen {
		text = channels.ChunkMarkdown(text, maxMessageLen)[0] + "..."
	}
	if err := s.api.patchPost(ctx, s.postID, text, nil); err != nil {
		slog.Debug("mattermost stream update failed", "error", err)
		return
	}
	s.lastUpdate = time.Now()
}

// Stop is a no-op: Send() writes the final text into the placeholder after
// FinalizeStream hands the post ID back.
func (s *mmStream) Stop(_ context.Context) error { return nil }

// MessageID returns 0 — Mattermost post IDs are strings.
func (s *mmStream) MessageID() int { return 0 }

// StreamEnabled reports whether streaming is on for DMs or channels.
func (c *Channel) StreamEnabled(isGroup bool) bool {
	if isGroup {
		return c.cfg.GroupStream != nil && *c.cfg.GroupStream
	}
	return c.cfg.DMStream != nil && *c.cfg.DMStream
}

// CreateStream returns a stream bound to the placeholder posted for chatID
// (the local key). Without a placeholder the stream does nothing.
func (c *Channel) CreateStream(_ context.Context, chatID string, _ bool) (channels.ChannelStream, error) {
	s := &mmStream{api: c.api}
	if pID, ok := c.placeholders.Load(chatID); ok {
		s.postID = pID.(string)
	}
	return s, nil
}

// FinalizeStream stores the placeholder post ID back so Send() edits it with
// the final response.
func (c *Channel) FinalizeStream(_ context.Context, chatID string, stream channels.ChannelStream) {
	s, ok := stream.(*mmStream)
	if !ok || s.postID == "" {
		return
	}
	c.placeholders.Store(chatID, s.postID)
}

// ReasoningStreamEnabled returns false: reasoning is not shown as a separate
// post, matching the Slack edit-in-place behavior.
func (c *Channel) ReasoningStreamEnabled() bool { return false }
