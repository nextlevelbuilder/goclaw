package oa

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/common"
)

// resolveSlug picks the operator-supplied path or falls back to a name-derived
// slug so wizard-created channels work without an explicit webhook_path.
func resolveSlug(cfgPath, name string) string {
	if cfgPath != "" {
		return cfgPath
	}
	return common.DeriveSlugFromName(name)
}

// OAuthCallbackPath is the fixed browser return path registered in the Zalo
// Developers console for every OA channel on a gateway.
const OAuthCallbackPath = "/oauth/zalo/callback"

// CallbackURL returns the public OAuth redirect URL shown during channel setup.
func CallbackURL(publicBase string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(publicBase), "/")
	if base == "" {
		return "", errors.New("zalo_oa: public gateway URL is unavailable")
	}
	return base + OAuthCallbackPath, nil
}

// WebhookURL returns the exact public endpoint that must be registered in the
// Zalo Developers console. A configured slug remains authoritative for
// existing instances; otherwise the same name-derived slug used by the router
// is returned.
func WebhookURL(publicBase, cfgPath, name string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(publicBase), "/")
	if base == "" {
		return "", errors.New("zalo_oa: public gateway URL is unavailable")
	}
	slug := resolveSlug(cfgPath, name)
	if err := common.ValidateSlug(slug); err != nil {
		return "", fmt.Errorf("zalo_oa: webhook slug: %w", err)
	}
	return base + common.WebhookPathPrefix + slug, nil
}

// startWebhookTransport registers with the shared router and optionally
// fires the catch-up sweep. Returns nil on misconfig so instance_loader
// doesn't crash. Channels with no webhook secret yet enter bootstrap
// mode (Degraded, acks ping, drops events).
func (c *Channel) startWebhookTransport() error {
	slug := resolveSlug(c.cfg.WebhookPath, c.Name())
	if err := c.webhookRouter.RegisterInstance(c.instanceID, c, c.TenantID(), slug); err != nil {
		c.MarkFailed("webhook slug invalid",
			err.Error(),
			channels.ChannelFailureKindConfig, false)
		return nil
	}
	c.resolvedSlug = slug

	if c.inBootstrap() {
		c.MarkBootstrap(
			channels.ChannelBootstrapAwaitingSecret,
			"awaiting webhook secret",
			"Zalo OA Secret Key not yet pasted. Webhook acks URL-verification ping with HTTP 200 but drops events. Paste Khóa bí mật OA in Credentials tab to enable signature verification.",
			channels.ChannelFailureKindConfig,
			true,
		)
		slog.Info("zalo_oa.webhook.bootstrap_active",
			"instance_id", c.instanceID, "oa_id", c.creds().OAID, "slug", slug)
		return nil
	}

	// The webhook must be live before OAuth consent so the create wizard can
	// register the URL and save Zalo's Webhook Secret Key in one continuous
	// flow. Until consent supplies oa_id, verified events are acknowledged but
	// dropped by HandleWebhookEvent and catch-up must not start.
	if c.creds().OAID == "" {
		slog.Info("zalo_oa.webhook.registered_preconsent",
			"instance_id", c.instanceID, "signature_mode", normalizeMode(c.cfg.WebhookSignatureMode), "slug", slug)
		return nil
	}

	mode := normalizeMode(c.cfg.WebhookSignatureMode)
	slog.Info("zalo_oa.webhook.registered",
		"instance_id", c.instanceID, "oa_id", c.creds().OAID, "signature_mode", mode, "slug", slug)

	if c.cfg.CatchUpOnRestart {
		c.catchUpWG.Add(1)
		go c.runCatchUpSweepGoroutine()
	}
	c.MarkHealthy("webhook")
	return nil
}

// runCatchUpSweepGoroutine runs runCatchUpSweep with stopCh-aware cancel.
// Both goroutines are tracked by catchUpWG so Stop() drains cleanly.
func (c *Channel) runCatchUpSweepGoroutine() {
	defer c.catchUpWG.Done()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c.catchUpWG.Go(func() {
		select {
		case <-c.stopCh:
			cancel()
		case <-ctx.Done():
		}
	})

	c.runCatchUpSweep(ctx)
}
