package pipeline

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

type spanRecorder struct {
	store.TracingStore

	mu    sync.Mutex
	spans []store.SpanData
}

func (r *spanRecorder) BatchCreateSpans(_ context.Context, spans []store.SpanData) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spans = append(r.spans, spans...)
	return nil
}
func (r *spanRecorder) BatchUpdateTraceAggregates(context.Context, uuid.UUID) error { return nil }
func (r *spanRecorder) DeleteTracesOlderThan(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (r *spanRecorder) RecoverStaleRunningTraces(context.Context, time.Time) (int64, error) {
	return 0, nil
}

// Memory flush runs its own LLM loop; without a span its duration is invisible in the trace.
func TestMemoryFlushStage_EmitsSpan(t *testing.T) {
	t.Parallel()
	rec := &spanRecorder{}
	c := tracing.NewCollector(rec)
	c.Start()
	ctx := tracing.WithCollector(context.Background(), c)
	ctx = tracing.WithTraceID(ctx, uuid.New())

	deps := &PipelineDeps{
		RunMemoryFlush: func(context.Context, *RunState) error { return nil },
	}
	state := defaultState()
	state.Prune.HistoryTokens = 188939
	state.Prune.HistoryBudget = 180058

	if err := NewMemoryFlushStage(deps).Execute(ctx, state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	c.Stop()

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.spans) != 1 || rec.spans[0].Name != "memory_flush" {
		t.Fatalf("spans = %+v, want one memory_flush span", rec.spans)
	}
	if rec.spans[0].Status != store.SpanStatusCompleted {
		t.Errorf("status = %q, want %q", rec.spans[0].Status, store.SpanStatusCompleted)
	}
}
