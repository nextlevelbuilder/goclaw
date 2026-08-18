package config

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSLOConfigEffectiveDefaults(t *testing.T) {
	var s SLOConfig
	if got := s.EffectiveSLOTarget(); got != DefaultSLOTargetPercent {
		t.Fatalf("default target = %v, want %.2f", got, DefaultSLOTargetPercent)
	}
	if got := s.EffectiveSLOWindow(); got != time.Duration(DefaultSLOWindowSeconds)*time.Second {
		t.Fatalf("default window = %v, want %v", got, time.Duration(DefaultSLOWindowSeconds)*time.Second)
	}
}

func TestSLOConfigEffectiveOverrides(t *testing.T) {
	s := SLOConfig{Enabled: true, TargetPercent: 0.95, WindowSeconds: 1800}
	if got := s.EffectiveSLOTarget(); got != 0.95 {
		t.Fatalf("target = %v, want 0.95", got)
	}
	if got := s.EffectiveSLOWindow(); got != 30*time.Minute {
		t.Fatalf("window = %v, want 30m", got)
	}
}

func TestSLOConfigZeroAndNegativeFallBack(t *testing.T) {
	for _, s := range []SLOConfig{
		{},
		{TargetPercent: -1, WindowSeconds: 0},
		{TargetPercent: 0, WindowSeconds: -10},
		{TargetPercent: 1.5, WindowSeconds: 100}, // > 1 is invalid
	} {
		if got := s.EffectiveSLOTarget(); got <= 0 || got > 1 {
			t.Fatalf("target must clamp into (0,1], got %v for %+v", got, s)
		}
		if got := s.EffectiveSLOWindow(); got <= 0 {
			t.Fatalf("window must be positive, got %v for %+v", got, s)
		}
	}
}

func TestAlertingConfigEffectiveDefaults(t *testing.T) {
	var a AlertingConfig
	if got := a.EffectiveAlertMinInterval(); got != time.Duration(DefaultAlertMinIntervalSeconds)*time.Second {
		t.Fatalf("default min interval = %v, want %v",
			got, time.Duration(DefaultAlertMinIntervalSeconds)*time.Second)
	}
}

func TestAlertingConfigEffectiveOverrides(t *testing.T) {
	a := AlertingConfig{Enabled: true, WebhookURL: "https://example.test/hook", MinIntervalSeconds: 120}
	if got := a.EffectiveAlertMinInterval(); got != 2*time.Minute {
		t.Fatalf("min interval = %v, want 2m", got)
	}
}

func TestAlertingConfigZeroAndNegativeFallBack(t *testing.T) {
	for _, a := range []AlertingConfig{
		{},
		{MinIntervalSeconds: -5},
	} {
		if got := a.EffectiveAlertMinInterval(); got <= 0 {
			t.Fatalf("min interval must be positive, got %v for %+v", got, a)
		}
	}
}

func TestDefaultSeedsSLOAndAlerts(t *testing.T) {
	cfg := Default()
	if cfg.Reliability.SLO.TargetPercent != DefaultSLOTargetPercent {
		t.Fatalf("Default() SLO target = %v, want %v", cfg.Reliability.SLO.TargetPercent, DefaultSLOTargetPercent)
	}
	if cfg.Reliability.SLO.WindowSeconds != DefaultSLOWindowSeconds {
		t.Fatalf("Default() SLO window = %d, want %d", cfg.Reliability.SLO.WindowSeconds, DefaultSLOWindowSeconds)
	}
	if cfg.Reliability.Alerts.MinIntervalSeconds != DefaultAlertMinIntervalSeconds {
		t.Fatalf("Default() alert min interval = %d, want %d",
			cfg.Reliability.Alerts.MinIntervalSeconds, DefaultAlertMinIntervalSeconds)
	}
}

func TestReliabilitySLOAlertConfigJSONRoundTrip(t *testing.T) {
	in := ReliabilityConfig{
		SLO: SLOConfig{Enabled: true, TargetPercent: 0.97, WindowSeconds: 600},
		Alerts: AlertingConfig{
			Enabled:            true,
			WebhookURL:         "https://hooks.example.test/slo",
			MinIntervalSeconds: 300,
		},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out ReliabilityConfig
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.SLO.Enabled != in.SLO.Enabled ||
		out.SLO.TargetPercent != in.SLO.TargetPercent ||
		out.SLO.WindowSeconds != in.SLO.WindowSeconds {
		t.Fatalf("SLO round-trip mismatch: in=%+v out=%+v", in.SLO, out.SLO)
	}
	if out.Alerts.Enabled != in.Alerts.Enabled ||
		out.Alerts.WebhookURL != in.Alerts.WebhookURL ||
		out.Alerts.MinIntervalSeconds != in.Alerts.MinIntervalSeconds {
		t.Fatalf("alerts round-trip mismatch: in=%+v out=%+v", in.Alerts, out.Alerts)
	}
}