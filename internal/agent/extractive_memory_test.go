package agent

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// ─── ExtractiveMemoryFallback ─────────────────────────────────────────────

func TestExtractiveMemoryFallback_EmptyHistory(t *testing.T) {
	result := ExtractiveMemoryFallback(nil)
	if result != "" {
		t.Errorf("empty history should return empty, got %q", result)
	}
}

func TestExtractiveMemoryFallback_OnlySystemAndToolMessages(t *testing.T) {
	msgs := []providers.Message{
		{Role: "system", Content: "system prompt"},
		{Role: "tool", Content: "tool result"},
	}
	result := ExtractiveMemoryFallback(msgs)
	// No user/assistant content → nothing to extract
	if result != "" {
		t.Errorf("only system/tool roles should return empty, got %q", result)
	}
}

func TestExtractiveMemoryFallback_ExtractsDecision(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "what should we use?"},
		{Role: "assistant", Content: "We decided to use PostgreSQL for the main database"},
	}
	result := ExtractiveMemoryFallback(msgs)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(result, "Decisions") {
		t.Error("expected Decisions section")
	}
	if !strings.Contains(result, "PostgreSQL") {
		t.Error("expected PostgreSQL in decisions")
	}
}

func TestExtractiveMemoryFallback_ExtractsURL(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "use this endpoint: https://api.example.com/v1/data for all requests"},
		{Role: "assistant", Content: "Got it"},
	}
	result := ExtractiveMemoryFallback(msgs)
	if !strings.Contains(result, "https://api.example.com/v1/data") {
		t.Error("expected URL in key facts")
	}
}

func TestExtractiveMemoryFallback_ExtractsDate(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "the deadline is 2025-12-31"},
		{Role: "assistant", Content: "noted"},
	}
	result := ExtractiveMemoryFallback(msgs)
	if !strings.Contains(result, "2025-12-31") {
		t.Error("expected date in key facts")
	}
}

func TestExtractiveMemoryFallback_ExtractsUserPreference(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "I prefer using tabs over spaces always"},
		{Role: "assistant", Content: "understood"},
	}
	result := ExtractiveMemoryFallback(msgs)
	if !strings.Contains(result, "Preferences") {
		t.Error("expected User Preferences section")
	}
}

func TestExtractiveMemoryFallback_HeaderPresent(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "decided to use Go for backend"},
		{Role: "assistant", Content: "sounds good"},
	}
	result := ExtractiveMemoryFallback(msgs)
	if !strings.Contains(result, "Extracted Context") {
		t.Error("expected header in result")
	}
}

func TestExtractiveMemoryFallback_NothingExtractable(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "hello there"},
		{Role: "assistant", Content: "hi"},
	}
	// Might return empty if no patterns match.
	// This is valid behavior — we just verify no panic.
	_ = ExtractiveMemoryFallback(msgs)
}

func TestExtractiveMemoryFallback_DeduplicatesMatches(t *testing.T) {
	// Same decision repeated twice → should appear once.
	msgs := []providers.Message{
		{Role: "user", Content: "decided to use Redis for caching"},
		{Role: "assistant", Content: "decided to use Redis for caching"},
	}
	result := ExtractiveMemoryFallback(msgs)
	// Count occurrences of "Redis"
	count := strings.Count(result, "decided to use Redis")
	if count > 1 {
		t.Errorf("expected deduplicated output, got %d occurrences", count)
	}
}

// ─── dedup ────────────────────────────────────────────────────────────────

func TestDedup_RemovesDuplicates(t *testing.T) {
	input := []string{"a", "b", "a", "c", "b"}
	got := dedup(input)
	if len(got) != 3 {
		t.Errorf("dedup = %v, want 3 unique items", got)
	}
}

func TestDedup_EmptyInput(t *testing.T) {
	got := dedup(nil)
	if got != nil {
		t.Errorf("dedup(nil) = %v, want nil", got)
	}
}

func TestDedup_SkipsBlankStrings(t *testing.T) {
	input := []string{"a", "   ", "", "b"}
	got := dedup(input)
	for _, s := range got {
		if s == "" || s == "   " {
			t.Errorf("dedup returned blank entry: %q", s)
		}
	}
}

func TestDedup_PreservesOrder(t *testing.T) {
	input := []string{"c", "a", "b", "a"}
	got := dedup(input)
	if len(got) != 3 || got[0] != "c" || got[1] != "a" || got[2] != "b" {
		t.Errorf("dedup = %v, want [c a b]", got)
	}
}

// ─── appendIfAbsent ───────────────────────────────────────────────────────

func TestAppendIfAbsent_AppendsWhenMissing(t *testing.T) {
	s := []string{"a", "b"}
	got := appendIfAbsent(s, "c")
	if len(got) != 3 || got[2] != "c" {
		t.Errorf("appendIfAbsent = %v, want [a b c]", got)
	}
}

func TestAppendIfAbsent_SkipsWhenPresent(t *testing.T) {
	s := []string{"a", "b", "c"}
	got := appendIfAbsent(s, "b")
	if len(got) != 3 {
		t.Errorf("appendIfAbsent should not append duplicate, got %v", got)
	}
}
