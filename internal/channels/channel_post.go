package channels

import (
	"context"
	"fmt"
)

// PostButton is one inline URL button for a rich channel post (label + URL).
type PostButton struct {
	Label string
	URL   string
}

// ChannelPoster is implemented by channels that can publish a rich channel post
// — a photo with an HTML caption and inline URL buttons — and return the
// platform message id. Only telegram implements it today.
type ChannelPoster interface {
	SendChannelPost(ctx context.Context, chatID, imagePath, captionHTML string, buttons []PostButton) (int64, error)
}

// PublishChannelPost resolves the named channel and publishes a rich post
// directly through it, returning the published message id. It deliberately does
// NOT route through bus.OutboundMessage, which carries neither inline buttons
// nor a message id. Errors if the channel is unknown or cannot post.
func (m *Manager) PublishChannelPost(ctx context.Context, channelName, chatID, imagePath, captionHTML string, buttons []PostButton) (int64, error) {
	m.mu.RLock()
	channel, exists := m.channels[channelName]
	m.mu.RUnlock()
	if !exists {
		return 0, fmt.Errorf("channel %s not found", channelName)
	}
	poster, ok := channel.(ChannelPoster)
	if !ok {
		return 0, fmt.Errorf("channel %s does not support channel posts", channelName)
	}
	return poster.SendChannelPost(ctx, chatID, imagePath, captionHTML, buttons)
}
