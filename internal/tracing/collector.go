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
)

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

	// traces that need aggregate updates on flush
	dirtyTraces   map[uuid.UUID]struct{}
	dirtyTracesMu sync.Mutex

	verbose  bool         // when true, LLM spans include full input messages
	exporter SpanExporter // optional external exporter (nil = disabled)

	// OnFlush is called after each flush cycle with the trace IDs that had
	// their aggregates updated. Used to broadcast realtime trace events.
	OnFlush func(traceIDs []uuid.UUID)
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

// Start begins the background flush loop.
func (c *Collector) Start() {
	c.wg.Add(1)
	go c.flushLoop()
	go c.recoverStaleTraces() // fix orphan traces from previous crash
	slog.Info("tracing collector started")
}

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
// Used by child trace runs (e.g. announce) to toggle the parent trace back to
// "running" while the child is active, then "completed" when done.
func (c *Collector) SetTraceStatus(ctx context.Context, traceID uuid.UUID, status string) {
	if err := c.store.UpdateTrace(ctx, traceID, map[string]any{"status": status}); err != nil {
		slog.Warn("tracing: failed to set trace status", "trace_id", traceID, "error", err)
	}
	c.markDirty(traceID)
}

// FinishTrace marks a trace as completed and schedules aggregate update.
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
	if err := c.store.UpdateTrace(ctx, traceID, updates); err != nil {
		slog.Warn("tracing: failed to finish trace", "trace_id", traceID, "error", err)
	}
	c.markDirty(traceID)
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

// recoverStaleTraces marks "running" traces older than 30 min as "error".
// Called once on startup to fix orphans left by a previous crash or stuck goroutine.
func (c *Collector) recoverStaleTraces() {
	const staleThreshold = 30 * time.Minute
	cutoff := time.Now().UTC().Add(-staleThreshold)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	recovered, err := c.store.RecoverStaleRunningTraces(ctx, cutoff)
	if err != nil {
		slog.Warn("tracing: failed to recover stale running traces", "error", err)
		return
	}
	if recovered > 0 {
		slog.Info("tracing: recovered stale running traces on startup",
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

// truncatePreviewStr sanitizes and truncates a string by removing the middle.
func (c *Collector) truncatePreviewStr(s string) string {
	return TruncateMid(s, c.PreviewMaxLen())
}
