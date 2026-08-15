package agent

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeFilename(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// Latin / Vietnamese
		{"vietnamese_basic", "Báo cáo Q4.pdf", "bao-cao-q4"},
		{"vietnamese_dong_lower", "đường.doc", "duong"},
		{"vietnamese_dong_upper", "Đường.doc", "duong"},
		{"english_simple", "Quarterly Report.pdf", "quarterly-report"},
		{"english_numbers", "report-2025_v2.pdf", "report-2025-v2"},
		{"double_dot", "report.final.pdf", "report-final"},
		// Non-Latin preserved
		{"cjk_jp", "猫の写真.png", "猫の写真"},
		{"cjk_zh", "报告.pdf", "报告"},
		{"arabic", "تقرير.pdf", "تقرير"},
		// Degenerate / unsafe
		{"empty", "", ""},
		{"whitespace_only", "   ", ""},
		// path_traversal: filepath.Base strips dirs → "passwd" is safe slug (no /, no ..).
		{"path_traversal", "../../etc/passwd", "passwd"},
		{"dotfile", ".env", ""},
		{"dot_only", ".", ""},
		{"dotdot_only", "..", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeFilename(tc.in)
			if got != tc.want {
				t.Fatalf("sanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizeFilenameGuarantees(t *testing.T) {
	// Path-traversal-in-middle and unsafe runes must never survive in output.
	inputs := []string{
		"a/b/c.png",
		"a\\b\\c.png",
		"name:with:colons.txt",
		"quote\"mark.pdf",
		"pipe|file.md",
		"question?.md",
		"star*.md",
		"angle<>.md",
		"with\x00null.pdf",
		"猫/の写真.png",
	}
	unsafe := regexp.MustCompile(`[/\\:*?"<>|\x00-\x1f]`)
	for _, in := range inputs {
		out := sanitizeFilename(in)
		if unsafe.MatchString(out) {
			t.Errorf("sanitizeFilename(%q) = %q contains unsafe char", in, out)
		}
		if strings.Contains(out, "..") {
			t.Errorf("sanitizeFilename(%q) = %q contains ..", in, out)
		}
	}
}

func TestSanitizeFilenameLengthCap(t *testing.T) {
	// Latin: 200-char ASCII stem.
	long := strings.Repeat("a", 200) + ".pdf"
	out := sanitizeFilename(long)
	if n := utf8.RuneCountInString(out); n > maxFilenameRunes {
		t.Errorf("Latin cap: got %d runes, want <= %d", n, maxFilenameRunes)
	}
	// CJK: 100 CJK runes (multi-byte).
	cjkLong := strings.Repeat("猫", 100) + ".png"
	cjkOut := sanitizeFilename(cjkLong)
	if n := utf8.RuneCountInString(cjkOut); n > maxFilenameRunes {
		t.Errorf("CJK cap: got %d runes, want <= %d", n, maxFilenameRunes)
	}
}

func TestSanitizeFilenameNoLeadingTrailingDash(t *testing.T) {
	cases := []string{
		"  hello  .pdf",
		"---weird---.pdf",
		"$$$money$$$.pdf",
	}
	for _, in := range cases {
		out := sanitizeFilename(in)
		if out == "" {
			continue
		}
		if strings.HasPrefix(out, "-") || strings.HasSuffix(out, "-") {
			t.Errorf("sanitizeFilename(%q) = %q has leading/trailing dash", in, out)
		}
	}
}

func TestShortID(t *testing.T) {
	got := shortID(8)
	if len(got) != 8 {
		t.Fatalf("shortID(8) len = %d, want 8", len(got))
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}$`).MatchString(got) {
		t.Fatalf("shortID(8) = %q, not lowercase hex", got)
	}
	// Two calls should (almost always) differ — 32-bit entropy.
	if got == shortID(8) {
		// Retry once — if still equal, something is wrong.
		if got == shortID(8) {
			t.Fatalf("shortID(8) produced identical output twice in a row: %q", got)
		}
	}
}

func TestShortIDPanicsOnInvalidInput(t *testing.T) {
	for _, n := range []int{0, -2, 3, 7} {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("shortID(%d) did not panic", n)
				}
			}()
			_ = shortID(n)
		}()
	}
}
