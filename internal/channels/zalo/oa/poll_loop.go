package oa

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// runPollLoop runs a polling cycle on each tick; ErrRateLimit switches
// to rate-limit ticker until a clean cycle. Cursor flushes are debounced.
// Early-returns on webhook transport so a regression can't double-dispatch.
func (c *Channel) runPollLoop(parentCtx context.Context) {
	defer c.pollWG.Done()
	if c.cfg.Transport == "webhook" {
		slog.Info("zalo_oa.poll.skipped_for_webhook_transport", "name", c.Name())
		return
	}

	t := time.NewTicker(c.pollInterval)
	defer t.Stop()
	flush := time.NewTicker(cursorFlushInterval)
	defer flush.Stop()

	rateLimited := false
	pollCtx := store.WithTenantID(parentCtx, c.TenantID())

	for {
		select {
		case <-c.stopCh:
			c.flushCursorOnExit(pollCtx)
			return
		case <-flush.C:
			if c.cursor.IsDirty() {
				if err := c.flushCursor(pollCtx); err != nil {
					slog.Warn("zalo_oa.poll.cursor_flush_failed", "error", err)
				}
			}
		case <-t.C:
			// Cycle ctx outlives the HTTP client timeout (30s) so errors
			// surface their real cause, not "context deadline exceeded".
			cycleCtx, cancel := context.WithTimeout(pollCtx, 45*time.Second)
			err := c.pollOnce(cycleCtx)
			cancel()
			switch {
			case errors.Is(err, ErrRateLimit):
				if !rateLimited {
					c.MarkDegraded("rate limited", err.Error(), channels.ChannelFailureKindNetwork, true)
					t.Reset(rateLimitBackoff)
					rateLimited = true
				}
			case err != nil:
				slog.Warn("zalo_oa.poll_failed", "oa_id", c.creds().OAID, "error", err)
				// Auth errors after pollOnce's retry-once-on-auth mean the
				// operator must re-consent.
				c.markAuthFailedIfNeeded(err)
			default:
				if rateLimited {
					c.MarkHealthy("polling")
					t.Reset(c.pollInterval)
					rateLimited = false
				}
			}
		}
	}
}

// flushCursor persists the cursor via SQL JSONB merge so a sibling-key
// update from the UI (e.g. dm_policy) isn't clobbered by a read-modify-write.
func (c *Channel) flushCursor(ctx context.Context) error {
	if c.ciStore == nil || c.instanceID == [16]byte{} {
		return errors.New("zalo_oa: cursor flush without store/instance ID")
	}
	snapshot := c.cursor.Snapshot()
	// Guard against total LRU eviction wiping the persisted cursor:
	// MergeConfig is shallow merge, so {"poll_cursor":{}} would clobber.
	if len(snapshot) == 0 {
		c.cursor.ClearDirty()
		return nil
	}
	patch := map[string]any{configCursorKey: snapshot}
	if err := c.ciStore.MergeConfig(ctx, c.instanceID, patch); err != nil {
		return fmt.Errorf("merge cursor into config: %w", err)
	}
	c.cursor.ClearDirty()
	return nil
}

// flushCursorOnExit is best-effort persistence at Stop.
func (c *Channel) flushCursorOnExit(parentCtx context.Context) {
	if !c.cursor.IsDirty() {
		return
	}
	ctx, cancel := context.WithTimeout(parentCtx, 5*time.Second)
	defer cancel()
	if err := c.flushCursor(ctx); err != nil {
		slog.Warn("zalo_oa.poll.cursor_flush_on_exit_failed", "error", err)
	}
}
