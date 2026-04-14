package langsmithexport

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	langsmith "github.com/langchain-ai/langsmith-go"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

const (
	// pendingTTL is how long we keep "running" spans in the pending map
	// before cleaning them up as orphans.
	pendingTTL       = 5 * time.Minute
	pendingPruneFreq = 2 * time.Minute
)

// pendingEntry stores metadata needed when a root span's two-phase update arrives.
// Only used for root spans; child spans use childBuf instead.
type pendingEntry struct {
	createdAt      time.Time
	dottedOrder    string
	langsmithRunID uuid.UUID // may differ from span ID (root spans use TraceID)
}

// runInfo tracks a LangSmith run's remapped ID and dotted_order so child
// spans can build proper hierarchy and parent references.
type runInfo struct {
	langsmithID uuid.UUID
	dottedOrder string
	createdAt   time.Time
}

// childBufEntry stores a pre-built RunCreate for a child "running" span.
// Instead of sending POST+PATCH (which fails when PATCH arrives in a
// different sink batch without parent_run_id), we buffer until the
// update arrives, then send a single complete POST.
type childBufEntry struct {
	rc        *langsmith.RunCreate
	createdAt time.Time
}

// Config configures the LangSmith exporter.
type Config struct {
	APIKey  string // required
	Project string // default: "default"
	APIUrl  string // default: LangSmith cloud
}

// Exporter sends GoClaw spans to LangSmith as runs via the multipart
// ingestion API. Implements tracing.SpanExporter and tracing.SpanUpdateExporter.
type Exporter struct {
	client *langsmith.TracingClient

	// pending tracks root "running" spans (two-phase POST+PATCH).
	// Key: GoClaw spanID, value: pendingEntry.
	pending sync.Map

	// childBuf buffers child "running" spans for deferred single-shot POST.
	// Key: GoClaw spanID, value: childBufEntry.
	childBuf sync.Map

	// runMap tracks every emitted run's LangSmith ID and dotted_order so
	// child spans arriving in later batches can build proper hierarchy.
	// Key: GoClaw spanID, value: runInfo.
	runMap sync.Map

	pruneOnce sync.Once
	pruneWg   sync.WaitGroup
	stopCh    chan struct{}
}

// New creates a LangSmith exporter with the given config.
func New(cfg Config) (*Exporter, error) {
	if cfg.Project == "" {
		cfg.Project = "default"
	}

	opts := []langsmith.TracingOption{
		langsmith.WithTracingAPIKey(cfg.APIKey),
		langsmith.WithTracingProject(cfg.Project),
	}
	if cfg.APIUrl != "" {
		opts = append(opts, langsmith.WithTracingAPIURL(cfg.APIUrl))
	}

	client, err := langsmith.NewTracingClient(context.Background(), opts...)
	if err != nil {
		return nil, err
	}

	return &Exporter{
		client: client,
		stopCh: make(chan struct{}),
	}, nil
}

// ExportSpans converts GoClaw SpanData to LangSmith runs and sends them.
// Called by the Collector during flush alongside the PostgreSQL batch insert.
func (e *Exporter) ExportSpans(ctx context.Context, spans []store.SpanData) {
	if e == nil || len(spans) == 0 {
		return
	}

	e.startPruneLoop()

	// First pass: register root span ID mappings so children in the same
	// batch can resolve their parent's LangSmith ID and dotted_order.
	for _, s := range spans {
		if s.ParentSpanID == nil && s.ID != s.TraceID {
			dottedOrder := formatDottedOrder(s.StartTime, s.TraceID)
			e.runMap.Store(s.ID, runInfo{
				langsmithID: s.TraceID,
				dottedOrder: dottedOrder,
				createdAt:   time.Now(),
			})
		}
	}

	for _, s := range spans {
		// Resolve parent's LangSmith ID and dotted_order for hierarchy.
		var parentDottedOrder string
		var parentLangsmithID *uuid.UUID
		if s.ParentSpanID != nil {
			if ri, ok := e.runMap.Load(*s.ParentSpanID); ok {
				info := ri.(runInfo)
				parentDottedOrder = info.dottedOrder
				parentLangsmithID = &info.langsmithID
			}
		}

		rc := spanToRunCreate(s, parentDottedOrder, parentLangsmithID)

		// Track this run for future children and two-phase updates.
		e.runMap.Store(s.ID, runInfo{
			langsmithID: rc.ID,
			dottedOrder: rc.DottedOrder,
			createdAt:   time.Now(),
		})

		if isRunningSpan(s) {
			if s.ParentSpanID != nil {
				// Child running span: buffer for deferred single-shot POST.
				// Standalone PATCHes for children fail LangSmith validation
				// because RunUpdate has no ParentRunID field.
				e.childBuf.Store(s.ID, childBufEntry{
					rc:        rc,
					createdAt: time.Now(),
				})
				continue
			}
			// Root running span: two-phase POST+PATCH is safe
			// (run_id == trace_id, single-part dotted_order).
			e.pending.Store(s.ID, pendingEntry{
				createdAt:      time.Now(),
				dottedOrder:    rc.DottedOrder,
				langsmithRunID: rc.ID,
			})
		}

		if err := e.client.CreateRun(rc); err != nil {
			slog.Warn("langsmith: failed to create run",
				"span_id", s.ID, "name", s.Name, "error", err)
		}
	}
}

// ExportSpanUpdates converts deferred span updates to LangSmith RunUpdates.
// Called by the Collector after DB span updates are applied.
func (e *Exporter) ExportSpanUpdates(ctx context.Context, updates []tracing.SpanUpdate) {
	if e == nil || len(updates) == 0 {
		return
	}

	for _, u := range updates {
		// Buffered child span: apply update and send a single complete POST.
		if entry, ok := e.childBuf.LoadAndDelete(u.SpanID); ok {
			if cb, ok := entry.(childBufEntry); ok {
				applySpanUpdate(cb.rc, u)
				if err := e.client.CreateRun(cb.rc); err != nil {
					slog.Warn("langsmith: failed to create completed child run",
						"span_id", u.SpanID, "error", err)
				}
				continue
			}
		}

		// Root span: send PATCH (run_id == trace_id, single-part DO → valid).
		ru := spanUpdateToRunUpdate(u)
		if entry, ok := e.pending.LoadAndDelete(u.SpanID); ok {
			if pe, ok := entry.(pendingEntry); ok {
				ru.DottedOrder = pe.dottedOrder
				if pe.langsmithRunID != uuid.Nil {
					ru.ID = pe.langsmithRunID
				}
			}
		}

		if err := e.client.UpdateRun(ru); err != nil {
			slog.Warn("langsmith: failed to update run",
				"span_id", u.SpanID, "error", err)
		}
	}
}

// Shutdown gracefully shuts down the LangSmith exporter, flushing remaining runs.
func (e *Exporter) Shutdown(ctx context.Context) error {
	if e == nil {
		return nil
	}
	close(e.stopCh)
	e.pruneWg.Wait()
	slog.Info("langsmith exporter shutting down")
	e.client.Close()
	return nil
}

// startPruneLoop starts a background goroutine (once) to clean up orphaned
// pending entries that never received an update.
func (e *Exporter) startPruneLoop() {
	e.pruneOnce.Do(func() {
		e.pruneWg.Add(1)
		go func() {
			defer e.pruneWg.Done()
			ticker := time.NewTicker(pendingPruneFreq)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					e.pruneStale()
				case <-e.stopCh:
					return
				}
			}
		}()
	})
}

// pruneStale removes expired entries from pending, childBuf, and runMap.
// Orphaned child buffers are flushed as incomplete runs (better than lost).
func (e *Exporter) pruneStale() {
	cutoff := time.Now().Add(-pendingTTL)
	var pruned int
	e.pending.Range(func(key, value any) bool {
		if pe, ok := value.(pendingEntry); ok && pe.createdAt.Before(cutoff) {
			e.pending.Delete(key)
			pruned++
		}
		return true
	})
	// Flush orphaned child buffers as incomplete runs.
	e.childBuf.Range(func(key, value any) bool {
		if cb, ok := value.(childBufEntry); ok && cb.createdAt.Before(cutoff) {
			e.childBuf.Delete(key)
			if err := e.client.CreateRun(cb.rc); err != nil {
				slog.Warn("langsmith: failed to flush orphaned child run", "error", err)
			}
			pruned++
		}
		return true
	})
	e.runMap.Range(func(key, value any) bool {
		if ri, ok := value.(runInfo); ok && ri.createdAt.Before(cutoff) {
			e.runMap.Delete(key)
		}
		return true
	})
	if pruned > 0 {
		slog.Debug("langsmith: pruned stale entries", "count", pruned)
	}
}

// Compile-time interface checks.
var (
	_ tracing.SpanExporter       = (*Exporter)(nil)
	_ tracing.SpanUpdateExporter = (*Exporter)(nil)
)
