package telegram

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/meow"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// meowPublishFn publishes the approved post for a handle + date, returning a
// human-readable result line. Injected by the gateway; nil = /meow post off.
type meowPublishFn func(ctx context.Context, handle, date string, force bool) (string, error)

// SetMeowConfig enables the owner-gated /meow command family by giving the
// channel the system config store that holds owner-gate state. nil = disabled.
func (c *Channel) SetMeowConfig(cs store.SystemConfigStore) { c.meowCfg = cs }

// SetMeowPublisher injects the owner-gated publish entrypoint used by /meow post.
func (c *Channel) SetMeowPublisher(fn meowPublishFn) { c.meowPublish = fn }

// meowOwnerAllowed reports whether senderID may run owner-gated /meow commands.
// Closed by default: a nil config store, or an unconfigured/unverified owner,
// denies everyone. senderID must be the bare numeric Telegram id.
func (c *Channel) meowOwnerAllowed(ctx context.Context, senderID string) bool {
	if c.meowCfg == nil {
		return false
	}
	return meow.LoadOwnerGate(ctx, c.meowCfg).Allowed(senderID)
}
