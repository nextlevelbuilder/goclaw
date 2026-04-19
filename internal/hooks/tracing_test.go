package hooks_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// capturingTracingStore is a minimal store.TracingStore that records spans
// received via BatchCreateSpans. Other methods fall through to the embedded
// (nil) interface — tests must not call them.
type capturingTracingStore struct {
	store.TracingStore // embedded; unused methods are not called by Collector flush

	mu    sync.Mutex
	spans []store.SpanData
}

func (c *capturingTracingStore) BatchCreateSpans(_ context.Context, spans []store.SpanData) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.spans = append(c.spans, spans...)
	return nil
}

func (c *capturingTracingStore) BatchUpdateTraceAggregates(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (c *capturingTracingStore) DeleteTracesOlderThan(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func (c *capturingTracingStore) snapshot() []store.SpanData {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]store.SpanData, len(c.spans))
	copy(out, c.spans)
	return out
}

// newRunningCollector returns a collector + started goroutines. Caller MUST
// Stop() to flush buffered spans to the capturing store.
func newRunningCollector(t *testing.T) (*tracing.Collector, *capturingTracingStore) {
	t.Helper()
	cs := &capturingTracingStore{}
	c := tracing.NewCollector(cs)
	c.Start()
	return c, cs
}

// TestEmitHookSpan_NoCollector_NoPanic: safe no-op when ctx has no collector.
func TestEmitHookSpan_NoCollector_NoPanic(t *testing.T) {
	hooks.EmitHookSpan(context.Background(), hooks.EventPreToolUse, hooks.HandlerCommand,
		time.Now().Add(-100*time.Millisecond), hooks.DecisionAllow, "")
}

// TestEmitHookSpan_NameFormat: span name = "hook.<handlerType>.<event>".
func TestEmitHookSpan_NameFormat(t *testing.T) {
	c, cs := newRunningCollector(t)
	ctx := tracing.WithCollector(context.Background(), c)
	ctx = tracing.WithTraceID(ctx, uuid.New())

	hooks.EmitHookSpan(ctx, hooks.EventPreToolUse, hooks.HandlerCommand,
		time.Now().Add(-50*time.Millisecond), hooks.DecisionAllow, "")

	c.Stop() // flush synchronously

	spans := cs.snapshot()
	if len(spans) == 0 {
		t.Fatal("expected at least one span flushed")
	}
	span := spans[0]
	want := "hook.command.pre_tool_use"
	if span.Name != want {
		t.Errorf("span name = %q, want %q", span.Name, want)
	}
	if span.SpanType != store.SpanTypeEvent {
		t.Errorf("span type = %q, want %q", span.SpanType, store.SpanTypeEvent)
	}
}

// TestEmitHookSpan_DurationAndStatus: duration is derived from startedAt; status
// reflects error presence (completed vs error).
func TestEmitHookSpan_DurationAndStatus(t *testing.T) {
	c, cs := newRunningCollector(t)
	ctx := tracing.WithCollector(context.Background(), c)
	ctx = tracing.WithTraceID(ctx, uuid.New())

	startedOK := time.Now().Add(-42 * time.Millisecond)
	hooks.EmitHookSpan(ctx, hooks.EventPostToolUse, hooks.HandlerHTTP, startedOK, hooks.DecisionAllow, "")

	startedErr := time.Now().Add(-17 * time.Millisecond)
	hooks.EmitHookSpan(ctx, hooks.EventPostToolUse, hooks.HandlerHTTP, startedErr, hooks.DecisionBlock, "timeout")

	c.Stop()

	spans := cs.snapshot()
	if len(spans) < 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}
	okSpan := spans[0]
	errSpan := spans[1]

	if okSpan.Status != store.SpanStatusCompleted {
		t.Errorf("ok span status = %q, want %q", okSpan.Status, store.SpanStatusCompleted)
	}
	if okSpan.Error != "" {
		t.Errorf("ok span should have empty error; got %q", okSpan.Error)
	}
	if okSpan.DurationMS < 40 {
		t.Errorf("ok span duration_ms = %d, want >= 40", okSpan.DurationMS)
	}
	if errSpan.Status != store.SpanStatusError {
		t.Errorf("err span status = %q, want %q", errSpan.Status, store.SpanStatusError)
	}
	if errSpan.Error != "timeout" {
		t.Errorf("err span error = %q, want %q", errSpan.Error, "timeout")
	}
}

// TestEmitHookSpan_DecisionInMetadata: decision is serialized into span.Metadata.
func TestEmitHookSpan_DecisionInMetadata(t *testing.T) {
	c, cs := newRunningCollector(t)
	ctx := tracing.WithCollector(context.Background(), c)
	ctx = tracing.WithTraceID(ctx, uuid.New())

	hooks.EmitHookSpan(ctx, hooks.EventUserPromptSubmit, hooks.HandlerPrompt,
		time.Now().Add(-10*time.Millisecond), hooks.DecisionBlock, "")

	c.Stop()

	spans := cs.snapshot()
	if len(spans) == 0 {
		t.Fatal("expected a span flushed")
	}
	if len(spans[0].Metadata) == 0 {
		t.Fatal("expected non-empty metadata json")
	}
	var md map[string]any
	if err := json.Unmarshal(spans[0].Metadata, &md); err != nil {
		t.Fatalf("metadata is not valid json: %v", err)
	}
	if got, _ := md["decision"].(string); got != string(hooks.DecisionBlock) {
		t.Errorf("metadata.decision = %q, want %q", got, string(hooks.DecisionBlock))
	}
}

// TestEmitHookSpan_PropagatesTenantAndTrace: ctx values flow through to the span.
func TestEmitHookSpan_PropagatesTenantAndTrace(t *testing.T) {
	c, cs := newRunningCollector(t)

	traceID := uuid.New()
	parentSpanID := uuid.New()
	tenantID := uuid.New()

	ctx := tracing.WithCollector(context.Background(), c)
	ctx = tracing.WithTraceID(ctx, traceID)
	ctx = tracing.WithParentSpanID(ctx, parentSpanID)
	ctx = store.WithTenantID(ctx, tenantID)

	hooks.EmitHookSpan(ctx, hooks.EventPreToolUse, hooks.HandlerCommand,
		time.Now().Add(-5*time.Millisecond), hooks.DecisionAllow, "")

	c.Stop()

	spans := cs.snapshot()
	if len(spans) == 0 {
		t.Fatal("expected a span flushed")
	}
	s := spans[0]
	if s.TraceID != traceID {
		t.Errorf("trace_id = %v, want %v", s.TraceID, traceID)
	}
	if s.ParentSpanID == nil || *s.ParentSpanID != parentSpanID {
		t.Errorf("parent_span_id = %v, want %v", s.ParentSpanID, parentSpanID)
	}
	if s.TenantID != tenantID {
		t.Errorf("tenant_id = %v, want %v", s.TenantID, tenantID)
	}
	if !strings.HasPrefix(s.Name, "hook.") {
		t.Errorf("span name %q must start with %q", s.Name, "hook.")
	}
}
