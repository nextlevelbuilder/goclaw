package meow

import (
	"strings"
	"testing"
)

func TestBuildCaption_StackAndEscape(t *testing.T) {
	c := BuildCaption("안녕 <b>해커</b> & 친구", "Hi <script> & you")
	// HTML special chars escaped so they can't inject Telegram markup.
	if strings.Contains(c, "<b>") || strings.Contains(c, "<script>") {
		t.Fatalf("unescaped HTML leaked: %q", c)
	}
	if !strings.Contains(c, "&lt;b&gt;") || !strings.Contains(c, "&amp;") {
		t.Fatalf("expected escaped entities: %q", c)
	}
	// KO on top, divider, EN below.
	if !strings.Contains(c, CaptionDivider) {
		t.Fatalf("missing divider: %q", c)
	}
	if strings.Index(c, "안녕") > strings.Index(c, CaptionDivider) {
		t.Fatalf("KO block should precede divider: %q", c)
	}
}

func TestBuildCaption_SingleLang(t *testing.T) {
	if c := BuildCaption("", "EN only"); strings.Contains(c, CaptionDivider) {
		t.Errorf("no divider when one block empty: %q", c)
	}
	if c := BuildCaption("KO만", ""); strings.Contains(c, CaptionDivider) {
		t.Errorf("no divider when one block empty: %q", c)
	}
}

func TestSplitForPhoto(t *testing.T) {
	// Short caption: single chunk, no rest.
	first, rest := SplitForPhoto("short")
	if first != "short" || rest != nil {
		t.Fatalf("short caption should not split: %q %v", first, rest)
	}

	// Long caption: first ≤1024, all chunks within limits, content preserved.
	long := strings.Repeat("가나다라마바사아\n", 400) // ~3600 UTF-16 units
	first, rest = SplitForPhoto(long)
	if captionLen(first) > TelegramCaptionLimit {
		t.Fatalf("first chunk exceeds caption limit: %d", captionLen(first))
	}
	if len(rest) == 0 {
		t.Fatal("expected overflow chunks")
	}
	for i, r := range rest {
		if captionLen(r) > TelegramMessageLimit {
			t.Fatalf("rest[%d] exceeds message limit: %d", i, captionLen(r))
		}
	}
	// No content lost (ignoring the \n join boundaries).
	joined := strings.ReplaceAll(first+"\n"+strings.Join(rest, "\n"), "\n", "")
	if joined != strings.ReplaceAll(long, "\n", "") {
		t.Fatal("split lost or altered content")
	}
}

// A single oversized line full of escaped HTML entities must never be cut
// mid-entity (which would corrupt the HTML caption).
func TestSplitForPhoto_NoBrokenEntity(t *testing.T) {
	raw := strings.Repeat("a&b<c>d\"e ", 600) // one long line, ~6000 units
	caption := BuildCaption(raw, "")           // escapes & < > " into entities
	first, rest := SplitForPhoto(caption)

	endsMidEntity := func(s string) bool {
		amp := strings.LastIndex(s, "&")
		return amp > strings.LastIndex(s, ";") // dangling "&..." with no ";"
	}
	all := append([]string{first}, rest...)
	for i, chunk := range all {
		if endsMidEntity(chunk) {
			t.Fatalf("chunk[%d] ends mid-entity: ...%q", i, tail(chunk, 12))
		}
	}
	if strings.Join(all, "") != caption {
		t.Fatal("entity-aware split altered content")
	}
}

func tail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}
