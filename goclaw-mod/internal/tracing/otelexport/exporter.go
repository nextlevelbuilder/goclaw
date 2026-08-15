package otelexport

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Config configures the OpenTelemetry OTLP exporter.
type Config struct {
	Endpoint    string            // OTLP endpoint (e.g. "localhost:4317")
	Protocol    string            // "grpc" (default) or "http"
	Insecure    bool              // skip TLS for local dev
	ServiceName string            // OTEL service name (default "goclaw-gateway")
	Headers     map[string]string // extra headers (auth tokens, etc.)
}

// Exporter converts GoClaw SpanData → OTel spans and exports via OTLP.
// It implements the tracing.SpanExporter interface.
type Exporter struct {
	provider *sdktrace.TracerProvider
	tracer   trace.Tracer
}

// New creates an OTLP exporter with the given config.
func New(ctx context.Context, cfg Config) (*Exporter, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("OTLP endpoint is required")
	}

	serviceName := cfg.ServiceName
	if serviceName == "" {
		serviceName = "goclaw-gateway"
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("otel resource: %w", err)
	}

	var exporter sdktrace.SpanExporter
	switch cfg.Protocol {
	case "http":
		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(cfg.Endpoint),
		}
		if cfg.Insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(cfg.Headers))
		}
		exporter, err = otlptracehttp.New(ctx, opts...)
	default: // "grpc"
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(cfg.Endpoint),
		}
		if cfg.Insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlptracegrpc.WithHeaders(cfg.Headers))
		}
		exporter, err = otlptracegrpc.New(ctx, opts...)
	}
	if err != nil {
		return nil, fmt.Errorf("otel exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithMaxExportBatchSize(100),
			sdktrace.WithBatchTimeout(5*time.Second),
		),
		sdktrace.WithResource(res),
	)

	return &Exporter{
		provider: tp,
		tracer:   tp.Tracer("goclaw"),
	}, nil
}

// ExportSpans converts GoClaw SpanData to OTel spans and exports them.
// Called by the Collector during flush alongside the PostgreSQL batch insert.
func (e *Exporter) ExportSpans(ctx context.Context, spans []store.SpanData) {
	if e == nil || len(spans) == 0 {
		return
	}

	for _, s := range spans {
		e.exportSpan(ctx, s)
	}
}

func (e *Exporter) exportSpan(ctx context.Context, s store.SpanData) {
	// Build trace/span IDs from our UUIDs
	traceID := uuidToTraceID(s.TraceID)
	spanID := uuidToSpanID(s.ID)

	// Create a span context for the parent relationship
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})

	// Build attributes based on span type
	attrs := []attribute.KeyValue{
		attribute.String("goclaw.span_type", s.SpanType),
	}

	if s.Model != "" {
		attrs = append(attrs, attribute.String("gen_ai.request.model", s.Model))
	}
	if s.Provider != "" {
		attrs = append(attrs, attribute.String("gen_ai.system", s.Provider))
	}
	if s.InputTokens > 0 {
		attrs = append(attrs, attribute.Int("gen_ai.usage.input_tokens", s.InputTokens))
	}
	if s.OutputTokens > 0 {
		attrs = append(attrs, attribute.Int("gen_ai.usage.output_tokens", s.OutputTokens))
	}
	if s.FinishReason != "" {
		attrs = append(attrs, attribute.String("gen_ai.response.finish_reason", s.FinishReason))
	}
	if s.ToolName != "" {
		attrs = append(attrs, attribute.String("goclaw.tool.name", s.ToolName))
	}
	if s.ToolCallID != "" {
		attrs = append(attrs, attribute.String("goclaw.tool.call_id", s.ToolCallID))
	}
	if s.DurationMS > 0 {
		attrs = append(attrs, attribute.Int("goclaw.duration_ms", s.DurationMS))
	}
	if s.AgentID != nil {
		attrs = append(attrs, attribute.String("goclaw.agent_id", s.AgentID.String()))
	}
	if s.InputPreview != "" {
		preview := s.InputPreview
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		attrs = append(attrs, attribute.String("goclaw.input_preview", preview))
	}
	if s.OutputPreview != "" {
		preview := s.OutputPreview
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		attrs = append(attrs, attribute.String("goclaw.output_preview", preview))
	}

	// Create parent context if parent span exists
	parentCtx := ctx
	if s.ParentSpanID != nil {
		parentSpanCtx := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceID,
			SpanID:     uuidToSpanID(*s.ParentSpanID),
			TraceFlags: trace.FlagsSampled,
			Remote:     true,
		})
		parentCtx = trace.ContextWithRemoteSpanContext(parentCtx, parentSpanCtx)
	}

	// Map span type to OTel span kind
	kind := trace.SpanKindInternal
	if s.SpanType == "llm_call" {
		kind = trace.SpanKindClient
	}

	// Start span with exact timestamps
	_, span := e.tracer.Start(parentCtx, s.Name,
		trace.WithTimestamp(s.StartTime),
		trace.WithSpanKind(kind),
		trace.WithAttributes(attrs...),
	)

	// Set span context to our generated IDs (override auto-generated ones)
	// Note: OTel SDK generates its own IDs. We use ReadWriteSpan to override.
	// Since we can't easily override IDs in the standard SDK, we set them as attributes
	// for correlation with PostgreSQL traces.
	span.SetAttributes(
		attribute.String("goclaw.trace_id", s.TraceID.String()),
		attribute.String("goclaw.span_id", s.ID.String()),
	)

	if s.Status == "error" {
		span.SetStatus(codes.Error, s.Error)
		if s.Error != "" {
			span.RecordError(fmt.Errorf("%s", s.Error))
		}
	} else {
		span.SetStatus(codes.Ok, "")
	}

	// End with exact timestamp
	endTime := s.StartTime.Add(time.Duration(s.DurationMS) * time.Millisecond)
	if s.EndTime != nil {
		endTime = *s.EndTime
	}
	span.End(trace.WithTimestamp(endTime))

	// Force the span context for correlation
	_ = spanCtx
}

// Shutdown gracefully shuts down the OTel exporter, flushing remaining spans.
func (e *Exporter) Shutdown(ctx context.Context) error {
	if e == nil {
		return nil
	}
	slog.Info("otel exporter shutting down")
	return e.provider.Shutdown(ctx)
}

// uuidToTraceID converts a UUID to an OTel TraceID (16 bytes).
func uuidToTraceID(id [16]byte) trace.TraceID {
	return trace.TraceID(id)
}

// uuidToSpanID converts a UUID to an OTel SpanID (8 bytes, uses last 8 bytes of UUID).
func uuidToSpanID(id [16]byte) trace.SpanID {
	var sid trace.SpanID
	copy(sid[:], id[8:16])
	return sid
}
