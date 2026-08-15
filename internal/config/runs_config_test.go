package config

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRunsConfigEffectiveDefaults(t *testing.T) {
	var r RunsConfig
	if d := r.EffectiveHeartbeatInterval(); d != time.Duration(DefaultRunsHeartbeatIntervalMs)*time.Millisecond {
		t.Fatalf("default heartbeat = %v", d)
	}
	if d := r.EffectiveStaleAfter(); d != time.Duration(DefaultRunsStaleAfterMs)*time.Millisecond {
		t.Fatalf("default stale-after = %v", d)
	}
	if d := r.EffectiveSweepInterval(); d != time.Duration(DefaultRunsSweepIntervalMs)*time.Millisecond {
		t.Fatalf("default sweep interval = %v", d)
	}
}

func TestRunsConfigEffectiveOverrides(t *testing.T) {
	r := RunsConfig{HeartbeatIntervalMs: 5000, StaleAfterMs: 30000, SweepIntervalMs: 15000}
	if d := r.EffectiveHeartbeatInterval(); d != 5*time.Second {
		t.Fatalf("heartbeat = %v, want 5s", d)
	}
	if d := r.EffectiveStaleAfter(); d != 30*time.Second {
		t.Fatalf("stale-after = %v, want 30s", d)
	}
	if d := r.EffectiveSweepInterval(); d != 15*time.Second {
		t.Fatalf("sweep interval = %v, want 15s", d)
	}
}

func TestRunsConfigZeroAndNegativeFallBack(t *testing.T) {
	// Zero and negative values must clamp back to defaults, not produce 0s
	// (which would spam the DB or never sweep).
	for _, r := range []RunsConfig{
		{},
		{HeartbeatIntervalMs: -1, StaleAfterMs: 0, SweepIntervalMs: -10},
		{HeartbeatIntervalMs: 0, StaleAfterMs: -5, SweepIntervalMs: 0},
	} {
		if d := r.EffectiveHeartbeatInterval(); d <= 0 {
			t.Fatalf("heartbeat must be positive, got %v for %+v", d, r)
		}
		if d := r.EffectiveStaleAfter(); d <= 0 {
			t.Fatalf("stale-after must be positive, got %v for %+v", d, r)
		}
		if d := r.EffectiveSweepInterval(); d <= 0 {
			t.Fatalf("sweep interval must be positive, got %v for %+v", d, r)
		}
	}
}

func TestReliabilityConfigJSONRoundTrip(t *testing.T) {
	in := ReliabilityConfig{Runs: RunsConfig{HeartbeatIntervalMs: 7000, StaleAfterMs: 90000, SweepIntervalMs: 45000}}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out ReliabilityConfig
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Runs.HeartbeatIntervalMs != in.Runs.HeartbeatIntervalMs ||
		out.Runs.StaleAfterMs != in.Runs.StaleAfterMs ||
		out.Runs.SweepIntervalMs != in.Runs.SweepIntervalMs {
		t.Fatalf("round-trip mismatch: in=%+v out=%+v", in, out)
	}
}
