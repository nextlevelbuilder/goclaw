package tracing_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

type eventSpanStore struct {
	store.TracingStore

	mu    sync.Mutex
	spans []store.SpanData
}

func (s *eventSpanStore) BatchCreateSpans(_ context.Context, spans []store.SpanData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spans = append(s.spans, spans...)
	return nil
}
func (s *eventSpanStore) BatchUpdateTraceAggregates(context.Context, uuid.UUID) error { return nil }
func (s *eventSpanStore) DeleteTracesOlderThan(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (s *eventSpanStore) RecoverStaleRunningTraces(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func TestEmitEventSpan_NoCollector_NoPanic(t *testing.T) {
	tracing.EmitEventSpan(context.Background(), "memory_flush", time.Now(), nil, nil)
}

func TestEmitEventSpan_RecordsStatusDurationAndMetadata(t *testing.T) {
	st := &eventSpanStore{}
	c := tracing.NewCollector(st)
	c.Start()
	traceID, parentID := uuid.New(), uuid.New()
	ctx := tracing.WithCollector(context.Background(), c)
	ctx = tracing.WithTraceID(ctx, traceID)
	ctx = tracing.WithParentSpanID(ctx, parentID)

	tracing.EmitEventSpan(ctx, "mid_loop_compaction", time.Now().Add(-40*time.Millisecond),
		errors.New("summarize failed"), map[string]any{"outcome": "not_compacted"})
	c.Stop()

	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(st.spans))
	}
	span := st.spans[0]
	if span.Name != "mid_loop_compaction" || span.SpanType != store.SpanTypeEvent {
		t.Errorf("span = %s/%s, want mid_loop_compaction/%s", span.SpanType, span.Name, store.SpanTypeEvent)
	}
	if span.TraceID != traceID || span.ParentSpanID == nil || *span.ParentSpanID != parentID {
		t.Errorf("span not attached to trace %s / parent %s", traceID, parentID)
	}
	if span.Status != store.SpanStatusError || span.Error != "summarize failed" {
		t.Errorf("status/error = %q/%q, want error/summarize failed", span.Status, span.Error)
	}
	if span.DurationMS < 40 || span.EndTime == nil {
		t.Errorf("duration = %dms, end_time = %v; want >= 40ms and set", span.DurationMS, span.EndTime)
	}
	var meta map[string]any
	if err := json.Unmarshal(span.Metadata, &meta); err != nil || meta["outcome"] != "not_compacted" {
		t.Errorf("metadata = %s, want outcome=not_compacted", span.Metadata)
	}
}
