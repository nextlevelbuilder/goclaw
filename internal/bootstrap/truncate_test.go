package bootstrap

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTrimContentPreservesUTF8(t *testing.T) {
	content := strings.Repeat("界", 100)
	got := trimContent(content, "SOUL.md", 100)
	if !utf8.ValidString(got) {
		t.Fatalf("trimContent split a UTF-8 rune: %q", got)
	}
	if !strings.HasPrefix(got, "界") || !strings.HasSuffix(got, "界") {
		t.Fatalf("expected both head and tail: %q", got)
	}
}

func TestClampToBudgetPreservesUTF8AndByteLimit(t *testing.T) {
	for _, content := range []string{strings.Repeat("界", 20), strings.Repeat("a", 20)} {
		for budget := 1; budget <= 12; budget++ {
			got := clampToBudget(content, budget)
			if !utf8.ValidString(got) {
				t.Errorf("budget %d split a UTF-8 rune: %q", budget, got)
			}
			if len(got) > budget {
				t.Errorf("budget %d exceeded: got %d bytes", budget, len(got))
			}
		}
	}
}

func TestBuildContextFilesPreservesUTF8WithinTotalBudget(t *testing.T) {
	files := []File{{Name: "SOUL.md", Content: strings.Repeat("界", 100)}}
	got := BuildContextFiles(files, TruncateConfig{MaxCharsPerFile: 100, TotalMaxChars: 128})
	if len(got) != 1 {
		t.Fatalf("got %d files, want 1", len(got))
	}
	if !utf8.ValidString(got[0].Content) || len(got[0].Content) > 128 {
		t.Fatalf("invalid or oversized context: %q", got[0].Content)
	}
}
