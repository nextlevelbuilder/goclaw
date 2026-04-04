package telegram

import (
	"strings"
	"testing"
)

func TestDisplayWidth(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"hello", 5},
		{"Khởi động", 9},        // Vietnamese diacritics = single-width
		{"Hardware tối thiểu", 18}, // Vietnamese diacritics = single-width
		{"Ngôn ngữ", 8},
		{"đ", 1},                 // Vietnamese d-stroke = single-width
		{"中文", 4},               // CJK = double-width
		{"日本語", 6},              // CJK = double-width
	}

	for _, tt := range tests {
		got := displayWidth(tt.input)
		if got != tt.want {
			t.Errorf("displayWidth(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestRenderTableAsCode_Vietnamese(t *testing.T) {
	lines := []string{
		"| Metric | OpenClaw | ZeroClaw |",
		"|--------|----------|----------|",
		"| Ngôn ngữ | TypeScript/Node.js | Rust |",
		"| Khởi động | > 500s | < 10ms |",
		"| Hardware tối thiểu | Mac mini $599 | $10 (bao gồm cả Raspberry Pi) |",
	}

	result := renderTableAsCode(lines)

	// Every non-separator line should have the same number of pipes
	resultLines := strings.Split(result, "\n")
	if len(resultLines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(resultLines))
	}

	// Check separator line width matches header line width
	headerWidth := displayWidth(resultLines[0])
	sepWidth := displayWidth(resultLines[1])
	if headerWidth != sepWidth {
		t.Errorf("header width (%d) != separator width (%d)\nheader: %s\nsep:    %s",
			headerWidth, sepWidth, resultLines[0], resultLines[1])
	}

	// Check all data rows match header width
	for i := 2; i < len(resultLines); i++ {
		rowWidth := displayWidth(resultLines[i])
		if rowWidth != headerWidth {
			t.Errorf("row %d width (%d) != header width (%d)\nrow:    %s\nheader: %s",
				i, rowWidth, headerWidth, resultLines[i], resultLines[0])
		}
	}
}

func TestMarkdownToTelegramHTML_Mentions(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string // substring that MUST appear
		deny  string // substring that must NOT appear
	}{
		{
			name:  "mention stays plain text",
			input: "Hello @viettran how are you?",
			want:  "@viettran",
			deny:  `href="https://t.me/`,
		},
		{
			name:  "mention not wrapped in link tag",
			input: "cc @john please review",
			want:  "@john",
			deny:  "<a ",
		},
		{
			name:  "email not treated as mention",
			input: "send to user@domain.com",
			want:  "user@domain.com",
			deny:  `href=`,
		},
		{
			name:  "mention survives italic conversion",
			input: "_italic_ and @bot_name end",
			want:  "@bot_name",
			deny:  `<a `,
		},
		{
			name:  "multiple mentions plain text",
			input: "@alice and @bob discussed",
			want:  "@alice",
			deny:  "t.me",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := markdownToTelegramHTML(tt.input)
			if !strings.Contains(got, tt.want) {
				t.Errorf("expected %q in output, got: %s", tt.want, got)
			}
			if tt.deny != "" && strings.Contains(got, tt.deny) {
				t.Errorf("unexpected %q in output, got: %s", tt.deny, got)
			}
		})
	}
}

func TestChunkHTML(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   []string
	}{
		{
			name:   "natural boundary",
			input:  "hello world",
			maxLen: 6,
			want:   []string{"hello", "world"},
		},
		{
			name:   "tag exceeds maxLen uses fallback",
			input:  "hello <a href='url'>link</a> world",
			maxLen: 12,
			want:   []string{"hello", "<a href='url", "'>link</a>", "world"},
		},
		{
			name:   "avoid mid-tag split safe",
			input:  "hello <a href='url'>link</a> world",
			maxLen: 25,
			// "hello " (6) -> remaining "<a href='url'>link</a> world" (28)
			// maxLen 25. remaining[:25] is "<a href='url'>link</a> wo"
			// lastOpen is at "</a" (18). lastClose at "</a>" (21).
			// lastOpen < lastClose (18 < 21). No cut change from safety.
			// lastSpace is at index 22 (" world"). cutAt=23.
			want: []string{"hello", "<a href='url'>link</a>", "world"},
		},
		{
			name:   "avoid mid-entity split",
			input:  "hello &amp; world",
			maxLen: 9,
			// "hello " (6) -> then "&amp; world"
			// At second chunk: remaining="&amp; world", maxLen=9
			// remaining[:9] is "&amp; wor"
			// lastSpace is at index 5. cutAt=6.
			want: []string{"hello", "&amp;", "world"},
		},
		{
			name:   "monolithic fallback",
			input:  "monolithicblock",
			maxLen: 5,
			want:   []string{"monol", "ithic", "block"},
		},
		{
			name:   "paragraph preferred",
			input:  "para1\n\npara2\nline3",
			maxLen: 10,
			want:   []string{"para1", "para2", "line3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := chunkHTML(tt.input, tt.maxLen)
			if len(got) != len(tt.want) {
				t.Fatalf("chunkHTML() returned %d chunks, want %d\ngot:  %q\nwant: %q", len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("chunk[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
