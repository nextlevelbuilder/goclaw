package config

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/reliability"
)

func TestCircuitConfigEffectiveDefaults(t *testing.T) {
	var c CircuitConfig
	opts := c.EffectiveCircuit()

	want := reliability.DefaultCircuitOptions()
	if opts.FailureThreshold != want.FailureThreshold {
		t.Fatalf("default failure_threshold = %d, want %d", opts.FailureThreshold, want.FailureThreshold)
	}
	if opts.DegradedThreshold != want.DegradedThreshold {
		t.Fatalf("default degraded_threshold = %d, want %d", opts.DegradedThreshold, want.DegradedThreshold)
	}
	if opts.Cooldown != want.Cooldown {
		t.Fatalf("default cooldown = %v, want %v", opts.Cooldown, want.Cooldown)
	}
	if opts.HalfOpenMax != want.HalfOpenMax {
		t.Fatalf("default half_open_max = %d, want %d", opts.HalfOpenMax, want.HalfOpenMax)
	}
	if opts.ProbeTimeout != want.ProbeTimeout {
		t.Fatalf("default probe_timeout = %v, want %v", opts.ProbeTimeout, want.ProbeTimeout)
	}
	if got := c.EffectiveRateLimitMaxPending(); got != DefaultCircuitRateLimitMaxPending {
		t.Fatalf("default rate_limit_max_pending = %d, want %d", got, DefaultCircuitRateLimitMaxPending)
	}
}

func TestCircuitConfigEffectiveOverrides(t *testing.T) {
	c := CircuitConfig{
		FailureThreshold:   7,
		DegradedThreshold:  3,
		CooldownMs:         45000,
		HalfOpenMax:        4,
		ProbeTimeoutMs:     15000,
		RateLimitMaxPending: 12,
	}
	opts := c.EffectiveCircuit()
	if opts.FailureThreshold != 7 || opts.DegradedThreshold != 3 {
		t.Fatalf("thresholds not applied: %+v", opts)
	}
	if opts.Cooldown != 45*time.Second {
		t.Fatalf("cooldown = %v, want 45s", opts.Cooldown)
	}
	if opts.HalfOpenMax != 4 {
		t.Fatalf("half_open_max = %d, want 4", opts.HalfOpenMax)
	}
	if opts.ProbeTimeout != 15*time.Second {
		t.Fatalf("probe_timeout = %v, want 15s", opts.ProbeTimeout)
	}
	if got := c.EffectiveRateLimitMaxPending(); got != 12 {
		t.Fatalf("rate_limit_max_pending = %d, want 12", got)
	}
}

func TestCircuitConfigZeroAndNegativeFallBack(t *testing.T) {
	for _, c := range []CircuitConfig{
		{FailureThreshold: -1, DegradedThreshold: -2, CooldownMs: -5, HalfOpenMax: -1, ProbeTimeoutMs: -5},
		{CooldownMs: 0, ProbeTimeoutMs: 0},
	} {
		opts := c.EffectiveCircuit()
		want := reliability.DefaultCircuitOptions()
		if opts.FailureThreshold != want.FailureThreshold ||
			opts.DegradedThreshold != want.DegradedThreshold ||
			opts.Cooldown != want.Cooldown ||
			opts.HalfOpenMax != want.HalfOpenMax ||
			opts.ProbeTimeout != want.ProbeTimeout {
			t.Fatalf("invalid/zero config must fall back to defaults, got %+v for %+v", opts, c)
		}
	}
	if got := (CircuitConfig{RateLimitMaxPending: -3}).EffectiveRateLimitMaxPending(); got != DefaultCircuitRateLimitMaxPending {
		t.Fatalf("negative rate_limit_max_pending must fall back to %d, got %d", DefaultCircuitRateLimitMaxPending, got)
	}
}

func TestReliabilityConfigCircuitJSONRoundTrip(t *testing.T) {
	in := ReliabilityConfig{Circuit: CircuitConfig{
		FailureThreshold:   9,
		DegradedThreshold:  4,
		CooldownMs:         60000,
		HalfOpenMax:        2,
		ProbeTimeoutMs:     20000,
		RateLimitMaxPending: 25,
	}}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out ReliabilityConfig
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Circuit != in.Circuit {
		t.Fatalf("round-trip mismatch: in=%+v out=%+v", in.Circuit, out.Circuit)
	}
}
