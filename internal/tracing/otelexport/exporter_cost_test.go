package otelexport

import (
	"context"
	"testing"

	"github.com/google/uuid"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// spanRecorder is a minimal sdktrace.SpanExporter that collects exported spans
// for attribute assertions. It avoids needing a live OTLP backend.
type spanRecorder struct {
	spans []sdktrace.ReadOnlySpan
}

func (r *spanRecorder) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	r.spans = append(r.spans, spans...)
	return nil
}

func (r *spanRecorder) Shutdown(context.Context) error { return nil }

// recordingExporter builds an Exporter whose spans are captured by a recorder
// instead of shipped over OTLP, so exportSpan's attribute construction can be
// asserted deterministically.
func recordingExporter(t *testing.T) (*Exporter, *spanRecorder) {
	t.Helper()
	rec := &spanRecorder{}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(rec)),
	)
	return &Exporter{
		provider: tp,
		tracer:   tp.Tracer("goclaw-test"),
	}, rec
}

func TestExportSpanCostAttribute(t *testing.T) {
	exp, rec := recordingExporter(t)
	cost := 0.0042
	span := store.SpanData{
		ID:           uuid.New(),
		TraceID:      uuid.New(),
		SpanType:     "llm_call",
		Name:         "claude-sonnet",
		Model:        "claude-sonnet-4-5",
		Provider:     "anthropic",
		InputTokens:  100,
		OutputTokens: 50,
		TotalCost:    &cost,
	}
	exp.ExportSpans(context.Background(), []store.SpanData{span})

	if len(rec.spans) != 1 {
		t.Fatalf("exported spans = %d, want 1", len(rec.spans))
	}
	found := false
	for _, kv := range rec.spans[0].Attributes() {
		if kv.Key == "goclaw.llm.cost_usd" {
			found = true
			if got := kv.Value.AsFloat64(); got != cost {
				t.Fatalf("cost attr = %v, want %v", got, cost)
			}
		}
	}
	if !found {
		t.Fatal("missing goclaw.llm.cost_usd attribute")
	}
}

func TestExportSpanOmitsZeroCost(t *testing.T) {
	exp, rec := recordingExporter(t)
	zero := 0.0
	span := store.SpanData{
		ID:        uuid.New(),
		TraceID:   uuid.New(),
		SpanType:  "llm_call",
		Name:      "free-model",
		TotalCost: &zero,
	}
	exp.ExportSpans(context.Background(), []store.SpanData{span})

	if len(rec.spans) != 1 {
		t.Fatalf("exported spans = %d, want 1", len(rec.spans))
	}
	for _, kv := range rec.spans[0].Attributes() {
		if kv.Key == "goclaw.llm.cost_usd" {
			t.Fatal("cost attribute must be omitted when TotalCost is 0")
		}
	}
}

func TestExportSpanOmitsNilCost(t *testing.T) {
	exp, rec := recordingExporter(t)
	span := store.SpanData{
		ID:       uuid.New(),
		TraceID:  uuid.New(),
		SpanType: "llm_call",
		Name:     "no-cost",
	}
	exp.ExportSpans(context.Background(), []store.SpanData{span})

	if len(rec.spans) != 1 {
		t.Fatalf("exported spans = %d, want 1", len(rec.spans))
	}
	for _, kv := range rec.spans[0].Attributes() {
		if kv.Key == "goclaw.llm.cost_usd" {
			t.Fatal("cost attribute must be omitted when TotalCost is nil")
		}
	}
}
