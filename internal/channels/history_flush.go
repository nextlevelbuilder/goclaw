package channels

import (
	"context"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// StartFlusher starts the background DB flush goroutine. No-op if RAM-only.
func (ph *PendingHistory) StartFlusher() {
	if ph.store == nil {
		return
	}
	go ph.flushLoop()
}

// StopFlusher stops the background flusher and flushes remaining buffer. No-op if RAM-only.
func (ph *PendingHistory) StopFlusher() {
	if ph.store == nil {
		return
	}
	close(ph.stopCh)
	<-ph.stopped
}

// enqueueFlush adds a message to the flush buffer and signals the flusher if full.
func (ph *PendingHistory) enqueueFlush(msg store.PendingMessage) {
	ph.flushMu.Lock()
	ph.flushBuf = append(ph.flushBuf, msg)
	shouldSignal := len(ph.flushBuf) >= flushBatchMax
	ph.flushMu.Unlock()

	if shouldSignal {
		ph.signalFlush()
	}
}

// removeFromFlushBuf removes all pending flushes for a given history key.
func (ph *PendingHistory) removeFromFlushBuf(historyKey string) {
	ph.flushMu.Lock()
	defer ph.flushMu.Unlock()

	filtered := ph.flushBuf[:0]
	for _, msg := range ph.flushBuf {
		if msg.HistoryKey != historyKey {
			filtered = append(filtered, msg)
		}
	}
	ph.flushBuf = filtered
}

// flushNow force-flushes the buffer to DB synchronously. Used before compaction.
func (ph *PendingHistory) flushNow() {
	ph.flushMu.Lock()
	batch := ph.flushBuf
	ph.flushBuf = nil
	ph.flushMu.Unlock()

	if len(batch) == 0 {
		return
	}
	ph.writeBatch(batch)
}

// signalFlush non-blocking signal to the flusher goroutine.
func (ph *PendingHistory) signalFlush() {
	select {
	case ph.flushSignal <- struct{}{}:
	default:
	}
}

// flushLoop runs in background, flushing buffer periodically or when signaled.
// Also runs periodic compaction sweep as a safety net for post-restart scenarios.
func (ph *PendingHistory) flushLoop() {
	defer close(ph.stopped)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	compactTicker := time.NewTicker(compactSweepInterval)
	defer compactTicker.Stop()

	for {
		select {
		case <-ph.stopCh:
			ph.flushNow() // final flush on shutdown
			return
		case <-ph.flushSignal:
			ph.flushNow()
		case <-ticker.C:
			ph.flushNow()
		case <-compactTicker.C:
			ph.sweepCompaction()
		}
	}
}

// writeBatch writes a batch of messages to the DB store.
func (ph *PendingHistory) writeBatch(batch []store.PendingMessage) {
	ctx, cancel := context.WithTimeout(ph.tenantCtx(), 15*time.Second)
	defer cancel()

	if err := ph.store.AppendBatch(ctx, batch); err != nil {
		slog.Warn("pending_history.flush_failed",
			"channel", ph.channelName,
			"batch_size", len(batch),
			"error", err,
		)
	}
}
