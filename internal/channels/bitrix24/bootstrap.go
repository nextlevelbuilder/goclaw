package bitrix24

import (
	"context"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// BootstrapPortals warms the shared Router with every bitrix_portals row in
// the database at gateway startup.
//
// Why at boot (vs lazy on first channel Start):
//   - Webhook lookups by domain need every portal registered even if no
//     channel instance is loaded yet (an admin might install a new portal
//     before adding the corresponding channel_instances row).
//   - Log install URLs for uninstalled portals so operators have a
//     copy-pasteable link without digging through the config.
//   - Kick off refresh loops eagerly; tokens silently expire otherwise.
//
// Idempotent: safe to call multiple times per process, and safe to call
// before any channel_instances row exists (just a no-op).
//
// Errors loading a single portal are logged and skipped — one broken row
// must not block the gateway from starting the rest.
func BootstrapPortals(ctx context.Context, portalStore store.BitrixPortalStore, encKey string) error {
	if portalStore == nil {
		return nil // no PG → no portals to bootstrap (SQLite lite edition)
	}

	router, err := InitWebhookRouter(portalStore, encKey, RouterConfig{})
	if err != nil {
		return err
	}

	rows, err := portalStore.ListAllForLoader(ctx)
	if err != nil {
		return err
	}

	for _, row := range rows {
		p, err := NewPortal(ctx, row.TenantID, row.Name, portalStore, encKey)
		if err != nil {
			slog.Warn("bitrix24 bootstrap: skip portal",
				"tenant", row.TenantID, "portal", row.Name, "domain", row.Domain, "err", err)
			continue
		}
		router.RegisterPortal(p)
		if !p.Installed() {
			slog.Info("bitrix24 bootstrap: portal not installed — admin must complete OAuth",
				"tenant", row.TenantID, "portal", row.Name, "domain", row.Domain,
				"install_path", installPath,
				"state_param", p.TenantID().String()+":"+p.Name())
			continue
		}
		router.EnsurePortalRunning(ctx, p)
		slog.Info("bitrix24 bootstrap: portal ready",
			"tenant", row.TenantID, "portal", row.Name, "domain", row.Domain)
	}
	return nil
}
