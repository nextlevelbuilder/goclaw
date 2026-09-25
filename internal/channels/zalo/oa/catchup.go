package oa

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	// catchUpStaleThreshold gates the sweep so a fresh restart doesn't
	// re-fetch on every boot.
	catchUpStaleThreshold = 30 * time.Minute
	catchUpPageSize       = 50
)

// runCatchUpSweep recovers messages possibly missed during downtime.
// Single bounded page, error-tolerant. Reuses the polling dedup path so
// overlap with webhook deliveries is harmless.
func (c *Channel) runCatchUpSweep(parentCtx context.Context) {
	ctx := store.WithTenantID(parentCtx, c.TenantID())

	last := c.cursor.LastSeenTimestamp()
	if last != 0 && time.Since(time.UnixMilli(last)) < catchUpStaleThreshold {
		return
	}

	msgs, err := c.listRecentChat(ctx, 0, catchUpPageSize)
	if err != nil {
		slog.Warn("zalo_oa.webhook.catchup_failed", "err", err)
		return
	}
	sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].Time < msgs[j].Time })

	dispatched := 0
	for _, m := range msgs {
		if m.FromID == "" || m.FromID == c.creds().OAID {
			continue
		}
		if m.Time != 0 {
			if m.Time <= c.cursor.Get(m.FromID) {
				continue
			}
		} else if m.MessageID == "" || c.seenIDs.SeenOrAdd(m.MessageID) {
			continue
		}
		c.dispatchInbound(m)
		if m.Time != 0 {
			c.cursor.Advance(m.FromID, m.Time)
		}
		dispatched++
	}
	slog.Info("zalo_oa.webhook.catchup_done", "fetched", len(msgs), "dispatched", dispatched)
}
