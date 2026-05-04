package bot

import (
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels/typing"
)

// Zalo expires the indicator after ~5s; re-fire under that.
const (
	typingKeepalive = 4 * time.Second
	typingMaxTTL    = 60 * time.Second
)

func (c *Channel) startTyping(chatID string) {
	if !c.IsRunning() {
		return
	}
	ctrl := typing.New(typing.Options{
		MaxDuration:       typingMaxTTL,
		KeepaliveInterval: typingKeepalive,
		StartFn: func() error {
			return c.sendChatAction(chatID, "typing")
		},
	})
	if prev, ok := c.typingCtrls.Load(chatID); ok {
		prev.(*typing.Controller).Stop()
	}
	c.typingCtrls.Store(chatID, ctrl)
	// If Stop's Range happened before our Store, ctrl would leak past shutdown.
	if !c.IsRunning() {
		c.typingCtrls.Delete(chatID)
		ctrl.Stop()
		return
	}
	ctrl.Start()
}
