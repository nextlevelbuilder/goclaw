package memory

import (
	"strings"
	"testing"
)

func TestBuildRecallQuery_NoContextReturnsMessageUnchanged(t *testing.T) {
	got := buildRecallQuery("what's my favorite?", "")
	if got != "what's my favorite?" {
		t.Errorf("empty context should return message unchanged, got %q", got)
	}
}

func TestBuildRecallQuery_EmptyMessageReturnsEmpty(t *testing.T) {
	got := buildRecallQuery("", "some recent context")
	if got != "" {
		t.Errorf("empty message should return empty, got %q", got)
	}
}

func TestBuildRecallQuery_CombinesContextAndMessage(t *testing.T) {
	ctx := "We were talking about coffee shops in downtown."
	msg := "what's my favorite?"
	got := buildRecallQuery(msg, ctx)

	if !strings.Contains(got, msg) {
		t.Errorf("result must contain user message, got %q", got)
	}
	if !strings.Contains(got, ctx) {
		t.Errorf("result must contain recent context, got %q", got)
	}
	// Query label must come AFTER context so position-biased embeddings give
	// the message the strongest signal.
	ctxIdx := strings.Index(got, "Context:")
	queryIdx := strings.Index(got, "Query:")
	if ctxIdx == -1 || queryIdx == -1 || ctxIdx >= queryIdx {
		t.Errorf("Context must precede Query label, got %q", got)
	}
}

func TestBuildRecallQuery_TruncatesOversizedContext(t *testing.T) {
	// 1000-char context, maxRecallContextChars = 400
	ctx := strings.Repeat("a", 1000)
	msg := "test"
	got := buildRecallQuery(msg, ctx)

	// Stripped context section should be at most maxRecallContextRunes long.
	// Full output has prefix overhead ("Context: " + "\nQuery: " + msg) ~ 20 chars.
	// Total should be < 500 chars (bounded by truncation + prefix + msg).
	if len(got) > maxRecallContextRunes+64 {
		t.Errorf("oversized context not truncated: got %d chars, want ≤ %d",
			len(got), maxRecallContextRunes+64)
	}
}

func TestBuildRecallQuery_TailClipsLongContext(t *testing.T) {
	// Tail-clip keeps the most recent portion (last N chars) since it's
	// closer to the current turn in conversation time.
	ctx := strings.Repeat("OLD ", 200) + strings.Repeat("NEW ", 20) // 820 chars, "NEW" at the end
	got := buildRecallQuery("follow-up", ctx)
	if !strings.Contains(got, "NEW") {
		t.Errorf("tail-clip should preserve recent ('NEW') portion, got %q", got)
	}
}

func TestBuildRecallQuery_TrimsWhitespace(t *testing.T) {
	got := buildRecallQuery("  query  ", "  ctx  ")
	if !strings.Contains(got, "Context: ctx") {
		t.Errorf("context should be trimmed, got %q", got)
	}
	if !strings.Contains(got, "Query: query") {
		t.Errorf("message should be trimmed, got %q", got)
	}
}

// TestBuildRecallQuery_UnicodeSafeTailClip verifies rune-safe truncation for
// Vietnamese / Chinese input. Byte-wise slicing would produce invalid UTF-8 at
// multi-byte rune boundaries — embedding models on that garbage return lower
// quality matches, defeating Phase 9's whole point.
func TestBuildRecallQuery_UnicodeSafeTailClip(t *testing.T) {
	// 500 Vietnamese tiếng Việt runes (each char = 2-3 bytes)
	viChar := "ế" // 3 bytes in UTF-8
	ctx := strings.Repeat(viChar, 500)
	msg := "câu hỏi"
	got := buildRecallQuery(msg, ctx)
	// The result must contain valid UTF-8 — no half-runes at the clip boundary.
	// If the clip cut a rune, the resulting string would contain 0xEF 0xBF 0xBD
	// replacement chars or fail strings.ContainsRune checks.
	if !strings.Contains(got, msg) {
		t.Errorf("message missing from result, got %q", got)
	}
	// Must still contain the Vietnamese marker char
	if !strings.Contains(got, viChar) {
		t.Errorf("vietnamese context missing, got %q", got)
	}
	// Every rune must be valid (conversion round-trip)
	for _, r := range got {
		if r == '\uFFFD' {
			t.Errorf("result contains replacement rune (invalid UTF-8 clip boundary), got %q", got)
			break
		}
	}
}

func TestTailClipRunes(t *testing.T) {
	if got := tailClipRunes("hello", 10); got != "hello" {
		t.Errorf("short input should be unchanged, got %q", got)
	}
	if got := tailClipRunes("abcdef", 3); got != "def" {
		t.Errorf("expected 'def', got %q", got)
	}
	// Chinese: 10 Han chars, clip to 3
	if got := tailClipRunes("一二三四五六七八九十", 3); got != "八九十" {
		t.Errorf("expected '八九十', got %q", got)
	}
	if got := tailClipRunes("anything", 0); got != "" {
		t.Errorf("zero maxRunes should return empty, got %q", got)
	}
}
