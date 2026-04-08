package providers

import (
	"testing"
)

// TestAnthropicSystemBlocksSplit verifies that a system prompt with
// the cache boundary marker is split into 2 blocks: stable (cached) + dynamic.
func TestAnthropicSystemBlocksSplit(t *testing.T) {
	prompt := "stable content\n" + CacheBoundaryMarker + "\ndynamic content"
	blocks := splitSystemPromptForCache(prompt)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0]["text"] != "stable content" {
		t.Errorf("block[0] text = %q, want %q", blocks[0]["text"], "stable content")
	}
	if blocks[0]["cache_control"] == nil {
		t.Error("block[0] missing cache_control")
	}
	if blocks[1]["text"] != "dynamic content" {
		t.Errorf("block[1] text = %q, want %q", blocks[1]["text"], "dynamic content")
	}
	if blocks[1]["cache_control"] != nil {
		t.Error("block[1] should NOT have cache_control")
	}
}

// TestAnthropicSingleBlockFallback verifies backward compat: no boundary
// marker → single block with cache_control.
func TestAnthropicSingleBlockFallback(t *testing.T) {
	prompt := "no boundary here"
	blocks := splitSystemPromptForCache(prompt)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0]["text"] != "no boundary here" {
		t.Errorf("block text = %q", blocks[0]["text"])
	}
	if blocks[0]["cache_control"] == nil {
		t.Error("single block missing cache_control")
	}
}

// TestAnthropicEmptyDynamic verifies that an empty dynamic section
// after the boundary produces only 1 block (no empty block appended).
func TestAnthropicEmptyDynamic(t *testing.T) {
	prompt := "stable only\n" + CacheBoundaryMarker + "\n"
	blocks := splitSystemPromptForCache(prompt)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block for empty dynamic, got %d", len(blocks))
	}
	if blocks[0]["text"] != "stable only" {
		t.Errorf("block text = %q", blocks[0]["text"])
	}
}

// TestAnthropicEmptyStable verifies that a boundary at the very start
// (empty stable section) still produces valid blocks without empty text.
func TestAnthropicEmptyStable(t *testing.T) {
	prompt := CacheBoundaryMarker + "\ndynamic only"
	blocks := splitSystemPromptForCache(prompt)
	// Stable is empty string after TrimSpace — should still produce a block
	// (Anthropic API handles empty text blocks gracefully).
	if len(blocks) < 1 {
		t.Fatal("expected at least 1 block")
	}
	// The dynamic block should have the actual content.
	last := blocks[len(blocks)-1]
	if last["text"] != "dynamic only" {
		t.Errorf("dynamic block text = %q, want %q", last["text"], "dynamic only")
	}
}
