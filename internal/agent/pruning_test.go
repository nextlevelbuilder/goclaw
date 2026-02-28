package agent

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// --- resolvePruningSettings ---

func TestResolvePruningSettings_NilConfig(t *testing.T) {
	s := resolvePruningSettings(nil)
	if s.keepLastAssistants != defaultKeepLastAssistants {
		t.Errorf("keepLastAssistants: want %d, got %d", defaultKeepLastAssistants, s.keepLastAssistants)
	}
	if s.softTrimRatio != defaultSoftTrimRatio {
		t.Errorf("softTrimRatio: want %f, got %f", defaultSoftTrimRatio, s.softTrimRatio)
	}
	if s.hardClearRatio != defaultHardClearRatio {
		t.Errorf("hardClearRatio: want %f, got %f", defaultHardClearRatio, s.hardClearRatio)
	}
	if !s.hardClearEnabled {
		t.Error("hardClearEnabled should default to true")
	}
	if s.hardClearPlaceholder != defaultHardClearPlaceholder {
		t.Errorf("placeholder: want %q, got %q", defaultHardClearPlaceholder, s.hardClearPlaceholder)
	}
}

func TestResolvePruningSettings_PartialOverrides(t *testing.T) {
	cfg := &config.ContextPruningConfig{
		KeepLastAssistants: 5,
		SoftTrimRatio:      0.4,
	}
	s := resolvePruningSettings(cfg)
	if s.keepLastAssistants != 5 {
		t.Errorf("keepLastAssistants: want 5, got %d", s.keepLastAssistants)
	}
	if s.softTrimRatio != 0.4 {
		t.Errorf("softTrimRatio: want 0.4, got %f", s.softTrimRatio)
	}
	// Non-overridden fields should use defaults
	if s.hardClearRatio != defaultHardClearRatio {
		t.Errorf("hardClearRatio should still be default, got %f", s.hardClearRatio)
	}
}

func TestResolvePruningSettings_SoftTrimOverrides(t *testing.T) {
	cfg := &config.ContextPruningConfig{
		SoftTrim: &config.ContextPruningSoftTrim{
			MaxChars:  8000,
			HeadChars: 2000,
			TailChars: 3000,
		},
	}
	s := resolvePruningSettings(cfg)
	if s.softTrimMaxChars != 8000 {
		t.Errorf("softTrimMaxChars: want 8000, got %d", s.softTrimMaxChars)
	}
	if s.softTrimHeadChars != 2000 {
		t.Errorf("softTrimHeadChars: want 2000, got %d", s.softTrimHeadChars)
	}
	if s.softTrimTailChars != 3000 {
		t.Errorf("softTrimTailChars: want 3000, got %d", s.softTrimTailChars)
	}
}

func TestResolvePruningSettings_HardClearDisabled(t *testing.T) {
	disabled := false
	cfg := &config.ContextPruningConfig{
		HardClear: &config.ContextPruningHardClear{
			Enabled:     &disabled,
			Placeholder: "[GONE]",
		},
	}
	s := resolvePruningSettings(cfg)
	if s.hardClearEnabled {
		t.Error("hardClearEnabled should be false")
	}
	if s.hardClearPlaceholder != "[GONE]" {
		t.Errorf("placeholder: want [GONE], got %q", s.hardClearPlaceholder)
	}
}

func TestResolvePruningSettings_InvalidRatiosIgnored(t *testing.T) {
	cfg := &config.ContextPruningConfig{
		SoftTrimRatio: 1.5, // > 1, should be ignored
		HardClearRatio: -0.1, // < 0, should be ignored
	}
	s := resolvePruningSettings(cfg)
	if s.softTrimRatio != defaultSoftTrimRatio {
		t.Errorf("invalid softTrimRatio should be ignored, got %f", s.softTrimRatio)
	}
	if s.hardClearRatio != defaultHardClearRatio {
		t.Errorf("invalid hardClearRatio should be ignored, got %f", s.hardClearRatio)
	}
}

// --- findAssistantCutoff ---

func TestFindAssistantCutoff_Basic(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "q1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q2"},
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "q3"},
		{Role: "assistant", Content: "a3"},
	}
	// Keep last 2 → cutoff at the 2nd-from-last assistant (a2, index 3)
	got := findAssistantCutoff(msgs, 2)
	if got != 3 {
		t.Errorf("expected cutoff at index 3, got %d", got)
	}
}

func TestFindAssistantCutoff_NotEnoughAssistants(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "q1"},
		{Role: "assistant", Content: "a1"},
	}
	got := findAssistantCutoff(msgs, 3) // want 3, only have 1
	if got != -1 {
		t.Errorf("expected -1 (not enough assistants), got %d", got)
	}
}

func TestFindAssistantCutoff_KeepLastZero(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "q1"},
		{Role: "assistant", Content: "a1"},
	}
	got := findAssistantCutoff(msgs, 0)
	if got != len(msgs) {
		t.Errorf("expected len(msgs) when keepLast=0, got %d", got)
	}
}

// --- takeHead / takeTail ---

func TestTakeHead_Basic(t *testing.T) {
	got := takeHead("Hello World", 5)
	if got != "Hello" {
		t.Errorf("expected 'Hello', got %q", got)
	}
}

func TestTakeHead_ShortString(t *testing.T) {
	got := takeHead("Hi", 10)
	if got != "Hi" {
		t.Errorf("expected 'Hi', got %q", got)
	}
}

func TestTakeHead_Zero(t *testing.T) {
	got := takeHead("Hello", 0)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestTakeHead_Unicode(t *testing.T) {
	got := takeHead("héllo wörld", 5)
	if got != "héllo" {
		t.Errorf("expected 'héllo', got %q", got)
	}
}

func TestTakeTail_Basic(t *testing.T) {
	got := takeTail("Hello World", 5)
	if got != "World" {
		t.Errorf("expected 'World', got %q", got)
	}
}

func TestTakeTail_ShortString(t *testing.T) {
	got := takeTail("Hi", 10)
	if got != "Hi" {
		t.Errorf("expected 'Hi', got %q", got)
	}
}

func TestTakeTail_Zero(t *testing.T) {
	got := takeTail("Hello", 0)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestTakeTail_Unicode(t *testing.T) {
	got := takeTail("héllo wörld", 5)
	if got != "wörld" {
		t.Errorf("expected 'wörld', got %q", got)
	}
}

// --- pruneContextMessages ---

func TestPruneContextMessages_NilConfig(t *testing.T) {
	msgs := []providers.Message{{Role: "user", Content: "hello"}}
	got := pruneContextMessages(msgs, 200000, nil)
	if len(got) != 1 {
		t.Errorf("expected 1 message, got %d", len(got))
	}
}

func TestPruneContextMessages_WrongMode(t *testing.T) {
	cfg := &config.ContextPruningConfig{Mode: "off"}
	msgs := []providers.Message{{Role: "user", Content: "hello"}}
	got := pruneContextMessages(msgs, 200000, cfg)
	if len(got) != 1 {
		t.Errorf("expected passthrough for mode=off, got %d messages", len(got))
	}
}

func TestPruneContextMessages_BelowSoftRatio(t *testing.T) {
	cfg := &config.ContextPruningConfig{Mode: "cache-ttl"}
	// Small messages with a large context window → ratio < 0.3 → no pruning
	msgs := []providers.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
		{Role: "user", Content: "bye"},
		{Role: "assistant", Content: "goodbye"},
	}
	got := pruneContextMessages(msgs, 200000, cfg)
	if len(got) != 4 {
		t.Errorf("expected no pruning (below ratio), got %d messages", len(got))
	}
}

func TestPruneContextMessages_SoftTrimLongToolResult(t *testing.T) {
	cfg := &config.ContextPruningConfig{Mode: "cache-ttl"}

	// Create a tool result larger than softTrimMaxChars (4000)
	longContent := strings.Repeat("x", 10000)
	msgs := []providers.Message{
		{Role: "user", Content: "read the file"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{{ID: "tc1", Name: "read_file"}}},
		{Role: "tool", Content: longContent, ToolCallID: "tc1"},
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "another question"},
		{Role: "assistant", Content: "answer"},
		{Role: "user", Content: "one more"},
		{Role: "assistant", Content: "final"},
	}

	// Use a small context window so ratio > 0.3
	// Total chars ~10100, contextWindow tokens=10000, charWindow=40000, ratio=10100/40000=0.25
	// Need smaller window: tokens=8000, charWindow=32000, ratio=10100/32000=0.31 → triggers soft trim
	got := pruneContextMessages(msgs, 8000, cfg)

	// The tool result at index 2 should be soft-trimmed
	if len(got) != len(msgs) {
		t.Fatalf("soft trim should not change message count, got %d", len(got))
	}
	toolMsg := got[2]
	if toolMsg.Role != "tool" {
		t.Fatalf("expected tool message at index 2, got %s", toolMsg.Role)
	}
	if len(toolMsg.Content) >= len(longContent) {
		t.Errorf("expected tool content to be trimmed (was %d chars), got %d", len(longContent), len(toolMsg.Content))
	}
	if !strings.Contains(toolMsg.Content, "Tool result trimmed") {
		t.Error("expected trimmed marker in content")
	}
	// ToolCallID should be preserved
	if toolMsg.ToolCallID != "tc1" {
		t.Errorf("ToolCallID should be preserved, got %q", toolMsg.ToolCallID)
	}
}

func TestPruneContextMessages_ProtectedZone(t *testing.T) {
	cfg := &config.ContextPruningConfig{Mode: "cache-ttl"}

	longContent := strings.Repeat("y", 10000)
	// The tool result is in the last 3 assistants' zone → should NOT be pruned
	msgs := []providers.Message{
		{Role: "user", Content: "query"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{{ID: "tc1", Name: "read_file"}}},
		{Role: "tool", Content: longContent, ToolCallID: "tc1"},
		{Role: "assistant", Content: "result"},
		// Only 2 assistant messages total — cutoff would be -1 (not enough for keepLast=3)
	}
	got := pruneContextMessages(msgs, 5000, cfg)

	// Should be unchanged because there aren't enough assistants for the cutoff
	toolMsg := got[2]
	if toolMsg.Content != longContent {
		t.Error("tool result in protected zone should NOT be pruned")
	}
}

func TestPruneContextMessages_EmptyMessages(t *testing.T) {
	cfg := &config.ContextPruningConfig{Mode: "cache-ttl"}
	got := pruneContextMessages(nil, 200000, cfg)
	if got != nil {
		t.Errorf("expected nil for empty msgs, got %v", got)
	}
}

func TestPruneContextMessages_ZeroContextWindow(t *testing.T) {
	cfg := &config.ContextPruningConfig{Mode: "cache-ttl"}
	msgs := []providers.Message{{Role: "user", Content: "test"}}
	got := pruneContextMessages(msgs, 0, cfg)
	if len(got) != 1 {
		t.Errorf("expected passthrough for zero context window, got %d", len(got))
	}
}

func TestPruneContextMessages_HardClear(t *testing.T) {
	cfg := &config.ContextPruningConfig{
		Mode:                 "cache-ttl",
		KeepLastAssistants:   1, // Only protect last 1 assistant → more prunable zone
		MinPrunableToolChars: 100,
	}

	// Long tool results in the prunable zone (before the last assistant)
	longContent := strings.Repeat("z", 20000)
	msgs := []providers.Message{
		{Role: "user", Content: "read many files"},
		// These tool results are in the prunable zone (before last assistant)
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{{ID: "tc1", Name: "read_file"}}},
		{Role: "tool", Content: longContent, ToolCallID: "tc1"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{{ID: "tc2", Name: "read_file"}}},
		{Role: "tool", Content: longContent, ToolCallID: "tc2"},
		{Role: "assistant", Content: "analysis"},
		{Role: "user", Content: "ok"},
		// This is the last assistant (protected by keepLastAssistants=1)
		{Role: "assistant", Content: "final answer"},
	}

	// tokens=2000, charWindow=8000
	// total chars ~40000+, ratio ~5.0 → way above both 0.3 and 0.5
	got := pruneContextMessages(msgs, 2000, cfg)

	// Check that at least one tool result was hard-cleared
	hardCleared := false
	for _, m := range got {
		if m.Role == "tool" && m.Content == defaultHardClearPlaceholder {
			hardCleared = true
			break
		}
	}
	if !hardCleared {
		t.Error("expected at least one tool result to be hard-cleared")
	}
}
