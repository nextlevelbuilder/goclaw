package telegram

import (
	"context"
	"strings"

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

// handleMeowCommand processes a /meow subcommand and returns the reply text.
// senderID is the bare numeric Telegram id. Pure (no telego I/O) so it is
// unit-testable; the command case wires the reply send.
func (c *Channel) handleMeowCommand(ctx context.Context, senderID string, args []string) string {
	if c.meowCfg == nil {
		return "Meow is not enabled on this instance."
	}
	sub := ""
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}
	switch sub {
	case "verify":
		ok, err := meow.VerifyRoundTrip(ctx, c.meowCfg, senderID)
		if err != nil {
			return "Verification failed: " + err.Error()
		}
		if ok {
			return "✅ Owner verified. Owner-gated /meow commands are now enabled."
		}
		return "❌ Not authorized. Ask an admin to set the Meow owner chat id, then run /meow verify from that chat."
	case "post":
		// Owner-gated: closed by default until configured + verified.
		if !c.meowOwnerAllowed(ctx, senderID) {
			return "❌ Not authorized. Owner-gated — run /meow verify from the configured owner chat first."
		}
		if c.meowPublish == nil {
			return "Publishing is not wired on this instance."
		}
		if len(args) < 3 {
			return "Usage: /meow post <@handle> <YYYY-MM-DD> [force]"
		}
		handle, date := args[1], args[2]
		force := len(args) > 3 && strings.EqualFold(args[3], "force")
		res, err := c.meowPublish(ctx, handle, date, force)
		if err != nil {
			return "Publish failed: " + err.Error()
		}
		return res
	default:
		return "Meow commands:\n" +
			"/meow verify — verify you are the owner\n" +
			"/meow post <@handle> <YYYY-MM-DD> [force] — publish a channel post (owner only)"
	}
}
