package agent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// spanCaptureStore records spans and span updates flushed by a tracing.Collector.
type spanCaptureStore struct {
	store.TracingStore

	mu      sync.Mutex
	spans   []store.SpanData
	updates map[uuid.UUID]map[string]any
}

func (c *spanCaptureStore) BatchCreateSpans(_ context.Context, spans []store.SpanData) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.spans = append(c.spans, spans...)
	return nil
}

func (c *spanCaptureStore) UpdateSpan(_ context.Context, spanID uuid.UUID, updates map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.updates == nil {
		c.updates = map[uuid.UUID]map[string]any{}
	}
	c.updates[spanID] = updates
	return nil
}

func (c *spanCaptureStore) BatchUpdateTraceAggregates(context.Context, uuid.UUID) error { return nil }
func (c *spanCaptureStore) DeleteTracesOlderThan(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (c *spanCaptureStore) RecoverStaleRunningTraces(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func newCaptureCtx(t *testing.T) (context.Context, *tracing.Collector, *spanCaptureStore) {
	t.Helper()
	cs := &spanCaptureStore{}
	c := tracing.NewCollector(cs)
	c.Start()
	ctx := tracing.WithCollector(context.Background(), c)
	ctx = tracing.WithTraceID(ctx, uuid.New())
	return ctx, c, cs
}

func TestConvertRunResult_CopiesStopReason(t *testing.T) {
	t.Parallel()
	got := convertRunResult(&pipeline.RunResult{Content: "notice", StopReason: "final request context budget exceeded"})
	if got.StopReason != "final request context budget exceeded" {
		t.Fatalf("StopReason = %q, want it copied from the pipeline result", got.StopReason)
	}
}

func TestRunTraceStatus(t *testing.T) {
	t.Parallel()
	status, errMsg := runTraceStatus(&RunResult{StopReason: "context budget exceeded"})
	if status != store.TraceStatusError || errMsg != "context budget exceeded" {
		t.Errorf("stopped run: got (%q, %q), want (%q, reason)", status, errMsg, store.TraceStatusError)
	}
	status, errMsg = runTraceStatus(&RunResult{Content: "answer"})
	if status != store.TraceStatusCompleted || errMsg != "" {
		t.Errorf("normal run: got (%q, %q), want (%q, \"\")", status, errMsg, store.TraceStatusCompleted)
	}
	if status, _ = runTraceStatus(nil); status != store.TraceStatusCompleted {
		t.Errorf("nil result: got %q, want %q", status, store.TraceStatusCompleted)
	}
}

func TestEmitAgentSpanEnd_StoppedRunMarksSpanError(t *testing.T) {
	t.Parallel()
	ctx, c, cs := newCaptureCtx(t)
	spanID := uuid.New()

	(&Loop{}).emitAgentSpanEnd(ctx, spanID, time.Now(), &RunResult{Content: "notice", StopReason: "context budget exceeded"}, nil)
	c.Stop()

	cs.mu.Lock()
	defer cs.mu.Unlock()
	updates := cs.updates[spanID]
	if updates["status"] != store.SpanStatusError {
		t.Errorf("status = %v, want %q", updates["status"], store.SpanStatusError)
	}
	if updates["error"] != "context budget exceeded" {
		t.Errorf("error = %v, want the stop reason", updates["error"])
	}
	if updates["output_preview"] != "notice" {
		t.Errorf("output_preview = %v, want the delivered notice", updates["output_preview"])
	}
}

func TestMakeCompactMessages_EmitsCompactionSpan(t *testing.T) {
	t.Parallel()
	ctx, c, cs := newCaptureCtx(t)
	history := []providers.Message{{Role: "user", Content: "compare pages"}}
	for _, id := range []string{"c1", "c2", "c3"} {
		history = append(history,
			providers.Message{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: id, Name: "read_file"}}},
			providers.Message{Role: "tool", ToolCallID: id, Content: "result"},
		)
	}

	_, _ = (&Loop{}).makeCompactMessages(nil)(ctx, history, "test-model")
	c.Stop()

	cs.mu.Lock()
	defer cs.mu.Unlock()
	if len(cs.spans) != 1 || cs.spans[0].Name != "mid_loop_compaction" {
		t.Fatalf("spans = %+v, want one mid_loop_compaction span", cs.spans)
	}
	var meta map[string]any
	if err := json.Unmarshal(cs.spans[0].Metadata, &meta); err != nil || meta["outcome"] != "not_compacted" {
		t.Errorf("metadata = %s, want outcome=not_compacted", cs.spans[0].Metadata)
	}
}
