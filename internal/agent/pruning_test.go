package agent

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// ─── resolvePruningSettings ───────────────────────────────────────────────

func TestResolvePruningSettings_Defaults(t *testing.T) {
	s := resolvePruningSettings(nil)
	if s.keepLastAssistants != defaultKeepLastAssistants {
		t.Errorf("keepLastAssistants = %d, want %d", s.keepLastAssistants, defaultKeepLastAssistants)
	}
	if s.softTrimRatio != defaultSoftTrimRatio {
		t.Errorf("softTrimRatio = %v, want %v", s.softTrimRatio, defaultSoftTrimRatio)
	}
	if s.hardClearRatio != defaultHardClearRatio {
		t.Errorf("hardClearRatio = %v, want %v", s.hardClearRatio, defaultHardClearRatio)
	}
	if !s.hardClearEnabled {
		t.Error("hardClearEnabled should be true by default")
	}
	if s.hardClearPlaceholder != defaultHardClearPlaceholder {
		t.Errorf("placeholder = %q, want %q", s.hardClearPlaceholder, defaultHardClearPlaceholder)
	}
}

func TestResolvePruningSettings_CustomValues(t *testing.T) {
	enabled := false
	cfg := &config.ContextPruningConfig{
		KeepLastAssistants:   5,
		SoftTrimRatio:        0.4,
		HardClearRatio:       0.6,
		MinPrunableToolChars: 10000,
		SoftTrim: &config.ContextPruningSoftTrim{
			MaxChars:  2000,
			HeadChars: 800,
			TailChars: 800,
		},
		HardClear: &config.ContextPruningHardClear{
			Enabled:     &enabled,
			Placeholder: "[gone]",
		},
	}
	s := resolvePruningSettings(cfg)
	if s.keepLastAssistants != 5 {
		t.Errorf("keepLastAssistants = %d, want 5", s.keepLastAssistants)
	}
	if s.softTrimRatio != 0.4 {
		t.Errorf("softTrimRatio = %v, want 0.4", s.softTrimRatio)
	}
	if s.hardClearEnabled {
		t.Error("hardClearEnabled should be false")
	}
	if s.hardClearPlaceholder != "[gone]" {
		t.Errorf("placeholder = %q, want [gone]", s.hardClearPlaceholder)
	}
	if s.softTrimMaxChars != 2000 {
		t.Errorf("softTrimMaxChars = %d, want 2000", s.softTrimMaxChars)
	}
}

func TestResolvePruningSettings_InvalidRatiosIgnored(t *testing.T) {
	// Ratios out of (0,1] range should be ignored, defaults kept.
	cfg := &config.ContextPruningConfig{
		SoftTrimRatio:  1.5, // invalid
		HardClearRatio: 0,   // invalid (0 not > 0)
	}
	s := resolvePruningSettings(cfg)
	if s.softTrimRatio != defaultSoftTrimRatio {
		t.Errorf("invalid SoftTrimRatio should keep default, got %v", s.softTrimRatio)
	}
	if s.hardClearRatio != defaultHardClearRatio {
		t.Errorf("invalid HardClearRatio should keep default, got %v", s.hardClearRatio)
	}
}

// ─── findAssistantCutoff ──────────────────────────────────────────────────

func TestFindAssistantCutoff_FewerThanKeepLast(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}
	// keepLast=3 but only 1 assistant → returns -1 (not enough to protect)
	idx := findAssistantCutoff(msgs, 3)
	if idx != -1 {
		t.Errorf("cutoff = %d, want -1", idx)
	}
}

func TestFindAssistantCutoff_ExactlyKeepLast(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "next"},
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "end"},
		{Role: "assistant", Content: "a3"},
	}
	// keepLast=3: protect all 3 assistant messages → cutoff at index of first assistant
	idx := findAssistantCutoff(msgs, 3)
	if idx != 1 {
		t.Errorf("cutoff = %d, want 1", idx)
	}
}

func TestFindAssistantCutoff_MoreThanKeepLast(t *testing.T) {
	// 5 assistants, keepLast=2 → cutoff at 3rd-from-last assistant
	msgs := []providers.Message{
		{Role: "assistant", Content: "a0"}, // idx 0
		{Role: "assistant", Content: "a1"}, // idx 1
		{Role: "assistant", Content: "a2"}, // idx 2
		{Role: "assistant", Content: "a3"}, // idx 3
		{Role: "assistant", Content: "a4"}, // idx 4
	}
	idx := findAssistantCutoff(msgs, 2)
	// 2nd from last = idx 3
	if idx != 3 {
		t.Errorf("cutoff = %d, want 3", idx)
	}
}

func TestFindAssistantCutoff_ZeroKeepLast(t *testing.T) {
	msgs := []providers.Message{{Role: "assistant", Content: "x"}}
	// keepLast=0 → return len(msgs) (protect nothing)
	idx := findAssistantCutoff(msgs, 0)
	if idx != len(msgs) {
		t.Errorf("cutoff = %d, want %d", idx, len(msgs))
	}
}

// ─── takeHead / takeTail ──────────────────────────────────────────────────

func TestTakeHead(t *testing.T) {
	cases := []struct {
		input string
		n     int
		want  string
	}{
		{"hello", 3, "hel"},
		{"hello", 10, "hello"},
		{"hello", 0, ""},
		{"αβγδε", 3, "αβγ"}, // unicode
	}
	for _, c := range cases {
		got := takeHead(c.input, c.n)
		if got != c.want {
			t.Errorf("takeHead(%q, %d) = %q, want %q", c.input, c.n, got, c.want)
		}
	}
}

func TestTakeTail(t *testing.T) {
	cases := []struct {
		input string
		n     int
		want  string
	}{
		{"hello", 3, "llo"},
		{"hello", 10, "hello"},
		{"hello", 0, ""},
		{"αβγδε", 2, "δε"}, // unicode
	}
	for _, c := range cases {
		got := takeTail(c.input, c.n)
		if got != c.want {
			t.Errorf("takeTail(%q, %d) = %q, want %q", c.input, c.n, got, c.want)
		}
	}
}

// ─── estimateMessageChars ─────────────────────────────────────────────────

func TestEstimateMessageChars(t *testing.T) {
	m := providers.Message{Content: "αβγ"} // 3 runes, 6 bytes
	if got := estimateMessageChars(m); got != 3 {
		t.Errorf("estimateMessageChars = %d, want 3", got)
	}
}

// ─── pruneContextMessages — mode off ────────────────────────────────────

func TestPruneContextMessages_ModeOff_ReturnsOriginal(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}
	cfg := &config.ContextPruningConfig{Mode: "off"}
	got := pruneContextMessages(msgs, 100000, cfg, nil, "", nil)
	if len(got) != len(msgs) {
		t.Error("mode=off should return original slice")
	}
}

func TestPruneContextMessages_ZeroWindow_ReturnsOriginal(t *testing.T) {
	msgs := []providers.Message{{Role: "user", Content: "hi"}}
	got := pruneContextMessages(msgs, 0, nil, nil, "", nil)
	if len(got) != len(msgs) {
		t.Error("zero context window should return original")
	}
}

func TestPruneContextMessages_SmallContext_NoChange(t *testing.T) {
	// Total content << softTrimRatio → no pruning.
	msgs := []providers.Message{
		{Role: "user", Content: "short"},
		{Role: "assistant", Content: "reply", ToolCalls: nil},
		{Role: "user", Content: "another"},
		{Role: "assistant", Content: "done"},
	}
	// Very large window → ratio tiny → nothing pruned.
	got := pruneContextMessages(msgs, 1_000_000, nil, nil, "", nil)
	if len(got) != len(msgs) {
		t.Errorf("small context: len=%d, want %d", len(got), len(msgs))
	}
}

func TestPruneContextMessages_SoftTrim_LongToolResult(t *testing.T) {
	// Build scenario: 4 assistant messages (so keepLast=3 protects last 3),
	// and an old tool result that is very long.
	toolContent := strings.Repeat("X", 10000) // long

	msgs := []providers.Message{
		{Role: "user", Content: "step1"},
		{Role: "assistant", Content: "a0", ToolCalls: []providers.ToolCall{{ID: "tc1", Name: "t"}}},
		{Role: "tool", Content: toolContent, ToolCallID: "tc1"},
		{Role: "user", Content: "step2"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "step3"},
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "step4"},
		{Role: "assistant", Content: "a3"},
	}

	cfg := &config.ContextPruningConfig{
		Mode:          "cache-ttl", // enable pruning
		SoftTrimRatio: 0.01,        // threshold very low → prune immediately
		HardClearRatio: 0.99,       // don't hard clear
		SoftTrim: &config.ContextPruningSoftTrim{
			MaxChars:  100,
			HeadChars: 50,
			TailChars: 50,
		},
	}
	// Use a context window that makes ratio exceed threshold.
	got := pruneContextMessages(msgs, 100, cfg, nil, "", nil)

	// Tool result should be trimmed — shorter than original.
	for _, m := range got {
		if m.Role == "tool" {
			if len(m.Content) >= len(toolContent) {
				t.Error("expected tool result to be trimmed")
			}
			return
		}
	}
	// If tool message not found, that's also acceptable (hard-cleared).
}

func TestPruneContextMessages_HardClear_WhenRatioVeryHigh(t *testing.T) {
	// Set up context that forces hard clear.
	toolContent := strings.Repeat("Y", 5000)

	msgs := []providers.Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", Content: "a0", ToolCalls: []providers.ToolCall{{ID: "tc1", Name: "t"}}},
		{Role: "tool", Content: toolContent, ToolCallID: "tc1"},
		{Role: "user", Content: "q2"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q3"},
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "q4"},
		{Role: "assistant", Content: "a3"},
	}

	enabled := true
	cfg := &config.ContextPruningConfig{
		Mode:                 "cache-ttl", // enable pruning
		SoftTrimRatio:        0.001,
		HardClearRatio:       0.001,
		MinPrunableToolChars: 1, // very low threshold
		SoftTrim: &config.ContextPruningSoftTrim{
			MaxChars:  100,
			HeadChars: 50,
			TailChars: 50,
		},
		HardClear: &config.ContextPruningHardClear{
			Enabled:     &enabled,
			Placeholder: "[cleared]",
		},
	}

	got := pruneContextMessages(msgs, 10, cfg, nil, "", nil)

	// Tool result should be replaced by placeholder or heavily trimmed.
	for _, m := range got {
		if m.Role == "tool" {
			if len(m.Content) >= len(toolContent) {
				t.Error("tool content should have been reduced")
			}
		}
	}
}

// ─── media tool higher budget ───────────────────────────────────────────

func TestPruneContextMessages_MediaToolHigherBudget(t *testing.T) {
	// 7000-char tool result: exceeds default softTrimMaxChars (6000)
	// but under media budget (8000). Media result should NOT be trimmed;
	// non-media result should be trimmed.
	content := strings.Repeat("Z", 7000)

	msgs := []providers.Message{
		{Role: "user", Content: "q"},
		// Old assistant with two tool calls: one read_image, one exec
		{Role: "assistant", Content: "a0", ToolCalls: []providers.ToolCall{
			{ID: "tc_img", Name: "read_image"},
			{ID: "tc_exec", Name: "exec"},
		}},
		{Role: "tool", Content: content, ToolCallID: "tc_img"},
		{Role: "tool", Content: content, ToolCallID: "tc_exec"},
		{Role: "user", Content: "q2"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q3"},
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "q4"},
		{Role: "assistant", Content: "a3"},
	}

	cfg := &config.ContextPruningConfig{
		Mode:           "cache-ttl", // enable pruning
		SoftTrimRatio:  0.01,        // force prune
		HardClearRatio: 0.999,       // don't hard clear
	}

	// Use 7000 tokens (28K chars) so per-result guard (30% = 8.4K) doesn't
	// trigger on 7K-char results, isolating the soft trim behavior.
	got := pruneContextMessages(msgs, 7000, cfg, nil, "", nil)

	// read_image result (7000 chars < 8000 media budget) → NOT trimmed
	imgResult := got[2]
	if imgResult.Content != content {
		t.Errorf("read_image result should NOT be trimmed, got len=%d (want %d)", len(imgResult.Content), len(content))
	}

	// exec result (5000 chars > 3000 default budget) → trimmed
	execResult := got[3]
	if len(execResult.Content) >= len(content) {
		t.Error("exec result should be trimmed (exceeds default budget)")
	}
}

func TestPruneContextMessages_MediaToolSkipsHardClear(t *testing.T) {
	content := strings.Repeat("M", 5000)

	msgs := []providers.Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", Content: "a0", ToolCalls: []providers.ToolCall{
			{ID: "tc_img", Name: "read_image"},
			{ID: "tc_exec", Name: "exec"},
		}},
		{Role: "tool", Content: content, ToolCallID: "tc_img"},
		{Role: "tool", Content: content, ToolCallID: "tc_exec"},
		{Role: "user", Content: "q2"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q3"},
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "q4"},
		{Role: "assistant", Content: "a3"},
	}

	enabled := true
	cfg := &config.ContextPruningConfig{
		Mode:                 "cache-ttl", // enable pruning
		SoftTrimRatio:        0.001,
		HardClearRatio:       0.001,
		MinPrunableToolChars: 1,
		HardClear: &config.ContextPruningHardClear{
			Enabled:     &enabled,
			Placeholder: "[cleared]",
		},
	}

	got := pruneContextMessages(msgs, 5000, cfg, nil, "", nil)

	// read_image result should survive hard clear (only soft-trimmed at most)
	imgResult := got[2]
	if imgResult.Content == "[cleared]" {
		t.Error("read_image result should NOT be hard-cleared")
	}

	// exec result should be hard-cleared
	execResult := got[3]
	if execResult.Content != "[cleared]" {
		t.Errorf("exec result should be hard-cleared, got len=%d", len(execResult.Content))
	}
}

func TestBuildToolCallNameMap(t *testing.T) {
	msgs := []providers.Message{
		{Role: "assistant", ToolCalls: []providers.ToolCall{
			{ID: "a", Name: "read_image"},
			{ID: "b", Name: "exec"},
		}},
		{Role: "tool", Content: "result", ToolCallID: "a"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{
			{ID: "c", Name: "web_fetch"},
		}},
	}
	m := buildToolCallNameMap(msgs)
	if m["a"] != "read_image" {
		t.Errorf("m[a] = %q, want read_image", m["a"])
	}
	if m["b"] != "exec" {
		t.Errorf("m[b] = %q, want exec", m["b"])
	}
	if m["c"] != "web_fetch" {
		t.Errorf("m[c] = %q, want web_fetch", m["c"])
	}
	if _, ok := m["nonexistent"]; ok {
		t.Error("unexpected key in map")
	}
}

// ─── hasImportantTail ────────────────────────────────────────────────────

func TestHasImportantTail_MatchesErrorKeyword(t *testing.T) {
	content := strings.Repeat("x", 1000) + " error occurred"
	if !hasImportantTail(content) {
		t.Error("expected important tail (error keyword)")
	}
}

func TestHasImportantTail_ShortContentNoMatch(t *testing.T) {
	// No keywords → false
	if hasImportantTail("no important content here at all") {
		t.Error("expected no important tail")
	}
}

// ─── Characterization tests (Phase 01) ────────────────────────────────────
// Lock current (buggy-vs-TS) behavior before refactoring. Subsequent phases
// update these tests to new expected values.

// makeLargeHistoryFixture creates a history with a large tool result for opt-in tests.
// contextWindow=5000 → charWindow=20000, total ~8030 chars → ratio ~40% ≥ 25%.
func makeLargeHistoryFixture() []providers.Message {
	return []providers.Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{{ID: "c1", Name: "read_file"}}},
		{Role: "tool", ToolCallID: "c1", Content: strings.Repeat("x", 8000)},
		{Role: "assistant", Content: "a1"},
		{Role: "assistant", Content: "a2"},
		{Role: "assistant", Content: "a3"},
	}
}

func TestPruneContextMessages_NilCfg_NoOp(t *testing.T) {
	// Phase 04: nil cfg → no pruning (opt-in). Matches TS design.
	msgs := makeLargeHistoryFixture()
	got := pruneContextMessages(msgs, 5000, nil, nil, "", nil)
	if estimateMessageChars(got[2]) != 8000 {
		t.Errorf("nil cfg should be no-op (opt-in). Got %d chars", estimateMessageChars(got[2]))
	}
}

func TestPruneContextMessages_EmptyMode_NoOp(t *testing.T) {
	cfg := &config.ContextPruningConfig{Mode: ""}
	msgs := makeLargeHistoryFixture()
	got := pruneContextMessages(msgs, 5000, cfg, nil, "", nil)
	if estimateMessageChars(got[2]) != 8000 {
		t.Errorf("empty mode should be no-op. Got %d chars", estimateMessageChars(got[2]))
	}
}

func TestPruneContextMessages_UnknownMode_NoOp(t *testing.T) {
	cfg := &config.ContextPruningConfig{Mode: "bogus"}
	msgs := makeLargeHistoryFixture()
	got := pruneContextMessages(msgs, 5000, cfg, nil, "", nil)
	if estimateMessageChars(got[2]) != 8000 {
		t.Errorf("unknown mode should be no-op. Got %d chars", estimateMessageChars(got[2]))
	}
}

func TestPruneContextMessages_CacheTtlMode_Prunes(t *testing.T) {
	cfg := &config.ContextPruningConfig{Mode: "cache-ttl"}
	msgs := makeLargeHistoryFixture()
	got := pruneContextMessages(msgs, 5000, cfg, nil, "", nil)
	if estimateMessageChars(got[2]) >= 8000 {
		t.Errorf("cache-ttl mode should prune. Got unchanged content.")
	}
}

func TestPruneContextMessages_Pass0_RemovedSuffixAbsent(t *testing.T) {
	// Phase 02: Pass 0 removed. This test asserts the distinctive suffix is GONE.
	// Pass 1 still trims via ratio gate, but with different suffix format.
	//
	// contextWindow=10000 → charWindow=40000; msg=15000 chars = 37.5% ≥ 25% softTrimRatio.
	// Pass 1 trims the large result.
	msgs := []providers.Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{{ID: "c1", Name: "read_file"}}},
		{Role: "tool", ToolCallID: "c1", Content: strings.Repeat("x", 15000)},
		{Role: "assistant", Content: "a1"},
		{Role: "assistant", Content: "a2"},
		{Role: "assistant", Content: "a3"},
	}
	cfg := &config.ContextPruningConfig{Mode: "cache-ttl"}
	got := pruneContextMessages(msgs, 10000, cfg, nil, "", nil)

	content := got[2].Content
	// Pass 0 suffix markers must be GONE after Phase 02 deletion.
	if strings.Contains(content, "Single tool result trimmed:") {
		t.Errorf("Pass 0 suffix should be removed; got: %q", testTruncate(content, 200))
	}
	if strings.Contains(content, "⚠️ [... middle content omitted ...]") {
		t.Errorf("Pass 0 middle-omitted marker should be removed; got: %q", testTruncate(content, 200))
	}
	// Must still be trimmed by Pass 1 (smaller than original).
	if estimateMessageChars(got[2]) >= 15000 {
		t.Errorf("Pass 1 should still trim the large result via ratio gate; got %d chars", estimateMessageChars(got[2]))
	}
}

// ─── PruneStats (Phase 05) ────────────────────────────────────────────────

func TestPruneContextMessages_Stats_TrimmedPopulated(t *testing.T) {
	// Large tool result that will be soft-trimmed.
	msgs := []providers.Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{{ID: "c1", Name: "read_file"}}},
		{Role: "tool", ToolCallID: "c1", Content: strings.Repeat("x", 15000)},
		{Role: "assistant", Content: "a1"},
		{Role: "assistant", Content: "a2"},
		{Role: "assistant", Content: "a3"},
	}
	cfg := &config.ContextPruningConfig{Mode: "cache-ttl"}
	var stats pipeline.PruneStats
	pruneContextMessages(msgs, 5000, cfg, nil, "", &stats)
	if stats.ResultsTrimmed == 0 {
		t.Error("expected at least one ResultsTrimmed, got 0")
	}
}

func TestPruneContextMessages_Stats_NilSafe(t *testing.T) {
	// Passing nil stats must not panic.
	msgs := []providers.Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{{ID: "c1", Name: "read_file"}}},
		{Role: "tool", ToolCallID: "c1", Content: strings.Repeat("x", 15000)},
		{Role: "assistant", Content: "a1"},
		{Role: "assistant", Content: "a2"},
		{Role: "assistant", Content: "a3"},
	}
	cfg := &config.ContextPruningConfig{Mode: "cache-ttl"}
	_ = pruneContextMessages(msgs, 5000, cfg, nil, "", nil) // must not panic
}

// ─── parseTTL (Phase 06) ─────────────────────────────────────────────────

func testTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
