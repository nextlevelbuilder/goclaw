package common

import "testing"

func TestStripMarkdown(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain text", "hello world", "hello world"},

		// Bold & italic
		{"bold stars", "this is **bold** text", "this is bold text"},
		{"bold underscores", "this is __bold__ text", "this is bold text"},
		{"bold+italic stars", "***important***", "important"},
		{"strikethrough", "this is ~~deleted~~ text", "this is deleted text"},

		// Code
		{"inline code", "use `fmt.Println` here", "use fmt.Println here"},
		{"fenced code block", "before\n```go\nfmt.Println(\"hi\")\n```\nafter", "before\nfmt.Println(\"hi\")\n\nafter"},
		{"fenced code block no lang", "```\ncode here\n```", "code here"},

		// Links & images
		{"link", "click [here](https://example.com) now", "click here (https://example.com) now"},
		{"image", "see ![alt](https://img.png) below", "see  below"},

		// Headers
		{"h1", "# Title", "Title"},
		{"h3", "### Section", "Section"},
		{"h6", "###### Deep", "Deep"},

		// Horizontal rules
		{"hr dashes", "above\n---\nbelow", "above\n\nbelow"},
		{"hr stars", "above\n***\nbelow", "above\n\nbelow"},

		// Blockquotes
		{"blockquote", "> this is quoted\n> second line", "this is quoted\nsecond line"},
		{"nested blockquote", "> > deep", "> deep"},

		// Bullets
		{"dash bullet", "- item one\n- item two", "• item one\n• item two"},
		{"star bullet", "* item one\n* item two", "• item one\n• item two"},
		{"plus bullet", "+ item one", "• item one"},
		{"indented bullet", "list:\n  - nested item", "list:\n  • nested item"},

		// Excessive newlines
		{"excessive newlines", "a\n\n\n\nb", "a\n\nb"},

		// Mixed
		{"mixed markdown", "## Hello\n\nThis is **bold** and `code`.\n\n- item\n- [link](url)\n\n> quote", "Hello\n\nThis is bold and code.\n\n• item\n• link (url)\n\nquote"},

		// Tables — converted to labeled bullet blocks (Zalo has no monospace / table render)
		{
			"table multi-col vietnamese",
			"| Họ tên | Chức vụ | Lương |\n|---|---|---|\n| Person A | Owner | 1,000,000đ |\n| Person B | Accountant | 2,000,000đ |",
			"• Person A\n  Chức vụ: Owner\n  Lương: 1,000,000đ\n• Person B\n  Chức vụ: Accountant\n  Lương: 2,000,000đ",
		},
		{
			"table 2-col key value",
			"| Key | Value |\n|---|---|\n| host | example.com |\n| port | 443 |",
			"• host\n  Value: example.com\n• port\n  Value: 443",
		},
		{
			"table with alignment colons",
			"| a | b |\n|:--|--:|\n| 1 | 2 |",
			"• 1\n  b: 2",
		},
		{
			"table empty cells skipped",
			"| a | b | c |\n|---|---|---|\n| 1 |  | 3 |",
			"• 1\n  c: 3",
		},
		{
			"table with prose around",
			"intro\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\noutro",
			"intro\n\n• 1\n  b: 2\n\noutro",
		},
		{
			"non-table pipes preserved",
			"plain | pipe | text\nanother line",
			"plain | pipe | text\nanother line",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripMarkdown(tt.in)
			if got != tt.want {
				t.Errorf("StripMarkdown(%q)\n got: %q\nwant: %q", tt.in, got, tt.want)
			}
		})
	}
}
