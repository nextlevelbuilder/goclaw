package tracing

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	defaultFlushInterval = 5 * time.Second
	defaultBufferSize    = 1000
	previewMaxLen        = 40_000
	traceRetention       = 7 * 24 * time.Hour // auto-delete traces older than 7 days
	pruneInterval        = 8 * time.Hour
	// staleThreshold: how long a "running" trace must be stuck before the recovery
	// worker marks it as "error". 10min is conservative — the primary sub-second
	// stop visibility is delivered by the trace.status WS event (Phase 4). Stale
	// recovery is a safety net for crashed/orphaned traces. Lowering this further
	// requires a `last_span_at` column so we don't sweep legitimate long-running
	// agents (see plan's Phase 3 unresolved question).
	staleThreshold = 10 * time.Minute
	staleRecoveryPeriod  = 30 * time.Second // new: run periodically instead of once on startup
	retryQueueCap        = 1000
	retryWorkerPeriod    = 5 * time.Second
	retryMaxTries        = 10
)

// TraceStatusPayload is the payload for EventTraceStatusChanged WS events.
// Sent immediately after a status write succeeds (not buffered by flush interval).
type TraceStatusPayload struct {
	TraceID string     `json:"traceId"`
	Status  string     `json:"status"`
	EndedAt *time.Time `json:"endedAt,omitempty"`
}

// StatusBroadcaster is a callback invoked after each successful trace status write.
// Implementations broadcast the payload to connected WS clients in the given tenant.
type StatusBroadcaster func(payload TraceStatusPayload, tenantID uuid.UUID)

// SpanExporter is implemented by backends that receive span data alongside
// the PostgreSQL store (e.g. OpenTelemetry OTLP).  Keeping this as an
// interface lets the OTel dependency live in a separate sub-package that can
// be swapped out by commenting one import line.
type SpanExporter interface {
	ExportSpans(ctx context.Context, spans []store.SpanData)
	Shutdown(ctx context.Context) error
}

// spanUpdate represents a deferred span field update, buffered alongside new
// spans and applied during the same flush cycle (after batch INSERT).
type spanUpdate struct {
	SpanID  uuid.UUID
	TraceID uuid.UUID
	Updates map[string]any
}

// pendingUpdate holds a trace update that failed to persist and is queued for retry.
type pendingUpdate struct {
	TraceID  uuid.UUID
	TenantID uuid.UUID // captured from caller ctx at enqueue time for retryWorker broadcast
	Updates  map[string]any
	Tries    int
	LastTry  time.Time
}

// Collector buffers spans in memory and periodically flushes them to the
// TracingStore in batches. Traces are created synchronously (one per run),
// while spans are buffered for async batch insert.
//
// When a SpanExporter is attached, spans are also exported to an
// external backend (Jaeger, Grafana Tempo, Datadog, etc.).
type Collector struct {
	store store.TracingStore

	spanCh       chan store.SpanData
	spanUpdateCh chan spanUpdate // deferred span updates (two-phase tracing)
	stopCh       chan struct{}
	wg           sync.WaitGroup

	// retryCh buffers trace updates that failed all inline retries.
	// Bounded at retryQueueCap — drop-oldest under sustained DB outage.
	retryCh chan pendingUpdate

	// traces that need aggregate updates on flush
	dirtyTraces   map[uuid.UUID]struct{}
	dirtyTracesMu sync.Mutex

	verbose  bool         // when true, LLM spans include full input messages
	exporter SpanExporter // optional external exporter (nil = disabled)

	// OnFlush is called after each flush cycle with the trace IDs that had
	// their aggregates updated. Used to broadcast realtime trace events.
	OnFlush func(traceIDs []uuid.UUID)

	// broadcastStatus is an optional callback invoked immediately after each
	// successful trace status write (SetTraceStatus / FinishTrace). Fires
	// before the 5s OnFlush tick for low-latency status delivery.
	broadcastStatus StatusBroadcaster
}

// NewCollector creates a new tracing collector backed by the given store.
// Set GOCLAW_TRACE_VERBOSE=1 to include full LLM input in spans.
func NewCollector(ts store.TracingStore) *Collector {
	verbose := os.Getenv("GOCLAW_TRACE_VERBOSE") != ""
	if verbose {
		slog.Info("tracing: verbose mode enabled (GOCLAW_TRACE_VERBOSE)")
	}
	return &Collector{
		store:        ts,
		spanCh:       make(chan store.SpanData, defaultBufferSize),
		spanUpdateCh: make(chan spanUpdate, defaultBufferSize),
		stopCh:       make(chan struct{}),
		retryCh:      make(chan pendingUpdate, retryQueueCap),
		dirtyTraces:  make(map[uuid.UUID]struct{}),
		verbose:      verbose,
	}
}

// Verbose returns true if verbose tracing is enabled (full LLM input logging).
func (c *Collector) Verbose() bool { return c.verbose }

// PreviewMaxLen returns the max preview length: 200K when verbose, 500 otherwise.
func (c *Collector) PreviewMaxLen() int {
	if c.verbose {
		return 200_000
	}
	return previewMaxLen
}

// SetExporter attaches an external span exporter (e.g. OpenTelemetry OTLP).
// When set, spans are exported to the external backend during each flush cycle.
func (c *Collector) SetExporter(exp SpanExporter) {
	c.exporter = exp
}

// SetStatusBroadcaster wires a callback that is invoked immediately after each
// successful trace status write. Called from SetTraceStatus and FinishTrace
// on success, bypassing the 5s OnFlush buffer for low-latency status events.
func (c *Collector) SetStatusBroadcaster(b StatusBroadcaster) {
	c.broadcastStatus = b
}

// Start begins the background flush loop and retry worker.
//
// NOTE: staleRecoveryLoop is intentionally NOT started. The current implementation
// sweeps traces by `start_time`, which would kill legitimate long-running agent
// runs (research chains, large code generation, long shell commands routinely
// exceed 10 minutes). Re-enable only after adding a `last_span_at` column so
// recovery can gate on "no activity for N minutes" instead of "started > N min
// ago". Until then, crashed/orphaned traces may remain `running` in DB — the
// primary abort path (router 2-phase + trace.status WS event) handles the
// common case; this is a safety-net gap we accept over false kills.
func (c *Collector) Start() {
	c.wg.Add(2) // flushLoop + retryWorker (staleRecoveryLoop disabled — see note above)
	go c.flushLoop()
	go c.retryWorker()
	// go c.staleRecoveryLoop() // disabled: would kill healthy long runs. See Start() godoc.
	slog.Info("tracing collector started")
}

// keep staleRecoveryLoop reachable to silence "unused" linter; re-enabled in
// Start() once last_span_at-based recovery lands.
var _ = (*Collector).staleRecoveryLoop

// Stop gracefully shuts down the collector, flushing remaining spans.
func (c *Collector) Stop() {
	close(c.stopCh)
	c.wg.Wait()

	// Shutdown external exporter (flushes remaining spans)
	if c.exporter != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.exporter.Shutdown(ctx); err != nil {
			slog.Warn("tracing: span exporter shutdown failed", "error", err)
		}
	}

	slog.Info("tracing collector stopped")
}

// CreateTrace synchronously creates a trace record.
func (c *Collector) CreateTrace(ctx context.Context, trace *store.TraceData) error {
	return c.store.CreateTrace(ctx, trace)
}

// UpdateTrace synchronously updates a trace record.
func (c *Collector) UpdateTrace(ctx context.Context, traceID uuid.UUID, updates map[string]any) error {
	return c.store.UpdateTrace(ctx, traceID, updates)
}

// EmitSpan enqueues a span for async batch insertion.
// Non-blocking: drops the span if the buffer is full.
func (c *Collector) EmitSpan(span store.SpanData) {
	if span.ID == uuid.Nil {
		span.ID = store.GenNewID()
	}
	if span.CreatedAt.IsZero() {
		span.CreatedAt = time.Now().UTC()
	}

	select {
	case c.spanCh <- span:
		c.markDirty(span.TraceID)
	default:
		slog.Warn("tracing: span buffer full, dropping span",
			"span_type", span.SpanType, "name", span.Name)
	}
}

// EmitSpanUpdate enqueues a deferred update for an existing span.
// Used by two-phase tracing: a "running" span is emitted via EmitSpan before
// execution starts, then updated via EmitSpanUpdate when execution completes.
// Non-blocking channel send — safe to call even after ctx cancellation.
func (c *Collector) EmitSpanUpdate(spanID, traceID uuid.UUID, updates map[string]any) {
	select {
	case c.spanUpdateCh <- spanUpdate{SpanID: spanID, TraceID: traceID, Updates: updates}:
		c.markDirty(traceID)
	default:
		slog.Warn("tracing: span update buffer full, dropping update",
			"span_id", spanID)
	}
}

// SetTraceStatus updates only the trace status and marks it dirty for re-aggregation.
// Uses a detached context with retries to survive caller context cancellation.
func (c *Collector) SetTraceStatus(ctx context.Context, traceID uuid.UUID, status string) {
	updates := map[string]any{"status": status}
	if c.updateTraceWithRetry(ctx, traceID, updates) {
		c.markDirty(traceID)
	}
}

// FinishTrace marks a trace as completed and schedules aggregate update.
// Uses a detached context with retries to survive caller context cancellation (e.g. abort path).
func (c *Collector) FinishTrace(ctx context.Context, traceID uuid.UUID, status string, errMsg string, outputPreview string) {
	now := time.Now().UTC()
	updates := map[string]any{
		"status":   status,
		"end_time": now,
	}
	if errMsg != "" {
		updates["error"] = errMsg
	}
	if outputPreview != "" {
		updates["output_preview"] = c.truncatePreviewStr(outputPreview)
	}
	if c.updateTraceWithRetry(ctx, traceID, updates) {
		c.markDirty(traceID)
	}
}

// updateTraceWithRetry persists trace updates with a detached ctx + 3 inline retries.
// Uses context.WithoutCancel to preserve tenant/locale values while detaching cancellation.
// On final failure, enqueues to the retry queue. Returns true if persisted inline.
func (c *Collector) updateTraceWithRetry(ctx context.Context, traceID uuid.UUID, updates map[string]any) bool {
	detached := context.WithoutCancel(ctx)
	backoffs := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 300 * time.Millisecond}
	for attempt := 0; attempt <= len(backoffs); attempt++ {
		opCtx, cancel := context.WithTimeout(detached, 5*time.Second)
		err := c.store.UpdateTrace(opCtx, traceID, updates)
		cancel()
		if err == nil {
			c.emitStatusBroadcast(ctx, traceID, updates)
			return true
		}
		slog.Warn("tracing: UpdateTrace attempt failed",
			"trace_id", traceID, "attempt", attempt+1, "error", err)
		if attempt < len(backoffs) {
			time.Sleep(backoffs[attempt])
		}
	}
	// All inline retries failed — enqueue for background worker
	c.enqueueRetry(ctx, traceID, updates)
	return false
}

// enqueueRetry pushes a failed update to the bounded retry channel.
// ctx is used to capture the tenant ID so the retryWorker can broadcast with correct tenant.
// Drop-oldest behavior when the queue is full to prevent unbounded growth.
func (c *Collector) enqueueRetry(ctx context.Context, traceID uuid.UUID, updates map[string]any) {
	item := pendingUpdate{
		TraceID:  traceID,
		TenantID: store.TenantIDFromContext(ctx),
		Updates:  updates,
		Tries:    0,
	}
	select {
	case c.retryCh <- item:
	default:
		// queue full — drop oldest, push new
		select {
		case dropped := <-c.retryCh:
			slog.Warn("tracing: retry queue full, dropped oldest",
				"dropped_trace_id", dropped.TraceID, "new_trace_id", traceID)
		default:
		}
		c.retryCh <- item
	}
}

// retryWorker drains the retry channel every 5 seconds and re-attempts
// failed trace updates. Items exceeding retryMaxTries are dropped with an error log.
func (c *Collector) retryWorker() {
	defer c.wg.Done()
	ticker := time.NewTicker(retryWorkerPeriod)
	defer ticker.Stop()
	var pending []pendingUpdate
	for {
		select {
		case <-c.stopCh:
			return
		case item := <-c.retryCh:
			pending = append(pending, item)
		case <-ticker.C:
			// drain any newly arrived items
			draining := true
			for draining {
				select {
				case item := <-c.retryCh:
					pending = append(pending, item)
				default:
					draining = false
				}
			}
			// attempt all pending updates
			kept := pending[:0]
			for _, p := range pending {
				// Restore tenant scope so UpdateTrace and broadcast use correct tenant.
				baseCtx := context.Background()
				if p.TenantID != uuid.Nil {
					baseCtx = store.WithTenantID(baseCtx, p.TenantID)
				}
				opCtx, cancel := context.WithTimeout(baseCtx, 5*time.Second)
				err := c.store.UpdateTrace(opCtx, p.TraceID, p.Updates)
				cancel()
				if err == nil {
					c.markDirty(p.TraceID)
					c.emitStatusBroadcast(baseCtx, p.TraceID, p.Updates)
					continue
				}
				p.Tries++
				p.LastTry = time.Now()
				if p.Tries < retryMaxTries {
					kept = append(kept, p)
				} else {
					slog.Error("tracing: retry exhausted, dropping update",
						"trace_id", p.TraceID, "tries", p.Tries)
				}
			}
			pending = kept
		}
	}
}

func (c *Collector) markDirty(traceID uuid.UUID) {
	c.dirtyTracesMu.Lock()
	c.dirtyTraces[traceID] = struct{}{}
	c.dirtyTracesMu.Unlock()
}

func (c *Collector) flushLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(defaultFlushInterval)
	defer ticker.Stop()

	pruneTicker := time.NewTicker(pruneInterval)
	defer pruneTicker.Stop()

	// Run initial prune shortly after startup.
	go c.pruneOldTraces()

	for {
		select {
		case <-ticker.C:
			c.flush()
		case <-pruneTicker.C:
			c.pruneOldTraces()
		case <-c.stopCh:
			c.flush()
			return
		}
	}
}

// staleRecoveryLoop runs recoverStaleOnce immediately on startup, then every
// staleRecoveryPeriod (30s). Replaces the one-shot recoverStaleTraces call.
func (c *Collector) staleRecoveryLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(staleRecoveryPeriod)
	defer ticker.Stop()
	// run immediately on startup too
	c.recoverStaleOnce()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.recoverStaleOnce()
		}
	}
}

// recoverStaleOnce marks "running" traces whose start_time is older than
// staleThreshold (10 min) as "error". Also recovers stuck spans.
//
// NOTE: Both PG and SQLite implementations use start_time < cutoff, not last
// activity time. Follow-up: gate on "no spans in last N min" instead (requires
// a last_span_at schema column). Tracked as an open question.
func (c *Collector) recoverStaleOnce() {
	cutoff := time.Now().UTC().Add(-staleThreshold)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	recovered, err := c.store.RecoverStaleRunningTraces(ctx, cutoff)
	if err != nil {
		slog.Warn("tracing: stale recovery failed", "error", err)
		return
	}
	if recovered > 0 {
		slog.Info("tracing: recovered stale running traces",
			"count", recovered, "older_than", cutoff.Format(time.RFC3339))
	}
}

// pruneOldTraces deletes traces and spans older than traceRetention.
func (c *Collector) pruneOldTraces() {
	cutoff := time.Now().UTC().Add(-traceRetention)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	deleted, err := c.store.DeleteTracesOlderThan(ctx, cutoff)
	if err != nil {
		slog.Warn("tracing: prune old traces failed", "error", err)
		return
	}
	if deleted > 0 {
		slog.Info("tracing: pruned old traces", "deleted", deleted, "older_than", cutoff.Format(time.RFC3339))
	}
}

func (c *Collector) flush() {
	// Drain span channel
	var spans []store.SpanData
	for {
		select {
		case span := <-c.spanCh:
			spans = append(spans, span)
		default:
			goto done
		}
	}
done:

	if len(spans) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := c.store.BatchCreateSpans(ctx, spans); err != nil {
			slog.Warn("tracing: batch span insert failed", "count", len(spans), "error", err)
		} else {
			slog.Debug("tracing: flushed spans", "count", len(spans))
		}

		// Export to external backend (non-blocking — errors logged, not propagated)
		if c.exporter != nil {
			c.exporter.ExportSpans(ctx, spans)
		}
	}

	// Drain and apply deferred span updates (two-phase tracing).
	// Must run AFTER batch insert so that "running" spans exist before we UPDATE them.
	var updates []spanUpdate
	for {
		select {
		case u := <-c.spanUpdateCh:
			updates = append(updates, u)
		default:
			goto doneUpdates
		}
	}
doneUpdates:
	if len(updates) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, u := range updates {
			if err := c.store.UpdateSpan(ctx, u.SpanID, u.Updates); err != nil {
				slog.Warn("tracing: span update failed", "span_id", u.SpanID, "error", err)
			}
		}
		slog.Debug("tracing: applied span updates", "count", len(updates))
	}

	// Update aggregates for dirty traces
	c.dirtyTracesMu.Lock()
	dirty := c.dirtyTraces
	c.dirtyTraces = make(map[uuid.UUID]struct{})
	c.dirtyTracesMu.Unlock()

	if len(dirty) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		for traceID := range dirty {
			if err := c.store.BatchUpdateTraceAggregates(ctx, traceID); err != nil {
				slog.Warn("tracing: aggregate update failed", "trace_id", traceID, "error", err)
			}
		}

		// Notify listeners about updated traces (realtime WS push).
		if c.OnFlush != nil {
			ids := make([]uuid.UUID, 0, len(dirty))
			for id := range dirty {
				ids = append(ids, id)
			}
			c.OnFlush(ids)
		}
	}
}

// emitStatusBroadcast fires broadcastStatus if set and the updates map contains a "status" key.
// Extracts optional "end_time" from updates for the EndedAt field.
func (c *Collector) emitStatusBroadcast(ctx context.Context, traceID uuid.UUID, updates map[string]any) {
	if c.broadcastStatus == nil {
		return
	}
	status, _ := updates["status"].(string)
	if status == "" {
		return
	}
	var endedAt *time.Time
	if t, ok := updates["end_time"].(time.Time); ok {
		endedAt = &t
	}
	c.broadcastStatus(TraceStatusPayload{
		TraceID: traceID.String(),
		Status:  status,
		EndedAt: endedAt,
	}, store.TenantIDFromContext(ctx))
}

// truncatePreviewStr sanitizes and truncates a string by removing the middle.
func (c *Collector) truncatePreviewStr(s string) string {
	return TruncateMid(s, c.PreviewMaxLen())
}
