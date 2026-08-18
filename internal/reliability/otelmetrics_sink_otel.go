//go:build otel

package reliability

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

// MetricNames is the frozen contract for the reliability OTel metrics. The
// prefix "goclaw.reliability." is consumed by the Phase 10 Grafana dashboard;
// rename only with a coordinated dashboard change.
const (
	metricPrefix = "goclaw.reliability."

	MetricRequests             = metricPrefix + "requests"
	MetricSuccesses            = metricPrefix + "successes"
	MetricRetries              = metricPrefix + "retries"
	MetricRateLimited          = metricPrefix + "rate_limited"
	MetricServerErrors         = metricPrefix + "server_errors"
	MetricTimeouts             = metricPrefix + "timeouts"
	MetricStreamStalls         = metricPrefix + "stream_stalls"
	MetricLoopDetected         = metricPrefix + "loop_detected"
	MetricRepeatedToolCalls    = metricPrefix + "repeated_tool_calls"
	MetricEmptyOutputs         = metricPrefix + "empty_outputs"
	MetricPrematureCompletions = metricPrefix + "premature_completions"
	MetricAgentRecovered       = metricPrefix + "agent_recovered"
	MetricAgentContinued       = metricPrefix + "agent_continued"
	MetricLLMLatencyMS         = metricPrefix + "llm_latency_ms"
)

// OTelSink implements reliability.Sink by recording each Snapshot delta into
// OTel counters and a latency histogram. It is concurrency-safe: the meter
// instruments are safe for concurrent Add/Record calls.
type OTelSink struct {
	// meter is retained so the sink stays usable even if the provider is shut
	// down; instruments created from a shutdown provider are no-ops.
	meter    metric.Meter
	counters map[string]metric.Int64Counter
	latency  metric.Float64Histogram
}

// NewOTelSink builds a sink bound to the given meter. Passing nil constructs a
// sink whose Emit is a no-op (nil meter methods are safe in the OTel API).
func NewOTelSink(mp *sdkmetric.MeterProvider) *OTelSink {
	s := &OTelSink{counters: make(map[string]metric.Int64Counter)}
	if mp == nil {
		return s
	}
	s.meter = mp.Meter("goclaw.reliability")
	for _, name := range []string{
		MetricRequests,
		MetricSuccesses,
		MetricRetries,
		MetricRateLimited,
		MetricServerErrors,
		MetricTimeouts,
		MetricStreamStalls,
		MetricLoopDetected,
		MetricRepeatedToolCalls,
		MetricEmptyOutputs,
		MetricPrematureCompletions,
		MetricAgentRecovered,
		MetricAgentContinued,
	} {
		if c, err := s.meter.Int64Counter(name); err == nil {
			s.counters[name] = c
		} else {
			slog.Warn("reliability otel sink: counter unavailable", "metric", name, "error", err)
		}
	}
	if h, err := s.meter.Float64Histogram(MetricLLMLatencyMS,
		metric.WithUnit("ms"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000),
	); err == nil {
		s.latency = h
	} else {
		slog.Warn("reliability otel sink: latency histogram unavailable", "error", err)
	}
	return s
}

// Emit records the snapshot delta into the OTel instruments. Snapshot fields
// are monotonic deltas produced by Metrics.Flush (each counter drained once).
func (s *OTelSink) Emit(sn Snapshot) {
	if s == nil || s.meter == nil {
		return
	}
	add := func(name string, v uint64) {
		if v == 0 {
			return
		}
		if c, ok := s.counters[name]; ok {
			c.Add(context.Background(), int64(v))
		}
	}
	add(MetricRequests, sn.LLMRequests)
	add(MetricSuccesses, sn.LLMSuccesses)
	add(MetricRetries, sn.LLMRetries)
	add(MetricRateLimited, sn.LLMRateLimited)
	add(MetricServerErrors, sn.LLMServerErrors)
	add(MetricTimeouts, sn.LLMTimeouts)
	add(MetricStreamStalls, sn.LLMStreamStalls)
	// Loop and premature-completion are single-source on the LLM-prefixed
	// snapshot fields (RecordLLMLoop / RecordLLMPrematureCompletion); the
	// legacy RecordLoopDetected / RecordPrematureCompletion counters are wired
	// nowhere in production and recording both would double-count.
	add(MetricLoopDetected, sn.LLMLoop)
	add(MetricRepeatedToolCalls, sn.LLMRepeatedToolCalls)
	add(MetricEmptyOutputs, sn.LLMEmptyOutputs)
	add(MetricPrematureCompletions, sn.LLMPrematureCompletions)
	add(MetricAgentRecovered, sn.AgentRecovered)
	add(MetricAgentContinued, sn.AgentContinued)

	if s.latency != nil && sn.LLMLatencyCount > 0 {
		avgMS := float64(sn.LLMLatencyNanos) / float64(sn.LLMLatencyCount) / 1e6
		s.latency.Record(context.Background(), avgMS)
	}
}

// OTelConfig configures the OTLP metric exporter. It mirrors the tracing
// exporter's options so a single telemetry.endpoint/protocol/insecure/headers
// set drives both traces and metrics. The struct is local to this package to
// avoid an import cycle (otelexport → store → config → reliability).
type OTelConfig struct {
	Endpoint    string
	Protocol    string
	Insecure    bool
	ServiceName string
	Headers     map[string]string
}

// NewOTelMeterProvider creates a meter provider exporting via OTLP (gRPC by
// default, HTTP with protocol=="http"), copying the option pattern from
// internal/tracing/otelexport. Attributes use literal keys (no semconv import)
// to stay within the module's existing dependency set.
func NewOTelMeterProvider(ctx context.Context, cfg OTelConfig) (*sdkmetric.MeterProvider, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("OTLP metrics endpoint is required")
	}
	serviceName := cfg.ServiceName
	if serviceName == "" {
		serviceName = "goclaw-gateway"
	}
	res, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", serviceName),
			attribute.String("service.version", "1.0.0"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("otel metric resource: %w", err)
	}

	var exp sdkmetric.Exporter
	switch cfg.Protocol {
	case "http":
		opts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(cfg.Endpoint)}
		if cfg.Insecure {
			opts = append(opts, otlpmetrichttp.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlpmetrichttp.WithHeaders(cfg.Headers))
		}
		exp, err = otlpmetrichttp.New(ctx, opts...)
	default: // "grpc"
		opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(cfg.Endpoint)}
		if cfg.Insecure {
			opts = append(opts, otlpmetricgrpc.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlpmetricgrpc.WithHeaders(cfg.Headers))
		}
		exp, err = otlpmetricgrpc.New(ctx, opts...)
	}
	if err != nil {
		return nil, fmt.Errorf("otel metric exporter: %w", err)
	}

	return sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp,
			sdkmetric.WithInterval(5*time.Second),
		)),
		sdkmetric.WithResource(res),
	), nil
}
