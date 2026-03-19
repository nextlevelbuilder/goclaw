package agent

import "testing"

func TestToolLoopDetection_NoLoop(t *testing.T) {
	var s toolLoopState

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
	var s toolLoopState

	var lastLevel string
	for range toolLoopWarningThreshold {
		h := s.record("list_files", map[string]any{"path": "."})
		s.recordResult(h, "access denied")
		lastLevel, _ = s.detect("list_files", h)
	}
	if lastLevel != "warning" {
		t.Fatalf("expected warning after %d calls, got %q", toolLoopWarningThreshold, lastLevel)
	}
}

func TestToolLoopDetection_Critical(t *testing.T) {
	var s toolLoopState

	var lastLevel string
	for range toolLoopCriticalThreshold {
		h := s.record("list_files", map[string]any{"path": "."})
		s.recordResult(h, "access denied")
		lastLevel, _ = s.detect("list_files", h)
	}
	if lastLevel != "critical" {
		t.Fatalf("expected critical after %d calls, got %q", toolLoopCriticalThreshold, lastLevel)
	}
}

func TestToolLoopDetection_DifferentArgs(t *testing.T) {
	var s toolLoopState

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
	var s toolLoopState

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
	var s toolLoopState

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
	var s toolLoopState
	for range readOnlyStreakWarning {
		s.recordMutation("read_file")
	}
	level, _ := s.detectReadOnlyStreak()
	if level != "warning" {
		t.Fatalf("expected warning after %d read-only calls, got %q", readOnlyStreakWarning, level)
	}
}

func TestReadOnlyStreak_Critical(t *testing.T) {
	var s toolLoopState
	for range readOnlyStreakCritical {
		s.recordMutation("list_files")
	}
	level, _ := s.detectReadOnlyStreak()
	if level != "critical" {
		t.Fatalf("expected critical after %d read-only calls, got %q", readOnlyStreakCritical, level)
	}
}

func TestReadOnlyStreak_ResetByMutation(t *testing.T) {
	var s toolLoopState
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
	var s toolLoopState
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
	var s toolLoopState
	sameResult := "directory listing output"
	for i := range sameResultWarning {
		args := map[string]any{"path": string(rune('a' + i))}
		h := s.record("list_files", args)
		s.recordResult(h, sameResult)
	}
	rh := hashResult(sameResult)
	level, _ := s.detectSameResult("list_files", rh)
	if level != "warning" {
		t.Fatalf("expected warning after %d same-result calls, got %q", sameResultWarning, level)
	}
}

func TestSameResult_Critical(t *testing.T) {
	var s toolLoopState
	sameResult := "directory listing output"
	for i := range sameResultCritical {
		args := map[string]any{"path": string(rune('a' + i))}
		h := s.record("list_files", args)
		s.recordResult(h, sameResult)
	}
	rh := hashResult(sameResult)
	level, _ := s.detectSameResult("list_files", rh)
	if level != "critical" {
		t.Fatalf("expected critical after %d same-result calls, got %q", sameResultCritical, level)
	}
}

func TestSameResult_DifferentResults(t *testing.T) {
	var s toolLoopState
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
