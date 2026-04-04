package agent

import (
	"fmt"
	"testing"
)

// ===== Layer 1: Same-args loop detection =====

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

// ===== Group A: Uniqueness-aware read-only streak detection =====

func TestReadOnlyStreak_ExplorationMode_NoKill(t *testing.T) {
	// A1: 12 unique reads ��� ratio=1.0 ��� exploration mode → NO trigger
	var s toolLoopState
	for i := range 12 {
		s.recordMutation("read_file", map[string]any{"path": fmt.Sprintf("/file%d.txt", i)})
	}
	level, _ := s.detectReadOnlyStreak()
	if level == "critical" {
		t.Fatal("12 unique reads should NOT trigger critical in exploration mode")
	}
	// Should also not warn (exploration warning at 24)
	if level != "" {
		t.Fatalf("12 unique reads should not trigger any level, got %q", level)
	}
}

func TestReadOnlyStreak_StuckMode_Kill(t *testing.T) {
	// A2: Same file read 12 times → ratio=1/12 → stuck mode → KILL
	var s toolLoopState
	for range 12 {
		s.recordMutation("read_file", map[string]any{"path": "/same-file.txt"})
	}
	level, _ := s.detectReadOnlyStreak()
	if level != "critical" {
		t.Fatalf("12 reads of same file should trigger critical, got %q", level)
	}
}

func TestReadOnlyStreak_LowUniqueness_Kill(t *testing.T) {
	// A3: 3 unique files read 4 times each (12 total) → ratio=0.25 → stuck → KILL
	var s toolLoopState
	files := []string{"/a.txt", "/b.txt", "/c.txt"}
	for i := range 12 {
		s.recordMutation("read_file", map[string]any{"path": files[i%3]})
	}
	level, _ := s.detectReadOnlyStreak()
	if level != "critical" {
		t.Fatalf("low uniqueness (3/12) should trigger critical, got %q", level)
	}
}

func TestReadOnlyStreak_BoundaryRatio_Stuck(t *testing.T) {
	// A4: 7 unique out of 12 = 0.583 → below 0.6 → stuck mode → critical at 12
	var s toolLoopState
	// First 7 unique
	for i := range 7 {
		s.recordMutation("read_file", map[string]any{"path": fmt.Sprintf("/unique%d.txt", i)})
	}
	// Next 5 repeat first file
	for range 5 {
		s.recordMutation("read_file", map[string]any{"path": "/unique0.txt"})
	}
	if s.readOnlyStreak != 12 {
		t.Fatalf("expected streak=12, got %d", s.readOnlyStreak)
	}
	if s.readOnlyUnique != 7 {
		t.Fatalf("expected unique=7, got %d", s.readOnlyUnique)
	}
	level, _ := s.detectReadOnlyStreak()
	if level != "critical" {
		t.Fatalf("7/12 unique (0.583) should be stuck mode → critical, got %q", level)
	}
}

func TestReadOnlyStreak_BoundaryRatio_Exploration(t *testing.T) {
	// A5: 8 unique out of 12 = 0.667 → above 0.6 → exploration mode → no trigger at 12
	var s toolLoopState
	// First 8 unique
	for i := range 8 {
		s.recordMutation("read_file", map[string]any{"path": fmt.Sprintf("/unique%d.txt", i)})
	}
	// Next 4 repeat first file
	for range 4 {
		s.recordMutation("read_file", map[string]any{"path": "/unique0.txt"})
	}
	if s.readOnlyStreak != 12 {
		t.Fatalf("expected streak=12, got %d", s.readOnlyStreak)
	}
	if s.readOnlyUnique != 8 {
		t.Fatalf("expected unique=8, got %d", s.readOnlyUnique)
	}
	level, _ := s.detectReadOnlyStreak()
	if level != "" {
		t.Fatalf("8/12 unique (0.667) should be exploration mode → no trigger at 12, got %q", level)
	}
}

func TestReadOnlyStreak_ExplorationWarning(t *testing.T) {
	// A6: 24 unique reads �� exploration mode → warning at 24
	var s toolLoopState
	for i := range 24 {
		s.recordMutation("read_file", map[string]any{"path": fmt.Sprintf("/file%d.txt", i)})
	}
	level, _ := s.detectReadOnlyStreak()
	if level != "warning" {
		t.Fatalf("24 unique reads should trigger exploration warning, got %q", level)
	}
}

func TestReadOnlyStreak_ExplorationKill(t *testing.T) {
	// A7: 36 unique reads → exploration mode → critical at 36
	var s toolLoopState
	for i := range 36 {
		s.recordMutation("read_file", map[string]any{"path": fmt.Sprintf("/file%d.txt", i)})
	}
	level, _ := s.detectReadOnlyStreak()
	if level != "critical" {
		t.Fatalf("36 unique reads should trigger exploration critical, got %q", level)
	}
}

func TestReadOnlyStreak_MutationResetsUniqueness(t *testing.T) {
	// A8: 10 unique reads → edit (resets) → 12 unique reads → exploration, no kill
	var s toolLoopState
	for i := range 10 {
		s.recordMutation("read_file", map[string]any{"path": fmt.Sprintf("/file%d.txt", i)})
	}
	// Mutation resets everything
	s.recordMutation("edit", nil)
	if s.readOnlyStreak != 0 {
		t.Fatalf("expected streak=0 after edit, got %d", s.readOnlyStreak)
	}
	if s.readOnlyUnique != 0 {
		t.Fatalf("expected unique=0 after edit, got %d", s.readOnlyUnique)
	}
	if s.seenReadArgs != nil {
		t.Fatal("expected seenReadArgs=nil after edit")
	}
	// 12 more unique reads → exploration mode
	for i := range 12 {
		s.recordMutation("read_file", map[string]any{"path": fmt.Sprintf("/new%d.txt", i)})
	}
	level, _ := s.detectReadOnlyStreak()
	if level == "critical" {
		t.Fatal("12 unique reads after mutation reset should NOT trigger critical")
	}
}

func TestReadOnlyStreak_ZeroStreak(t *testing.T) {
	// A9: Division-by-zero guard — fresh state �� no panic
	var s toolLoopState
	level, _ := s.detectReadOnlyStreak()
	if level != "" {
		t.Fatalf("expected no detection on zero streak, got %q", level)
	}
}

// ===== Group B: Action-aware team_tasks =====

func TestReadOnlyStreak_TeamTasksProgressNeutral(t *testing.T) {
	// B1: team_tasks(progress) is neutral — streak unchanged
	var s toolLoopState
	for range 5 {
		s.recordMutation("read_file", map[string]any{"path": "/file.txt"})
	}
	if s.readOnlyStreak != 5 {
		t.Fatalf("expected streak=5, got %d", s.readOnlyStreak)
	}
	s.recordMutation("team_tasks", map[string]any{"action": "progress", "percent": 50})
	if s.readOnlyStreak != 5 {
		t.Fatalf("team_tasks(progress) should be neutral, streak should stay 5, got %d", s.readOnlyStreak)
	}
}

func TestReadOnlyStreak_TeamTasksListReadOnly(t *testing.T) {
	// B2: team_tasks(list) is read-only — streak increments
	var s toolLoopState
	for range 5 {
		s.recordMutation("read_file", map[string]any{"path": "/file.txt"})
	}
	s.recordMutation("team_tasks", map[string]any{"action": "list"})
	if s.readOnlyStreak != 6 {
		t.Fatalf("team_tasks(list) should increment streak to 6, got %d", s.readOnlyStreak)
	}
}

func TestReadOnlyStreak_TeamTasksSearchReadOnly(t *testing.T) {
	// B3: team_tasks(search) is read-only — streak increments
	var s toolLoopState
	for range 5 {
		s.recordMutation("read_file", map[string]any{"path": "/file.txt"})
	}
	s.recordMutation("team_tasks", map[string]any{"action": "search", "query": "test"})
	if s.readOnlyStreak != 6 {
		t.Fatalf("team_tasks(search) should increment streak to 6, got %d", s.readOnlyStreak)
	}
}

func TestReadOnlyStreak_TeamTasksGetReadOnly(t *testing.T) {
	// B3b: team_tasks(get) is read-only — streak increments
	var s toolLoopState
	for range 5 {
		s.recordMutation("read_file", map[string]any{"path": "/file.txt"})
	}
	s.recordMutation("team_tasks", map[string]any{"action": "get", "task_id": "abc"})
	if s.readOnlyStreak != 6 {
		t.Fatalf("team_tasks(get) should increment streak to 6, got %d", s.readOnlyStreak)
	}
}

func TestReadOnlyStreak_TeamTasksCreateMutating(t *testing.T) {
	// B4: team_tasks(create) is mutating — streak resets to 0
	var s toolLoopState
	for range 5 {
		s.recordMutation("read_file", map[string]any{"path": "/file.txt"})
	}
	s.recordMutation("team_tasks", map[string]any{"action": "create", "subject": "test task"})
	if s.readOnlyStreak != 0 {
		t.Fatalf("team_tasks(create) should reset streak to 0, got %d", s.readOnlyStreak)
	}
}

func TestReadOnlyStreak_TeamTasksCompleteMutating(t *testing.T) {
	// B5: team_tasks(complete) is mutating — streak resets to 0
	var s toolLoopState
	for range 5 {
		s.recordMutation("read_file", map[string]any{"path": "/file.txt"})
	}
	s.recordMutation("team_tasks", map[string]any{"action": "complete", "result": "done"})
	if s.readOnlyStreak != 0 {
		t.Fatalf("team_tasks(complete) should reset streak to 0, got %d", s.readOnlyStreak)
	}
}

func TestReadOnlyStreak_TeamTasksCommentMutating(t *testing.T) {
	// B6: team_tasks(comment) is mutating — streak resets to 0
	var s toolLoopState
	for range 5 {
		s.recordMutation("read_file", map[string]any{"path": "/file.txt"})
	}
	s.recordMutation("team_tasks", map[string]any{"action": "comment", "text": "found something"})
	if s.readOnlyStreak != 0 {
		t.Fatalf("team_tasks(comment) should reset streak to 0, got %d", s.readOnlyStreak)
	}
}

func TestReadOnlyStreak_TeamTasksNoAction(t *testing.T) {
	// B7: team_tasks with no action arg → neutral (no crash)
	var s toolLoopState
	for range 5 {
		s.recordMutation("read_file", map[string]any{"path": "/file.txt"})
	}
	// Empty args — should not panic
	s.recordMutation("team_tasks", map[string]any{})
	if s.readOnlyStreak != 5 {
		t.Fatalf("team_tasks with no action should be neutral, streak should stay 5, got %d", s.readOnlyStreak)
	}
	// Nil args — should not panic
	s.recordMutation("team_tasks", nil)
	if s.readOnlyStreak != 5 {
		t.Fatalf("team_tasks with nil args should be neutral, streak should stay 5, got %d", s.readOnlyStreak)
	}
}

func TestReadOnlyStreak_TeamTasksUnknownAction(t *testing.T) {
	// B8: Unknown action → mutating (safe default)
	var s toolLoopState
	for range 5 {
		s.recordMutation("read_file", map[string]any{"path": "/file.txt"})
	}
	s.recordMutation("team_tasks", map[string]any{"action": "some_future_action"})
	if s.readOnlyStreak != 0 {
		t.Fatalf("team_tasks with unknown action should reset streak to 0, got %d", s.readOnlyStreak)
	}
}

// ===== Group C: Trace replay (integration-style) =====

func TestReadOnlyStreak_TraceReplay_Issue506(t *testing.T) {
	// C1: Exact trace scenario from issue #506.
	// Agent explores monorepo: 12 unique reads + 1 team_tasks(progress).
	// Should NOT trigger any critical kill.
	var s toolLoopState

	// Span 2: read_file(SKILL.md)
	s.recordMutation("read_file", map[string]any{"path": "/app/data/skills-store/builder-test-fixer/1/SKILL.md"})
	level, _ := s.detectReadOnlyStreak()
	if level == "critical" {
		t.Fatal("should not trigger critical at span 2")
	}

	// Span 4: team_tasks(progress, 10%)
	s.recordMutation("team_tasks", map[string]any{"action": "progress", "percent": float64(10), "text": "started"})
	if s.readOnlyStreak != 1 {
		t.Fatalf("team_tasks(progress) should be neutral, streak should stay 1, got %d", s.readOnlyStreak)
	}

	// Span 6: read_file(plan.md)
	s.recordMutation("read_file", map[string]any{"path": "/app/workspace/teams/019d2ad3/order-booking-optimization-plan-2026-03-26.md"})
	// Span 7: read_file(qa-review.md)
	s.recordMutation("read_file", map[string]any{"path": "/app/workspace/teams/019d2ad3/order-booking-phase-2-qa-review-2026-03-26.md"})
	// Span 8: read_file(risk-review.md)
	s.recordMutation("read_file", map[string]any{"path": "/app/workspace/teams/019d2ad3/order-booking-optimization-risk-review-2026-03-26.md"})
	// Span 9: list_files(teams/...)
	s.recordMutation("list_files", map[string]any{"path": "/app/workspace/teams/019d2ad3"})
	// Span 10: list_files(.)
	s.recordMutation("list_files", map[string]any{"path": "."})
	// Span 12: list_files(order-booking/)
	s.recordMutation("list_files", map[string]any{"path": "order-booking"})

	// At this point: streak=8 (warning threshold in stuck mode, but exploration mode).
	// Verify warning (if any) is not critical.
	level, _ = s.detectReadOnlyStreak()
	if level == "critical" {
		t.Fatal("should not trigger critical at streak=8 in exploration mode")
	}

	// Span 13: read_file(api/package.json)
	s.recordMutation("read_file", map[string]any{"path": "order-booking/apps/api/package.json"})
	// Span 14: read_file(web/package.json)
	s.recordMutation("read_file", map[string]any{"path": "order-booking/apps/web/package.json"})
	// Span 15: read_file(package.json)
	s.recordMutation("read_file", map[string]any{"path": "order-booking/package.json"})
	// Span 16: read_file(database/package.json)
	s.recordMutation("read_file", map[string]any{"path": "order-booking/packages/database/package.json"})

	// Count: 1 (SKILL.md) + 0 (progress neutral) + 3 (plan/qa/risk) + 3 (list_files) + 4 (pkg.jsons) = 11 reads
	if s.readOnlyStreak != 11 {
		t.Fatalf("expected streak=11, got %d", s.readOnlyStreak)
	}
	if s.readOnlyUnique != 11 {
		t.Fatalf("expected unique=11, got %d", s.readOnlyUnique)
	}
	level, _ = s.detectReadOnlyStreak()
	if level == "critical" {
		t.Fatal("REGRESSION: 11 unique reads should NOT trigger critical — this was the #506 false positive")
	}

	// Span 17: read_file(types/package.json)
	s.recordMutation("read_file", map[string]any{"path": "order-booking/packages/types/package.json"})
	// Span 18: read_file(turbo.json)
	s.recordMutation("read_file", map[string]any{"path": "order-booking/turbo.json"})

	// streak=13, all unique. In old code this would have been killed at 12.
	// With uniqueness-aware detection, exploration mode continues.
	if s.readOnlyStreak != 13 {
		t.Fatalf("expected streak=13, got %d", s.readOnlyStreak)
	}
	level, _ = s.detectReadOnlyStreak()
	if level == "critical" {
		t.Fatal("13 unique reads should not trigger critical in exploration mode")
	}
	if level != "" {
		t.Fatalf("13 unique reads in exploration mode should have no trigger, got %q", level)
	}
}

func TestReadOnlyStreak_TraceReplay_StuckLoop(t *testing.T) {
	// C2: Agent reads same 2 files alternating 12 times → stuck → KILL
	var s toolLoopState
	for i := range 12 {
		file := "/a.txt"
		if i%2 == 1 {
			file = "/b.txt"
		}
		s.recordMutation("read_file", map[string]any{"path": file})
	}
	// ratio = 2/12 = 0.167 → stuck mode
	if s.readOnlyUnique != 2 {
		t.Fatalf("expected unique=2, got %d", s.readOnlyUnique)
	}
	level, _ := s.detectReadOnlyStreak()
	if level != "critical" {
		t.Fatalf("stuck loop (2 files alternating) should trigger critical at 12, got %q", level)
	}
}

func TestReadOnlyStreak_ExactBoundaryRatio(t *testing.T) {
	// E4: Exactly 0.6 ratio → stuck mode (strict greater-than check)
	// 6 unique out of 10 = 0.6 → NOT > 0.6 → stuck mode → warning at 10
	var s toolLoopState
	for i := range 6 {
		s.recordMutation("read_file", map[string]any{"path": fmt.Sprintf("/u%d.txt", i)})
	}
	for range 4 {
		s.recordMutation("read_file", map[string]any{"path": "/u0.txt"})
	}
	if s.readOnlyStreak != 10 {
		t.Fatalf("expected streak=10, got %d", s.readOnlyStreak)
	}
	if s.readOnlyUnique != 6 {
		t.Fatalf("expected unique=6, got %d", s.readOnlyUnique)
	}
	level, _ := s.detectReadOnlyStreak()
	// ratio=0.6, NOT > 0.6, so stuck mode. Streak=10 >= warning=8 → warning.
	if level != "warning" {
		t.Fatalf("exact 0.6 ratio should be stuck mode → warning at 10, got %q", level)
	}
}

// ===== Existing tests — updated for new recordMutation(toolName, args) signature =====

func TestReadOnlyStreak_Warning(t *testing.T) {
	var s toolLoopState
	for range readOnlyStreakWarning {
		s.recordMutation("read_file", nil)
	}
	level, _ := s.detectReadOnlyStreak()
	// With nil args, all hashes are identical → unique=1 → ratio=1/8=0.125 → stuck mode
	if level != "warning" {
		t.Fatalf("expected warning after %d read-only calls, got %q", readOnlyStreakWarning, level)
	}
}

func TestReadOnlyStreak_Critical(t *testing.T) {
	var s toolLoopState
	for range readOnlyStreakCritical {
		s.recordMutation("list_files", nil)
	}
	level, _ := s.detectReadOnlyStreak()
	// nil args → unique=1 → ratio=1/12=0.083 → stuck mode → critical
	if level != "critical" {
		t.Fatalf("expected critical after %d read-only calls, got %q", readOnlyStreakCritical, level)
	}
}

func TestReadOnlyStreak_ResetByMutation(t *testing.T) {
	var s toolLoopState
	// 7 read-only calls → below warning
	for range 7 {
		s.recordMutation("read_file", nil)
	}
	// 1 edit resets streak
	s.recordMutation("edit", nil)
	if s.readOnlyStreak != 0 {
		t.Fatalf("expected streak 0 after edit, got %d", s.readOnlyStreak)
	}
	// 7 more reads → streak = 7, still below warning
	for range 7 {
		s.recordMutation("read_file", nil)
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
		s.recordMutation("read_file", nil)
	}
	// exec does not reset or increment
	s.recordMutation("exec", nil)
	if s.readOnlyStreak != 5 {
		t.Fatalf("expected streak 5 after exec, got %d", s.readOnlyStreak)
	}
	// 5 more reads → streak = 10
	for range 5 {
		s.recordMutation("list_files", nil)
	}
	if s.readOnlyStreak != 10 {
		t.Fatalf("expected streak 10, got %d", s.readOnlyStreak)
	}
	level, _ := s.detectReadOnlyStreak()
	if level != "warning" {
		t.Fatalf("expected warning at streak 10, got %q", level)
	}
}

func TestReadOnlyStreak_MCPNeutral(t *testing.T) {
	var s toolLoopState
	// 5 reads → streak = 5
	for range 5 {
		s.recordMutation("read_file", nil)
	}
	// MCP tools should not reset or increment (same as exec)
	s.recordMutation("mcp_gmail__query_gmail_emails", nil)
	if s.readOnlyStreak != 5 {
		t.Fatalf("expected streak 5 after mcp tool, got %d", s.readOnlyStreak)
	}
	s.recordMutation("mcp_gmail__get_gmail_email", nil)
	if s.readOnlyStreak != 5 {
		t.Fatalf("expected streak 5 after second mcp tool, got %d", s.readOnlyStreak)
	}
	// 7 more reads → streak = 12, should hit critical (stuck mode since nil args → unique=1)
	for range 7 {
		s.recordMutation("list_files", nil)
	}
	if s.readOnlyStreak != 12 {
		t.Fatalf("expected streak 12, got %d", s.readOnlyStreak)
	}
}

func TestReadOnlyStreak_MCPOnlyNeverTriggers(t *testing.T) {
	var s toolLoopState
	// 20 consecutive MCP tool calls → streak should stay 0
	for range 20 {
		s.recordMutation("mcp_gmail__query_gmail_emails", nil)
	}
	if s.readOnlyStreak != 0 {
		t.Fatalf("expected streak 0 after 20 mcp-only calls, got %d", s.readOnlyStreak)
	}
	level, _ := s.detectReadOnlyStreak()
	if level != "" {
		t.Fatalf("expected no detection for mcp-only calls, got %q", level)
	}
}

// ===== Layer 2: Same-result cross-args detection =====

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

// ===== Group D: Interaction between uniqueness and team_tasks =====

func TestReadOnlyStreak_ProgressBetweenReads(t *testing.T) {
	// D1: reads → team_tasks(progress) → more reads → streak continues
	var s toolLoopState
	for i := range 5 {
		s.recordMutation("read_file", map[string]any{"path": fmt.Sprintf("/file%d.txt", i)})
	}
	s.recordMutation("team_tasks", map[string]any{"action": "progress", "percent": 30})
	// Streak should stay at 5 (progress is neutral)
	if s.readOnlyStreak != 5 {
		t.Fatalf("expected streak=5 after progress, got %d", s.readOnlyStreak)
	}
	for i := range 5 {
		s.recordMutation("read_file", map[string]any{"path": fmt.Sprintf("/more%d.txt", i)})
	}
	// Streak=10, unique=10 → exploration mode, below warning
	if s.readOnlyStreak != 10 {
		t.Fatalf("expected streak=10, got %d", s.readOnlyStreak)
	}
	if s.readOnlyUnique != 10 {
		t.Fatalf("expected unique=10, got %d", s.readOnlyUnique)
	}
}

func TestReadOnlyStreak_ListBetweenReads(t *testing.T) {
	// D2: reads → team_tasks(list) → more reads → streak continues (list is read-only)
	var s toolLoopState
	for i := range 5 {
		s.recordMutation("read_file", map[string]any{"path": fmt.Sprintf("/file%d.txt", i)})
	}
	s.recordMutation("team_tasks", map[string]any{"action": "list"})
	// team_tasks(list) is read-only → streak=6
	if s.readOnlyStreak != 6 {
		t.Fatalf("expected streak=6 after team_tasks(list), got %d", s.readOnlyStreak)
	}
}

func TestReadOnlyStreak_CreateBetweenReads(t *testing.T) {
	// D3: reads → team_tasks(create) → more reads → new streak from 0
	var s toolLoopState
	for i := range 5 {
		s.recordMutation("read_file", map[string]any{"path": fmt.Sprintf("/file%d.txt", i)})
	}
	s.recordMutation("team_tasks", map[string]any{"action": "create", "subject": "task"})
	if s.readOnlyStreak != 0 {
		t.Fatalf("expected streak=0 after create, got %d", s.readOnlyStreak)
	}
	// New reads start fresh
	for range 3 {
		s.recordMutation("read_file", map[string]any{"path": "/new.txt"})
	}
	if s.readOnlyStreak != 3 {
		t.Fatalf("expected streak=3, got %d", s.readOnlyStreak)
	}
}
