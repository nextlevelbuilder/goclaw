package bitrix24

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Channel is the goclaw-side Bitrix24 bot handle.
//
// One Channel instance per (portal, bot_code) pair. Start() resolves the
// portal via the shared Router, calls imbot.register idempotently, and wires
// the returned bot_id into the Router so inbound events land here.
//
// Concurrency:
//   - BaseChannel handles its own locking for health / running / allowlist.
//   - `cfg`, `portalStore`, `encKey`, `router` are write-once at construction.
//   - `portal`, `client`, `botID` are written during Start() and read
//     afterwards; guarded by startMu so Stop() sees a coherent snapshot even
//     if it races a failed Start().
type Channel struct {
	*channels.BaseChannel

	cfg         bitrixInstanceConfig
	portalStore store.BitrixPortalStore
	encKey      string
	router      *Router

	startMu sync.Mutex
	portal  *Portal
	client  *Client
	botID   int

	// stopOnce ensures close(stopCh) runs at most once. stopCh is wired into
	// any long-running goroutine the channel spawns (currently none in Phase
	// 03, reserved for Phase 05 streaming).
	stopOnce sync.Once
	stopCh   chan struct{}

	// mentionRe caches the compiled per-botID mention regex. Rebuilt on
	// demand whenever the cached entry's bot id no longer matches the
	// channel's current bot id (e.g. after a Reload() re-registered and
	// got back a different id). mentionMu guards mentionRe specifically
	// so the hot read path doesn't contend with Start/Stop on startMu.
	mentionMu sync.Mutex
	mentionRe *mentionMatcher
}

// Type returns the platform identifier used by the router / health pages.
// Always "bitrix24" regardless of the DB-instance name.
func (c *Channel) Type() string { return channels.TypeBitrix24 }

// BotID exposes the registered bot id for the Phase 02 BotDispatcher
// contract. Returns 0 before Start() completes — Router.RegisterBot rejects
// zero, so early calls are a no-op.
func (c *Channel) BotID() int {
	c.startMu.Lock()
	defer c.startMu.Unlock()
	return c.botID
}

// PortalName returns the portal key configured on this channel (not the
// Bitrix24 domain). Used by BotDispatcher so Router.handleAppUninstall can
// drop bots by portal without needing a back-pointer to the portal struct.
func (c *Channel) PortalName() string { return c.cfg.Portal }

// Config returns a copy of the instance config. Exported for tests.
func (c *Channel) Config() bitrixInstanceConfig { return c.cfg }

// Start brings the channel online:
//  1. Resolve the portal (load from store on cold path via the Router).
//  2. Sanity-check it's installed — uninstalled portals surface as Failed
//     health with an actionable message (admin must visit /bitrix24/install).
//  3. Ensure the portal's refresh loop is running (idempotent).
//  4. imbot.register with idempotency via portal state.
//  5. Register the (bot_id → Channel) mapping with the Router.
//
// On failure the channel's own bot_id / Router bot entry are NOT populated,
// so a later Reload() re-runs cleanly. The portal entry is intentionally
// left behind — other bots on the same portal share it, and the Router's
// EnsurePortalRunning is idempotent across restarts.
//
// The mention regex cache is automatically invalidated if/when bot_id
// changes across retries (see mention() in handle.go) — no reset needed
// here. Avoiding a sync.Once reassignment also avoids a race with any
// in-flight handler goroutine still calling mention().
func (c *Channel) Start(ctx context.Context) error {
	c.MarkStarting("Registering bot")

	tid := c.TenantID()
	if tid == uuid.Nil {
		c.MarkFailed("Missing tenant", "channel instance has no tenant_id", channels.ChannelFailureKindConfig, false)
		return errors.New("bitrix24: missing tenant_id (InstanceLoader must call SetTenantID)")
	}

	p, err := c.router.ResolveOrLoadPortal(ctx, tid, c.cfg.Portal)
	if err != nil {
		c.MarkFailed("Portal not found", err.Error(), channels.ChannelFailureKindConfig, false)
		return err
	}
	if !p.Installed() {
		msg := fmt.Sprintf("Portal %q not installed — visit /bitrix24/install to authorize before starting the channel.", c.cfg.Portal)
		c.MarkFailed("Portal not installed", msg, channels.ChannelFailureKindAuth, false)
		return fmt.Errorf("bitrix24 portal %q not installed", c.cfg.Portal)
	}

	c.router.RegisterPortal(p)
	c.router.EnsurePortalRunning(ctx, p)

	c.startMu.Lock()
	c.portal = p
	c.client = p.Client()
	c.startMu.Unlock()

	botID, err := c.registerBot(ctx)
	if err != nil {
		c.MarkFailed("imbot.register failed", err.Error(), classifyStartupErr(err), true)
		return err
	}
	if err := p.RecordRegisteredBot(ctx, c.cfg.BotCode, botID); err != nil {
		slog.Warn("bitrix24: failed to persist bot_id",
			"tenant", tid, "portal", c.cfg.Portal, "bot_code", c.cfg.BotCode,
			"bot_id", botID, "err", err)
	}

	c.startMu.Lock()
	c.botID = botID
	c.startMu.Unlock()

	c.router.RegisterBot(botID, c)

	c.SetRunning(true)
	c.MarkHealthy("Connected")
	slog.Info("bitrix24 channel started",
		"name", c.Name(), "tenant", tid, "portal", c.cfg.Portal, "bot_id", botID)
	return nil
}

// Stop unwires the channel from the Router and closes the stop channel.
// Does NOT tear down the portal — other bots on the same portal keep it
// alive, and the Router's EnsurePortalRunning is idempotent on next Start().
// Safe to call multiple times.
func (c *Channel) Stop(ctx context.Context) error {
	c.startMu.Lock()
	botID := c.botID
	c.botID = 0
	c.startMu.Unlock()

	if botID > 0 {
		c.router.UnregisterBot(botID)
	}
	c.SetRunning(false)
	c.MarkStopped("")
	c.stopOnce.Do(func() { close(c.stopCh) })
	return nil
}

// WebhookHandler implements channels.WebhookChannel so the gateway can mount
// the shared Router onto the main HTTP mux. Only the first Bitrix24 channel
// wins the claim — every other channel returns ("", nil) and the gateway
// skips mounting. All portals share /bitrix24/install and /bitrix24/events.
func (c *Channel) WebhookHandler() (string, http.Handler) {
	return c.router.ClaimWebhookRoute()
}

// Router returns the shared Router instance. Exported for tests that want
// to assert Register / Unregister side-effects.
func (c *Channel) Router() *Router { return c.router }

// Portal returns the resolved portal (nil before Start()). Exported for
// tests that need to inspect the cached portal without reaching through
// the router.
func (c *Channel) Portal() *Portal {
	c.startMu.Lock()
	defer c.startMu.Unlock()
	return c.portal
}

// Client returns the REST client bound to the portal (nil before Start()).
func (c *Channel) Client() *Client {
	c.startMu.Lock()
	defer c.startMu.Unlock()
	return c.client
}

// classifyStartupErr maps registration errors into a health failure kind.
// APIError auth-family codes → Auth; transport errors → Network; everything
// else (config, quota, unknown) → Config so operators see an actionable
// "check instance config" message instead of a generic "unknown".
func classifyStartupErr(err error) channels.ChannelFailureKind {
	if err == nil {
		return channels.ChannelFailureKindUnknown
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case "expired_token", "invalid_token", "NO_AUTH_FOUND", "PORTAL_DELETED":
			return channels.ChannelFailureKindAuth
		}
	}
	return channels.ChannelFailureKindConfig
}
