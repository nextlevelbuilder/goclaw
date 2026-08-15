package channelmemory

import (
	"context"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type Worker struct {
	Service *Service
	Ticker  time.Duration
}

func (w *Worker) Start(ctx context.Context) func() {
	if w == nil || w.Service == nil || w.Service.Channels == nil {
		return func() {}
	}
	interval := w.Ticker
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	runCtx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				w.scan(runCtx)
			case <-runCtx.Done():
				return
			}
		}
	}()
	return cancel
}

func (w *Worker) scan(ctx context.Context) {
	instances, err := w.Service.Channels.ListAllEnabled(store.WithCrossTenant(ctx))
	if err != nil {
		slog.Warn("channel_memory.scan.instances", "error", err)
		return
	}
	for _, inst := range instances {
		cfg := ParseConfig(inst.Config)
		if !cfg.Enabled {
			continue
		}
		scoped := store.WithTenantID(ctx, inst.TenantID)
		result, err := w.Service.RunAll(scoped, &inst, "scheduled")
		if err != nil {
			slog.Debug("channel_memory.scan.skip", "channel", inst.Name, "error", err)
			continue
		}
		if result.RunCount > 0 || result.SkippedGroupCount > 0 || result.ErrorCount > 0 {
			slog.Debug("channel_memory.scan.groups",
				"channel", inst.Name,
				"runs", result.RunCount,
				"skipped_groups", result.SkippedGroupCount,
				"errors", result.ErrorCount)
		}
	}
}
