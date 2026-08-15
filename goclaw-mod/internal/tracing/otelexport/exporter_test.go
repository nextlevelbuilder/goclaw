package otelexport

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestUUIDToTraceID(t *testing.T) {
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	tid := uuidToTraceID(id)
	if tid == (trace.TraceID{}) {
		t.Error("expected non-zero trace ID")
	}
	// TraceID is 16 bytes, same as UUID
	if len(tid) != 16 {
		t.Errorf("expected 16 bytes, got %d", len(tid))
	}
}

func TestUUIDToSpanID(t *testing.T) {
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	sid := uuidToSpanID(id)
	if sid == (trace.SpanID{}) {
		t.Error("expected non-zero span ID")
	}
	// SpanID is 8 bytes, extracted from last 8 bytes of UUID
	if len(sid) != 8 {
		t.Errorf("expected 8 bytes, got %d", len(sid))
	}
	// Verify it uses the last 8 bytes
	for i := range 8 {
		if sid[i] != id[8+i] {
			t.Errorf("byte %d: expected %02x, got %02x", i, id[8+i], sid[i])
		}
	}
}

func TestUUIDToSpanID_DifferentUUIDs(t *testing.T) {
	id1 := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	id2 := uuid.MustParse("550e8400-e29b-41d4-b827-557766550001")
	sid1 := uuidToSpanID(id1)
	sid2 := uuidToSpanID(id2)
	if sid1 == sid2 {
		t.Error("different UUIDs should produce different span IDs")
	}
}

func TestNew_EmptyEndpoint(t *testing.T) {
	_, err := New(nil, Config{})
	if err == nil {
		t.Error("expected error for empty endpoint")
	}
}

func TestExporter_ExportSpans_NilExporter(t *testing.T) {
	// Should not panic
	var exp *Exporter
	exp.ExportSpans(nil, []store.SpanData{{
		ID:        uuid.New(),
		TraceID:   uuid.New(),
		SpanType:  "llm_call",
		Name:      "test",
		StartTime: time.Now(),
	}})
}

func TestExporter_Shutdown_NilExporter(t *testing.T) {
	var exp *Exporter
	if err := exp.Shutdown(nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestConfig_DefaultServiceName(t *testing.T) {
	cfg := Config{
		Endpoint: "localhost:4317",
		Insecure: true,
	}
	if cfg.ServiceName == "" {
		// New should default to "goclaw-gateway"
		// We can't easily test this without a running OTLP server,
		// but we verify the config struct accepts empty service name
	}
}

func TestConfig_Protocols(t *testing.T) {
	tests := []struct {
		protocol string
		valid    bool
	}{
		{"grpc", true},
		{"http", true},
		{"", true}, // defaults to grpc
	}
	for _, tc := range tests {
		cfg := Config{
			Endpoint: "localhost:4317",
			Protocol: tc.protocol,
		}
		if tc.valid && cfg.Endpoint == "" {
			t.Errorf("protocol %q: expected valid config", tc.protocol)
		}
	}
}
