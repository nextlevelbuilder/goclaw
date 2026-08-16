package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStreamConfigEffectiveDefaults(t *testing.T) {
	var s StreamConfig
	if d := s.EffectiveStreamIdleTimeout(); d != 0 {
		t.Fatalf("default idle timeout = %v, want 0 (disabled)", d)
	}
	if d := s.EffectiveStreamFirstByteTimeout(); d != 0 {
		t.Fatalf("default first-byte timeout = %v, want 0 (disabled)", d)
	}

	if DefaultStreamIdleTimeoutMs != 60000 {
		t.Fatalf("DefaultStreamIdleTimeoutMs = %d, want 60000", DefaultStreamIdleTimeoutMs)
	}
	if DefaultStreamFirstByteTimeoutMs != 0 {
		t.Fatalf("DefaultStreamFirstByteTimeoutMs = %d, want 0", DefaultStreamFirstByteTimeoutMs)
	}
}

func TestStreamConfigEffectiveOverrides(t *testing.T) {
	s := StreamConfig{IdleTimeoutMs: 45000, FirstByteTimeoutMs: 10000}
	if d := s.EffectiveStreamIdleTimeout(); d != 45*time.Second {
		t.Fatalf("idle timeout = %v, want 45s", d)
	}
	if d := s.EffectiveStreamFirstByteTimeout(); d != 10*time.Second {
		t.Fatalf("first-byte timeout = %v, want 10s", d)
	}
}

func TestStreamConfigZeroAndNegativeDisable(t *testing.T) {
	// 0 = disabled and negative values clamp to disabled (≤0 → 0).
	for _, s := range []StreamConfig{
		{},
		{IdleTimeoutMs: 0, FirstByteTimeoutMs: 0},
		{IdleTimeoutMs: -1, FirstByteTimeoutMs: -500},
		{IdleTimeoutMs: 0, FirstByteTimeoutMs: -10},
	} {
		if d := s.EffectiveStreamIdleTimeout(); d != 0 {
			t.Fatalf("idle timeout must be disabled, got %v for %+v", d, s)
		}
		if d := s.EffectiveStreamFirstByteTimeout(); d != 0 {
			t.Fatalf("first-byte timeout must be disabled, got %v for %+v", d, s)
		}
	}
}

func TestReliabilityConfigStreamJSONRoundTrip(t *testing.T) {
	in := ReliabilityConfig{Stream: StreamConfig{IdleTimeoutMs: 90000, FirstByteTimeoutMs: 15000}}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out ReliabilityConfig
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Stream.IdleTimeoutMs != in.Stream.IdleTimeoutMs ||
		out.Stream.FirstByteTimeoutMs != in.Stream.FirstByteTimeoutMs {
		t.Fatalf("round-trip mismatch: in=%+v out=%+v", in.Stream, out.Stream)
	}
}

func TestLoadStreamConfigFromJSON5(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json5")

	content := `{
		"reliability": {
			"stream": {
				"idle_timeout_ms": 30000,
				"first_byte_timeout_ms": 5000,
			},
		},
	}`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Reliability.Stream.IdleTimeoutMs != 30000 {
		t.Fatalf("idle_timeout_ms = %d, want 30000", cfg.Reliability.Stream.IdleTimeoutMs)
	}
	if cfg.Reliability.Stream.FirstByteTimeoutMs != 5000 {
		t.Fatalf("first_byte_timeout_ms = %d, want 5000", cfg.Reliability.Stream.FirstByteTimeoutMs)
	}
	if d := cfg.Reliability.Stream.EffectiveStreamIdleTimeout(); d != 30*time.Second {
		t.Fatalf("effective idle timeout = %v, want 30s", d)
	}
	if d := cfg.Reliability.Stream.EffectiveStreamFirstByteTimeout(); d != 5*time.Second {
		t.Fatalf("effective first-byte timeout = %v, want 5s", d)
	}
}

func TestLoadStreamConfigDisabledByZero(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json5")

	content := `{
		"reliability": {
			"stream": {
				"idle_timeout_ms": 0,
				"first_byte_timeout_ms": 0,
			},
		},
	}`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if d := cfg.Reliability.Stream.EffectiveStreamIdleTimeout(); d != 0 {
		t.Fatalf("idle timeout = %v, want 0 (disabled)", d)
	}
	if d := cfg.Reliability.Stream.EffectiveStreamFirstByteTimeout(); d != 0 {
		t.Fatalf("first-byte timeout = %v, want 0 (disabled)", d)
	}
}
