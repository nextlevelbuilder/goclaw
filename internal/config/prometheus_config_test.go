package config

import (
	"encoding/json"
	"testing"
)

func TestPrometheusConfigEffectiveDefaults(t *testing.T) {
	var p PrometheusConfig
	if got := p.EffectivePort(); got != DefaultPrometheusPort {
		t.Fatalf("default port = %d, want %d", got, DefaultPrometheusPort)
	}
	if got := p.BindHost(); got != DefaultPrometheusHost {
		t.Fatalf("default host = %q, want %q", got, DefaultPrometheusHost)
	}
}

func TestPrometheusConfigEffectiveOverrides(t *testing.T) {
	p := PrometheusConfig{Enabled: true, Port: 19100, Host: "0.0.0.0"}
	if got := p.EffectivePort(); got != 19100 {
		t.Fatalf("port = %d, want 19100", got)
	}
	if got := p.BindHost(); got != "0.0.0.0" {
		t.Fatalf("host = %q, want \"0.0.0.0\"", got)
	}
}

func TestTelemetryConfigPrometheusJSONRoundTrip(t *testing.T) {
	in := TelemetryConfig{
		Enabled:      true,
		Endpoint:     "localhost:4317",
		ServiceName:  "test",
		Prometheus:   PrometheusConfig{Enabled: true, Port: 19100, Host: "127.0.0.1"},
		ModelPricing: map[string]*ModelPricing{"anthropic/claude-sonnet-4-5": {InputPerMillion: 3, OutputPerMillion: 15}},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out TelemetryConfig
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Prometheus.Enabled || out.Prometheus.Port != 19100 || out.Prometheus.Host != "127.0.0.1" {
		t.Fatalf("prometheus round-trip mismatch: in=%+v out=%+v", in.Prometheus, out.Prometheus)
	}
}

func TestDefaultTelemetryPrometheusDisabled(t *testing.T) {
	cfg := Default()
	if cfg.Telemetry.Prometheus.Enabled {
		t.Fatal("Prometheus /metrics must be disabled by default")
	}
}