package mattermost

import (
	"context"
	"sync/atomic"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// MattermostStream is a no-op stream — Mattermost does not support streaming.
// It satisfies the channels.ChannelStream interface without making API calls.
type MattermostStream struct {
	stopped int32
}

// StreamEnabled returns false — Mattermost does not support streaming.
// Messages are sent as complete posts after the agent finishes processing.
func (c *Channel) StreamEnabled(_ bool) bool { return false }

// ReasoningStreamEnabled returns false — Mattermost uses typing indicator instead of a separate thinking message.
func (c *Channel) ReasoningStreamEnabled() bool { return false }

// CreateStream returns a no-op stream. No Mattermost post is created.
func (c *Channel) CreateStream(_ context.Context, _ string, _ bool) (channels.ChannelStream, error) {
	return &MattermostStream{}, nil
}

// FinalizeStream is a no-op — no placeholder to hand off.
func (c *Channel) FinalizeStream(_ context.Context, _ string, _ channels.ChannelStream) {}

// Update is a no-op.
func (s *MattermostStream) Update(_ context.Context, _ string) {}

// Stop is a no-op.
func (s *MattermostStream) Stop(_ context.Context) error {
	atomic.StoreInt32(&s.stopped, 1)
	return nil
}

// MessageID returns 0 (no streaming post exists).
func (s *MattermostStream) MessageID() int { return 0 }

