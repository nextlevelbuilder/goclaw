package config

import (
	"encoding/json"
	"testing"
)

// TestDefault_MaxCompactionsPerSession asserts the long-session compaction cap
// defaults to 12 (fresh installs ship the cap) and that a zero value in a
// loaded config means "unlimited legacy behaviour" (cap disabled).
func TestDefault_MaxCompactionsPerSession(t *testing.T) {
	cfg := Default()
	if cfg.Agents.Defaults.Compaction == nil {
		t.Fatal("Default() must seed a CompactionConfig so the cap default ships")
	}
	if got := cfg.Agents.Defaults.Compaction.MaxCompactionsPerSession; got != DefaultMaxCompactionsPerSession {
		t.Fatalf("default max compactions per session: got %d, want %d", got, DefaultMaxCompactionsPerSession)
	}
	if DefaultMaxCompactionsPerSession != 12 {
		t.Fatalf("DefaultMaxCompactionsPerSession must be 12, got %d", DefaultMaxCompactionsPerSession)
	}
}

// TestZeroValue_MaxCompactionsPerSession_Unlimited verifies that omitting the
// field (or setting 0) leaves the cap disabled — the legacy unlimited behaviour.
func TestZeroValue_MaxCompactionsPerSession_Unlimited(t *testing.T) {
	var cfg Config
	cfg.Agents.Defaults.Compaction = &CompactionConfig{}
	if cfg.Agents.Defaults.Compaction.MaxCompactionsPerSession != 0 {
		t.Fatal("zero-value CompactionConfig must have MaxCompactionsPerSession == 0")
	}
	if cfg.Agents.Defaults.Compaction.MaxCompactionsPerSession > 0 {
		t.Fatal("cap > 0 would enable the cap; zero must mean unlimited")
	}
}

// TestDefault_FreshResultCapTokens asserts the fresh tool-result cap defaults to
// 0 (disabled — zero-config preserves existing behaviour; Module B reads it).
func TestDefault_FreshResultCapTokens(t *testing.T) {
	cfg := Default()
	var got int
	if cfg.Agents.Defaults.ContextPruning != nil {
		got = cfg.Agents.Defaults.ContextPruning.FreshResultCapTokens
	}
	if got != 0 {
		t.Fatalf("default fresh result cap tokens: got %d, want 0 (disabled)", got)
	}
}

// TestRoundTrip_MaxCompactionsPerSession ensures the new field survives the JSON
// round-trip used by Save/Clone/MaskedCopy.
func TestRoundTrip_MaxCompactionsPerSession(t *testing.T) {
	cfg := Default()
	cfg.Agents.Defaults.Compaction.MaxCompactionsPerSession = 3
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Config
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Agents.Defaults.Compaction == nil || back.Agents.Defaults.Compaction.MaxCompactionsPerSession != 3 {
		t.Fatalf("round-trip lost MaxCompactionsPerSession: %#v", back.Agents.Defaults.Compaction)
	}
}
