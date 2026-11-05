package pricing

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const DefaultOpenRouterCatalogSyncInterval = 24 * time.Hour

func StartOpenRouterCatalogAutoSync(ctx context.Context, s store.UsageCapStore, interval time.Duration, onSync ...func(context.Context, int)) {
	if s == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultOpenRouterCatalogSyncInterval
	}
	client := &http.Client{Timeout: 30 * time.Second}
	go func() {
		syncOpenRouterCatalog(ctx, s, client, firstOpenRouterSyncHook(onSync))
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				syncOpenRouterCatalog(ctx, s, client, firstOpenRouterSyncHook(onSync))
			}
		}
	}()
}

func firstOpenRouterSyncHook(hooks []func(context.Context, int)) func(context.Context, int) {
	if len(hooks) == 0 {
		return nil
	}
	return hooks[0]
}

func syncOpenRouterCatalog(ctx context.Context, s store.UsageCapStore, client *http.Client, onSync func(context.Context, int)) {
	syncCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	entries, err := FetchOpenRouterCatalog(syncCtx, client)
	if err != nil {
		slog.Warn("usage_pricing.openrouter_auto_sync_failed", "error", err)
		return
	}
	count, err := s.UpsertPricingCatalog(syncCtx, entries)
	if err != nil {
		slog.Warn("usage_pricing.openrouter_auto_store_failed", "error", err)
		return
	}
	if count < len(entries) {
		// Partial imports are expected when the upstream payload carries an
		// unusable row; the store logs each skipped entry (Issue #1605).
		slog.Warn("usage_pricing.openrouter_auto_sync_partial",
			"upserted", count, "fetched", len(entries))
	}
	slog.Info("usage_pricing.openrouter_auto_synced", "count", count)
	if onSync != nil {
		onSync(ctx, count)
	}
}
