package googlechat

import (
	"strings"
	"testing"
)

func TestMarkdownToGoogleChat(t *testing.T) {
	tests := []struct {
		name, input, want string
	}{
		{"bold", "**hello**", "*hello*"},
		{"italic", "*hello*", "_hello_"},
		{"strikethrough", "~~deleted~~", "~deleted~"},
		{"code inline", "`code`", "`code`"},
		{"code block", "```go\nfunc(){}\n```", "```go\nfunc(){}\n```"},
		{"mixed", "**bold** and *italic*", "*bold* and _italic_"},
		{"link", "[text](https://example.com)", "<https://example.com|text>"},
		{"nested bold+italic", "***both***", "*_both_*"},
		{"empty", "", ""},
		{"plain text", "plain text", "plain text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := markdownToGoogleChat(tt.input)
			if got != tt.want {
				t.Errorf("markdownToGoogleChat(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDetectStructuredContent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"has table", "| col1 | col2 |\n|---|---|\n| a | b |", true},
		{"has long code block", "```\n" + string(make([]byte, 600)) + "\n```", true},
		{"short code block", "```\nshort\n```", false},
		{"plain text", "Hello world", false},
		{"inline code only", "`code` in text", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectStructuredContent(tt.input); got != tt.want {
				t.Errorf("detectStructuredContent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChunkByBytes(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		maxBytes  int
		wantCount int
	}{
		{"under limit", "hello", googleChatMaxMessageBytes, 1},
		{"empty", "", googleChatMaxMessageBytes, 0},
		{"over limit paragraph split", "para one\n\npara two\n\npara three", 20, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := chunkByBytes(tt.input, tt.maxBytes)
			if len(chunks) != tt.wantCount {
				t.Errorf("chunkByBytes() returned %d chunks, want %d", len(chunks), tt.wantCount)
			}
			for i, c := range chunks {
				if len([]byte(c)) > tt.maxBytes {
					t.Errorf("chunk[%d] = %d bytes, exceeds max %d", i, len([]byte(c)), tt.maxBytes)
				}
			}
		})
	}
}

func TestChunkByBytes_Unicode(t *testing.T) {
	vn := "Đây là một đoạn văn bản tiếng Việt dài để kiểm tra việc chia chunk theo byte"
	chunks := chunkByBytes(vn, 50)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if len([]byte(c)) > 50 {
			t.Errorf("chunk[%d] = %d bytes, exceeds 50", i, len([]byte(c)))
		}
		if c == "" {
			t.Errorf("chunk[%d] is empty", i)
		}
	}
	// Verify all words are preserved across chunks.
	reassembled := strings.Join(chunks, " ")
	if reassembled != vn {
		t.Errorf("reassembled text doesn't match original:\ngot:  %q\nwant: %q", reassembled, vn)
	}
}

func TestExtractSummary(t *testing.T) {
	tests := []struct {
		name, input, want string
	}{
		{"heading + bullets", "# Title\n- A\n- B\n- C\n- D\n- E", "Title\n\n- A\n- B\n- C"},
		{"no heading", "- A\n- B\n- C\n- D", "- A\n- B\n- C"},
		{"very short", "Hello", "Hello"},
		{"only heading", "# Title", "Title"},
		{"multiple headings", "# H1\ntext here\n## H2\nmore text", "H1\n\ntext here"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractSummary(tt.input); got != tt.want {
				t.Errorf("extractSummary(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
