package tools

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// Regression test for issue #1130: subagent span previews must never
// collapse to the bare 3-char "..." — not even at limits far below the
// multi-byte marker length, where the legacy label helper did exactly that
// (its rune-boundary backoff could drive the effective limit to 0 for
// non-empty input). truncateSpanPreview delegates to tracing.TruncateMid,
// the same helper the normal agent-loop tracing path uses, so the two
// paths stay behaviourally consistent.
func TestTruncateSpanPreviewNeverCollapsesToBareEllipsis(t *testing.T) {
	// ~1280 bytes of multi-byte content, mirroring a normal LLM reply.
	long := strings.Repeat("你好世界", 160)
	for _, limit := range []int{500, 200, 100, 60, 50, 4, 3, 2, 1} {
		got := truncateSpanPreview(long, limit)
		if got == "..." {
			t.Errorf("truncateSpanPreview(len=%d, limit=%d) collapsed to bare \"...\"", len(long), limit)
		}
	}
}

// truncateSpanPreview must produce the exact same result as the normal
// agent-loop tracing path for identical inputs.
func TestTruncateSpanPreviewMatchesAgentLoopTruncation(t *testing.T) {
	cases := []struct {
		name  string
		input string
		limit int
	}{
		{"ascii", strings.Repeat("a", 1000), 500},
		{"multibyte", strings.Repeat("你好世界", 160), 500},
		{"error-limit", strings.Repeat("x", 400), 200},
		{"fits", "short reply", 500},
		{"invalid-utf8", "ok\xff\xfebad" + strings.Repeat("z", 600), 500},
	}
	for _, tc := range cases {
		if got, want := truncateSpanPreview(tc.input, tc.limit), tracing.TruncateMid(tc.input, tc.limit); got != want {
			t.Errorf("%s: truncateSpanPreview != TruncateMid", tc.name)
		}
	}
}

// Previews must stay valid UTF-8 at the limits actually used by span
// emitters (500 / 100000 preview, 200 error), and must fit the limit.
func TestTruncateSpanPreviewValidUTF8AndLength(t *testing.T) {
	long := strings.Repeat("你好，世界。", 200) // 2400 bytes
	for _, limit := range []int{500, 200} {
		got := truncateSpanPreview(long, limit)
		if !utf8.ValidString(got) {
			t.Errorf("limit=%d: preview is not valid UTF-8", limit)
		}
		if len(got) > limit {
			t.Errorf("limit=%d: preview is %d bytes, exceeds limit", limit, len(got))
		}
		if !strings.Contains(got, "[truncated ") {
			t.Errorf("limit=%d: preview missing truncation marker: %q", limit, got[:min(60, len(got))])
		}
	}
	// Verbose preview limit exceeds the input entirely: passthrough.
	if got := truncateSpanPreview(long, 100000); got != long {
		t.Errorf("limit=100000: expected passthrough, got %d bytes", len(got))
	}
}

// Short inputs pass through unchanged.
func TestTruncateSpanPreviewPassthrough(t *testing.T) {
	in := "all done, nothing truncated"
	if got := truncateSpanPreview(in, 500); got != in {
		t.Errorf("passthrough mismatch: got %q", got)
	}
}
