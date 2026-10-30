package mattermost

import (
	"context"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels/typing"
)

const (
	// Mattermost clients drop the indicator after ~5s or on the next post
	// from the typer (the placeholder itself), so it is re-sent often.
	typingKeepalive = 4 * time.Second
	typingMaxTime   = 60 * time.Second
	typingTimeout   = 5 * time.Second
)

// startTyping shows the typing indicator for a reply under localKey until
// stopTyping, with keepalive and a TTL safety net.
func (c *Channel) startTyping(localKey, channelID, parentID string) {
	ctrl := typing.New(typing.Options{
		MaxDuration:       typingMaxTime,
		KeepaliveInterval: typingKeepalive,
		StartFn: func() error {
			ctx, cancel := context.WithTimeout(context.Background(), typingTimeout)
			defer cancel()
			return c.api.userTyping(ctx, c.botUserID, channelID, parentID)
		},
	})
	if prev, loaded := c.typingCtrls.Swap(localKey, ctrl); loaded {
		prev.(*typing.Controller).Stop()
	}
	ctrl.Start()
}

// stopTyping stops the indicator for localKey, if any.
func (c *Channel) stopTyping(localKey string) {
	if ctrl, ok := c.typingCtrls.LoadAndDelete(localKey); ok {
		ctrl.(*typing.Controller).Stop()
	}
}

// stopAllTyping stops every indicator; used on channel shutdown.
func (c *Channel) stopAllTyping() {
	c.typingCtrls.Range(func(k, _ any) bool {
		c.stopTyping(k.(string))
		return true
	})
}
