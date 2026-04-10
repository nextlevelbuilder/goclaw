package whatsapp

import "testing"

func TestMarkdownToWhatsApp(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain text", "hello world", "hello world"},
		{"header h1", "# Title", "*Title*"},
		{"header h3", "### Sub", "*Sub*"},
		{"bold stars", "this is **bold** text", "this is *bold* text"},
		{"bold underscores", "this is __bold__ text", "this is *bold* text"},
		{"strikethrough", "~~deleted~~", "~deleted~"},
		{"inline code", "use `fmt.Println`", "use ```fmt.Println```"},
		{"link", "[Go](https://go.dev)", "Go https://go.dev"},
		{"unordered list dash", "- item one\n- item two", "• item one\n• item two"},
		{"unordered list star", "* item one\n* item two", "• item one\n• item two"},
		{"blockquote", "> quoted text", "quoted text"},
		{
			"fenced code block preserved",
			"```go\nfmt.Println(\"hi\")\n```",
			"```\nfmt.Println(\"hi\")\n```",
		},
		{
			"code block not mangled by bold regex",
			"```\n**not bold**\n```",
			"```\n**not bold**\n```",
		},
		{"collapse blank lines", "a\n\n\n\nb", "a\n\nb"},
		{"html bold", "<b>bold</b>", "*bold*"},
		{"html italic", "<em>italic</em>", "_italic_"},
		{"html strikethrough", "<del>removed</del>", "~removed~"},
		{"html br", "line1<br>line2", "line1\nline2"},
		{"html link", `<a href="https://x.com">link</a>`, "link https://x.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := markdownToWhatsApp(tt.in)
			if got != tt.want {
				t.Errorf("markdownToWhatsApp(%q)\n got: %q\nwant: %q", tt.in, got, tt.want)
			}
		})
	}
}
