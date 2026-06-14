package meow

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const syncDateLayout = "2006-01-02"

// SyncWorker runs the inbound Sheets → Postgres sync on a schedule: per tab it
// reads rows, plans actions, deduplicates against the DB (so re-runs never create
// duplicate posts), applies ingest + approve through the existing validated path,
// and writes results back to the sheet. It NEVER publishes to Telegram — the
// publish path stays separate/manual.
type SyncWorker struct {
	Store              store.MeowStore
	Client             SheetClient
	TenantID           uuid.UUID
	InboxRoot          string
	AssetRoot          string
	ApprovedByFallback string
	Tabs               []string // brand_key tabs to poll; empty → every channel
}

// Run polls every interval until ctx is cancelled, running once immediately so a
// freshly-enabled worker syncs without waiting a full interval.
func (w *SyncWorker) Run(ctx context.Context, interval time.Duration) {
	w.tick(ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

// tick runs one sync pass, recovering from any panic so a malformed row or API
// hiccup can never crash the gateway.
func (w *SyncWorker) tick(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("meow.sheets.sync panic", "recover", r)
		}
	}()
	rep, err := w.SyncOnce(ctx)
	if err != nil {
		slog.Warn("meow.sheets.sync pass failed", "error", err)
		return
	}
	if rep.Ingested+rep.Approved+rep.Held+rep.Errored > 0 {
		slog.Info("meow.sheets.sync",
			"ingested", rep.Ingested, "approved", rep.Approved,
			"skipped", rep.Skipped, "held", rep.Held, "errored", rep.Errored)
	}
}

// SyncOnce performs one full sync pass across the configured tabs and returns the
// aggregate report. A per-tab read/list/write failure is logged and skipped
// without aborting the other tabs.
func (w *SyncWorker) SyncOnce(ctx context.Context) (SyncReport, error) {
	tctx := store.WithTenantID(ctx, w.TenantID)
	channels, err := w.Store.ListChannels(tctx, w.TenantID)
	if err != nil {
		return SyncReport{}, fmt.Errorf("list channels: %w", err)
	}
	byBrand := make(map[string]store.MpChannel, len(channels))
	for _, ch := range channels {
		byBrand[ch.BrandKey] = ch
	}
	resolveHandle := func(tab string) (string, bool) {
		ch, ok := byBrand[tab]
		return ch.Handle, ok
	}

	tabs := w.Tabs
	if len(tabs) == 0 {
		for _, ch := range channels {
			tabs = append(tabs, ch.BrandKey)
		}
	}

	var agg SyncReport
	for _, tab := range tabs {
		ch, ok := byBrand[tab]
		if !ok {
			slog.Warn("meow.sheets.sync unknown tab", "tab", tab)
			continue
		}
		agg.merge(w.syncTab(tctx, tab, ch, resolveHandle))
	}
	return agg, nil
}

// syncTab syncs one tab: read → plan → DB-dedup → apply → write back.
func (w *SyncWorker) syncTab(ctx context.Context, tab string, ch store.MpChannel, resolve func(string) (string, bool)) SyncReport {
	rows, err := w.Client.ReadTab(ctx, tab)
	if err != nil {
		slog.Warn("meow.sheets.sync read failed", "tab", tab, "error", err)
		return SyncReport{}
	}
	actions := PlanSync(rows, resolve)

	existing, err := w.Store.ListPostsByChannel(ctx, w.TenantID, ch.ID)
	if err != nil {
		slog.Warn("meow.sheets.sync list posts failed", "tab", tab, "error", err)
		return SyncReport{}
	}
	synced := syncedDates(existing)

	// DB pre-check makes the sync idempotent regardless of sheet write-back
	// success: a channel-day that already has a non-draft post is reconciled (its
	// DB status written back to the sheet) instead of re-ingested.
	toApply := make([]SyncAction, 0, len(actions))
	var reconciled []SyncOutcome
	for _, a := range actions {
		if a.Kind == SyncIngest {
			if st, done := synced[a.Date]; done {
				reconciled = append(reconciled, SyncOutcome{
					Tab: a.Tab, Date: a.Date, RowIndex: a.RowIndex, Result: RowResult{Status: st},
				})
				continue
			}
		}
		toApply = append(toApply, a)
	}

	rep := ApplySync(ctx, w.Store, w.TenantID, toApply, w.InboxRoot, w.AssetRoot, w.ApprovedByFallback)
	rep.Skipped += len(reconciled)
	rep.Outcomes = append(rep.Outcomes, reconciled...)

	for _, o := range rep.Outcomes {
		if err := w.Client.WriteBack(ctx, o.Tab, o.RowIndex, o.Result); err != nil {
			slog.Warn("meow.sheets.sync writeback failed", "tab", o.Tab, "row", o.RowIndex, "error", err)
		}
	}
	return rep
}

// syncedDates maps a channel's already-synced dates (any non-draft post) to that
// post's status, keyed by YYYY-MM-DD to match a sheet row's date.
func syncedDates(posts []store.MpContentPost) map[string]string {
	out := make(map[string]string, len(posts))
	for _, p := range posts {
		if p.Status == store.MpPostDraft {
			continue
		}
		out[p.ScheduledDate.Format(syncDateLayout)] = p.Status
	}
	return out
}

func (r *SyncReport) merge(o SyncReport) {
	r.Ingested += o.Ingested
	r.Approved += o.Approved
	r.Skipped += o.Skipped
	r.Held += o.Held
	r.Errored += o.Errored
	r.Outcomes = append(r.Outcomes, o.Outcomes...)
}
