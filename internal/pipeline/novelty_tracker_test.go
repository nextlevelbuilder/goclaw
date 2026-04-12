package pipeline

import "testing"

func TestNovelty_ExactRepeat(t *testing.T) {
	tracker := NewNoveltyTracker()
	args := map[string]any{"url": "https://example.com"}

	if tracker.CheckExactRepeat("web_fetch", args) {
		t.Fatal("did not expect repeat before first record")
	}
	tracker.Record("web_fetch", args, "page 1", false)
	if !tracker.CheckExactRepeat("web_fetch", args) {
		t.Fatal("expected exact repeat after first record")
	}
}

func TestNovelty_ContentSimilarity(t *testing.T) {
	tracker := NewNoveltyTracker()
	args := map[string]any{"url": "https://example.com"}

	tracker.Record("web_fetch", args, "same result", false)
	entry := tracker.Record("web_fetch", args, "same result", false)
	if entry.ConsecutiveSame != 2 {
		t.Fatalf("ConsecutiveSame = %d, want 2", entry.ConsecutiveSame)
	}
}

func TestNovelty_DiminishingReturns(t *testing.T) {
	tracker := NewNoveltyTracker()
	args := map[string]any{"path": "README.md"}

	tracker.Record("read_file", args, "1234567890", false)
	tracker.Record("read_file", args, "123456", false)
	entry := tracker.Record("read_file", args, "123", false)
	if entry.CallCount != 3 {
		t.Fatalf("CallCount = %d, want 3", entry.CallCount)
	}
	if entry.ShrinkingCount != 2 {
		t.Fatalf("ShrinkingCount = %d, want 2", entry.ShrinkingCount)
	}
}
