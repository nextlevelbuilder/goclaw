package tokencount

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestCount_CL100K(t *testing.T) {
	c := NewTiktokenCounter()
	// "Hello, world!" is 4 tokens in cl100k_base
	count := c.Count("claude-sonnet-4-5-20250929", "Hello, world!")
	if count < 3 || count > 6 {
		t.Errorf("cl100k count = %d, expected ~4", count)
	}
}

func TestCount_O200K(t *testing.T) {
	c := NewTiktokenCounter()
	count := c.Count("gpt-4o-mini", "Hello, world!")
	if count < 3 || count > 6 {
		t.Errorf("o200k count = %d, expected ~4", count)
	}
}

func TestCount_UnknownModel(t *testing.T) {
	c := NewTiktokenCounter()
	// Unknown model falls back to rune/3
	count := c.Count("unknown-model-xyz", "Hello, world!")
	expected := NewFallbackCounter().Count("unknown-model-xyz", "Hello, world!")
	if count != expected {
		t.Errorf("fallback count = %d, want %d", count, expected)
	}
}

func TestCountMessages_Cache(t *testing.T) {
	c := NewTiktokenCounter()
	msgs := []providers.Message{
		{Role: "user", Content: "What is 2+2?"},
		{Role: "assistant", Content: "The answer is 4."},
	}

	first := c.CountMessages("claude-sonnet-4-5-20250929", msgs)
	second := c.CountMessages("claude-sonnet-4-5-20250929", msgs)

	if first != second {
		t.Errorf("cached count %d != first count %d", second, first)
	}
	if first <= 0 {
		t.Errorf("count should be positive, got %d", first)
	}

	// Verify cache is populated
	c.mu.RLock()
	cacheLen := len(c.msgCache)
	c.mu.RUnlock()
	if cacheLen != 2 {
		t.Errorf("cache has %d entries, want 2", cacheLen)
	}
}

func TestCountMessages_Overhead(t *testing.T) {
	c := NewTiktokenCounter()
	msgs := []providers.Message{
		{Role: "user", Content: "Hi"},
	}
	count := c.CountMessages("claude-sonnet-4-5-20250929", msgs)

	// Should be > raw token count due to PerMessageOverhead
	rawCount := c.Count("claude-sonnet-4-5-20250929", "Hi")
	if count <= rawCount {
		t.Errorf("messages count %d should exceed raw count %d (overhead)", count, rawCount)
	}
}

func TestModelContextWindow(t *testing.T) {
	c := NewTiktokenCounter()

	tests := []struct {
		model string
		want  int
	}{
		{"claude-sonnet-4-5-20250929", 200_000},
		{"gpt-4o-mini", 128_000},
		{"gpt-5.4", 1_000_000},
		{"unknown-model", 200_000}, // conservative default
	}
	for _, tt := range tests {
		got := c.ModelContextWindow(tt.model)
		if got != tt.want {
			t.Errorf("ModelContextWindow(%q) = %d, want %d", tt.model, got, tt.want)
		}
	}
}

func TestResetCache(t *testing.T) {
	c := NewTiktokenCounter()
	msgs := []providers.Message{{Role: "user", Content: "test"}}

	c.CountMessages("claude-sonnet-4-5-20250929", msgs)

	c.mu.RLock()
	before := len(c.msgCache)
	c.mu.RUnlock()
	if before == 0 {
		t.Fatal("cache should have entries before reset")
	}

	c.ResetCache()

	c.mu.RLock()
	after := len(c.msgCache)
	c.mu.RUnlock()
	if after != 0 {
		t.Errorf("cache has %d entries after reset, want 0", after)
	}
}

func TestNewTokenCounter_Factory(t *testing.T) {
	fallback := NewTokenCounter(false)
	if _, ok := fallback.(*FallbackCounter); !ok {
		t.Errorf("NewTokenCounter(false) = %T, want *FallbackCounter", fallback)
	}

	tk := NewTokenCounter(true)
	if _, ok := tk.(*tiktokenCounter); !ok {
		t.Errorf("NewTokenCounter(true) = %T, want *tiktokenCounter", tk)
	}
}
