package agent

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestToolLoopDetection_NoLoop(t *testing.T) {
	s := newToolLoopState(nil)

	// 2 identical calls with same result → below threshold, no detection
	for i := range 2 {
		h := s.record("list_files", map[string]any{"path": "."})
		s.recordResult(h, "access denied")
		level, _ := s.detect("list_files", h)
		if level != "" {
			t.Fatalf("iteration %d: expected no detection, got %q", i, level)
		}
	}
}

func TestToolLoopDetection_Warning(t *testing.T) {
	s := newToolLoopState(nil)

	var lastLevel string
	for range defaultToolLoopWarningThreshold {
		h := s.record("list_files", map[string]any{"path": "."})
		s.recordResult(h, "access denied")
		lastLevel, _ = s.detect("list_files", h)
	}
	if lastLevel != "warning" {
		t.Fatalf("expected warning after %d calls, got %q", defaultToolLoopWarningThreshold, lastLevel)
	}
}

func TestToolLoopDetection_Critical(t *testing.T) {
	s := newToolLoopState(nil)

	var lastLevel string
	for range defaultToolLoopCriticalThreshold {
		h := s.record("list_files", map[string]any{"path": "."})
		s.recordResult(h, "access denied")
		lastLevel, _ = s.detect("list_files", h)
	}
	if lastLevel != "critical" {
		t.Fatalf("expected critical after %d calls, got %q", defaultToolLoopCriticalThreshold, lastLevel)
	}
}

func TestToolLoopDetection_DifferentArgs(t *testing.T) {
	s := newToolLoopState(nil)

	// Same tool but different args each time → no detection
	for i := range 15 {
		args := map[string]any{"path": string(rune('a' + i))}
		h := s.record("list_files", args)
		s.recordResult(h, "access denied")
		level, _ := s.detect("list_files", h)
		if level != "" {
			t.Fatalf("iteration %d: expected no detection for different args, got %q", i, level)
		}
	}
}

func TestToolLoopDetection_DifferentResults(t *testing.T) {
	s := newToolLoopState(nil)

	// Same args but different results each time → progress, no detection
	for i := range 15 {
		h := s.record("web_fetch", map[string]any{"url": "https://example.com"})
		s.recordResult(h, "result content "+string(rune('a'+i)))
		level, _ := s.detect("web_fetch", h)
		if level != "" {
			t.Fatalf("iteration %d: expected no detection for different results, got %q", i, level)
		}
	}
}

func TestToolLoopDetection_MixedTools(t *testing.T) {
	s := newToolLoopState(nil)

	// Alternate between two tools with same result → each tool only hit ~half
	// With 8 iterations, each tool is called 4 times → below critical (5)
	for i := range 8 {
		toolName := "list_files"
		if i%2 == 1 {
			toolName = "read_file"
		}
		h := s.record(toolName, map[string]any{"path": "."})
		s.recordResult(h, "error")
		level, _ := s.detect(toolName, h)
		// Each tool is only called 4 times, should at most warn
		if level == "critical" {
			t.Fatalf("iteration %d: unexpected critical for alternating tools", i)
		}
	}
}

func TestStableJSON(t *testing.T) {
	// Same keys in different order → same hash
	a := stableJSON(map[string]any{"b": 2, "a": 1})
	b := stableJSON(map[string]any{"a": 1, "b": 2})
	if a != b {
		t.Fatalf("stableJSON not deterministic: %q != %q", a, b)
	}
}

// --- Read-only streak detection ---

func TestReadOnlyStreak_Warning(t *testing.T) {
	s := newToolLoopState(nil)
	for range defaultReadOnlyStreakWarning {
		s.recordMutation("read_file")
	}
	level, _ := s.detectReadOnlyStreak()
	if level != "warning" {
		t.Fatalf("expected warning after %d read-only calls, got %q", defaultReadOnlyStreakWarning, level)
	}
}

func TestReadOnlyStreak_Critical(t *testing.T) {
	s := newToolLoopState(nil)
	for range defaultReadOnlyStreakCritical {
		s.recordMutation("list_files")
	}
	level, _ := s.detectReadOnlyStreak()
	if level != "critical" {
		t.Fatalf("expected critical after %d read-only calls, got %q", defaultReadOnlyStreakCritical, level)
	}
}

func TestReadOnlyStreak_ResetByMutation(t *testing.T) {
	s := newToolLoopState(nil)
	// 7 read-only calls → below warning
	for range 7 {
		s.recordMutation("read_file")
	}
	// 1 edit resets streak
	s.recordMutation("edit")
	if s.readOnlyStreak != 0 {
		t.Fatalf("expected streak 0 after edit, got %d", s.readOnlyStreak)
	}
	// 7 more reads → streak = 7, still below warning
	for range 7 {
		s.recordMutation("read_file")
	}
	level, _ := s.detectReadOnlyStreak()
	if level != "" {
		t.Fatalf("expected no detection at streak 7, got %q", level)
	}
}

func TestReadOnlyStreak_ExecNeutral(t *testing.T) {
	s := newToolLoopState(nil)
	// 5 reads → streak = 5
	for range 5 {
		s.recordMutation("read_file")
	}
	// exec does not reset or increment
	s.recordMutation("exec")
	if s.readOnlyStreak != 5 {
		t.Fatalf("expected streak 5 after exec, got %d", s.readOnlyStreak)
	}
	// 5 more reads → streak = 10
	for range 5 {
		s.recordMutation("list_files")
	}
	if s.readOnlyStreak != 10 {
		t.Fatalf("expected streak 10, got %d", s.readOnlyStreak)
	}
	level, _ := s.detectReadOnlyStreak()
	if level != "warning" {
		t.Fatalf("expected warning at streak 10, got %q", level)
	}
}

// --- Same-result cross-args detection ---

func TestSameResult_Warning(t *testing.T) {
	s := newToolLoopState(nil)
	sameResult := "directory listing output"
	for i := range defaultSameResultWarning {
		args := map[string]any{"path": string(rune('a' + i))}
		h := s.record("list_files", args)
		s.recordResult(h, sameResult)
	}
	rh := hashResult(sameResult)
	level, _ := s.detectSameResult("list_files", rh)
	if level != "warning" {
		t.Fatalf("expected warning after %d same-result calls, got %q", defaultSameResultWarning, level)
	}
}

func TestSameResult_Critical(t *testing.T) {
	s := newToolLoopState(nil)
	sameResult := "directory listing output"
	for i := range defaultSameResultCritical {
		args := map[string]any{"path": string(rune('a' + i))}
		h := s.record("list_files", args)
		s.recordResult(h, sameResult)
	}
	rh := hashResult(sameResult)
	level, _ := s.detectSameResult("list_files", rh)
	if level != "critical" {
		t.Fatalf("expected critical after %d same-result calls, got %q", defaultSameResultCritical, level)
	}
}

func TestSameResult_DifferentResults(t *testing.T) {
	s := newToolLoopState(nil)
	// Same tool, same args pattern, but different results each time → no detection
	for i := range 8 {
		args := map[string]any{"path": string(rune('a' + i))}
		h := s.record("list_files", args)
		s.recordResult(h, "result "+string(rune('a'+i)))
	}
	rh := hashResult("result a") // check against the first result
	level, _ := s.detectSameResult("list_files", rh)
	if level != "" {
		t.Fatalf("expected no detection for different results, got %q", level)
	}
}

func TestHashToolCall(t *testing.T) {
	// Same input → same hash
	h1 := hashToolCall("list_files", map[string]any{"path": "."})
	h2 := hashToolCall("list_files", map[string]any{"path": "."})
	if h1 != h2 {
		t.Fatal("hashToolCall not deterministic")
	}

	// Different tool → different hash
	h3 := hashToolCall("read_file", map[string]any{"path": "."})
	if h1 == h3 {
		t.Fatal("different tools should have different hashes")
	}
}


// --- Per-agent config override tests ---

func TestMergeToolLoopConfig_Nil(t *testing.T) {
	cfg := mergeToolLoopConfig(nil)
	if cfg.warningThreshold != defaultToolLoopWarningThreshold {
		t.Fatalf("expected default warning %d, got %d", defaultToolLoopWarningThreshold, cfg.warningThreshold)
	}
	if cfg.criticalThreshold != defaultToolLoopCriticalThreshold {
		t.Fatalf("expected default critical %d, got %d", defaultToolLoopCriticalThreshold, cfg.criticalThreshold)
	}
	if cfg.historySize != defaultToolLoopHistorySize {
		t.Fatalf("expected default history %d, got %d", defaultToolLoopHistorySize, cfg.historySize)
	}
}

func TestMergeToolLoopConfig_PartialOverride(t *testing.T) {
	cfg := mergeToolLoopConfig(&store.ToolLoopConfig{
		WarningThreshold: 5,
		ReadOnlyCritical: 20,
	})
	// Overridden
	if cfg.warningThreshold != 5 {
		t.Fatalf("expected overridden warning 5, got %d", cfg.warningThreshold)
	}
	if cfg.readOnlyCritical != 20 {
		t.Fatalf("expected overridden readOnlyCritical 20, got %d", cfg.readOnlyCritical)
	}
	// Defaults preserved
	if cfg.criticalThreshold != defaultToolLoopCriticalThreshold {
		t.Fatalf("expected default critical %d, got %d", defaultToolLoopCriticalThreshold, cfg.criticalThreshold)
	}
	if cfg.readOnlyWarning != defaultReadOnlyStreakWarning {
		t.Fatalf("expected default readOnlyWarning %d, got %d", defaultReadOnlyStreakWarning, cfg.readOnlyWarning)
	}
}

func TestMergeToolLoopConfig_FullOverride(t *testing.T) {
	cfg := mergeToolLoopConfig(&store.ToolLoopConfig{
		HistorySize:        50,
		WarningThreshold:   6,
		CriticalThreshold:  10,
		ReadOnlyWarning:    15,
		ReadOnlyCritical:   25,
		SameResultWarning:  8,
		SameResultCritical: 12,
	})
	if cfg.historySize != 50 {
		t.Fatalf("expected 50, got %d", cfg.historySize)
	}
	if cfg.warningThreshold != 6 {
		t.Fatalf("expected 6, got %d", cfg.warningThreshold)
	}
	if cfg.criticalThreshold != 10 {
		t.Fatalf("expected 10, got %d", cfg.criticalThreshold)
	}
	if cfg.readOnlyWarning != 15 {
		t.Fatalf("expected 15, got %d", cfg.readOnlyWarning)
	}
	if cfg.readOnlyCritical != 25 {
		t.Fatalf("expected 25, got %d", cfg.readOnlyCritical)
	}
	if cfg.sameResultWarning != 8 {
		t.Fatalf("expected 8, got %d", cfg.sameResultWarning)
	}
	if cfg.sameResultCritical != 12 {
		t.Fatalf("expected 12, got %d", cfg.sameResultCritical)
	}
}

func TestToolLoopDetection_CustomThresholds(t *testing.T) {
	// Custom: warning at 5, critical at 8
	s := newToolLoopState(&store.ToolLoopConfig{
		WarningThreshold:  5,
		CriticalThreshold: 8,
	})

	// 4 calls → below custom warning (5)
	for range 4 {
		h := s.record("list_files", map[string]any{"path": "."})
		s.recordResult(h, "access denied")
		level, _ := s.detect("list_files", h)
		if level != "" {
			t.Fatalf("expected no detection at <5, got %q", level)
		}
	}

	// 5th call → warning
	h := s.record("list_files", map[string]any{"path": "."})
	s.recordResult(h, "access denied")
	level, _ := s.detect("list_files", h)
	if level != "warning" {
		t.Fatalf("expected warning at 5, got %q", level)
	}

	// Fill up to 8 → critical
	for range 3 {
		h = s.record("list_files", map[string]any{"path": "."})
		s.recordResult(h, "access denied")
	}
	level, _ = s.detect("list_files", h)
	if level != "critical" {
		t.Fatalf("expected critical at 8, got %q", level)
	}
}

func TestReadOnlyStreak_CustomThresholds(t *testing.T) {
	s := newToolLoopState(&store.ToolLoopConfig{
		ReadOnlyWarning:  12,
		ReadOnlyCritical: 20,
	})

	// 11 reads → below custom warning (12)
	for range 11 {
		s.recordMutation("read_file")
	}
	level, _ := s.detectReadOnlyStreak()
	if level != "" {
		t.Fatalf("expected no detection at 11, got %q", level)
	}

	// 12th read → warning
	s.recordMutation("read_file")
	level, _ = s.detectReadOnlyStreak()
	if level != "warning" {
		t.Fatalf("expected warning at 12, got %q", level)
	}
}

func TestParseToolLoopConfig(t *testing.T) {
	// Test with valid config
	ad := &store.AgentData{
		OtherConfig: []byte(`{"tool_loop":{"warning_threshold":5,"critical_threshold":10}}`),
	}
	cfg := ad.ParseToolLoopConfig()
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.WarningThreshold != 5 {
		t.Fatalf("expected warning 5, got %d", cfg.WarningThreshold)
	}
	if cfg.CriticalThreshold != 10 {
		t.Fatalf("expected critical 10, got %d", cfg.CriticalThreshold)
	}

	// Test with empty other_config
	ad2 := &store.AgentData{}
	if ad2.ParseToolLoopConfig() != nil {
		t.Fatal("expected nil for empty other_config")
	}

	// Test with other_config but no tool_loop key
	ad3 := &store.AgentData{
		OtherConfig: []byte(`{"thinking_level":"high"}`),
	}
	if ad3.ParseToolLoopConfig() != nil {
		t.Fatal("expected nil when tool_loop key is absent")
	}
}
