package googlechat

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMarkdownToGoogleChat(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "bold",
			input: "**bold**",
			want:  "*bold*",
		},
		{
			name:  "italic",
			input: "*italic*",
			want:  "_italic_",
		},
		{
			name:  "strikethrough",
			input: "~~strike~~",
			want:  "~strike~",
		},
		{
			name:  "link",
			input: "[text](https://example.com)",
			want:  "<https://example.com|text>",
		},
		{
			name:  "inline code unchanged",
			input: "`code`",
			want:  "`code`",
		},
		{
			name:  "code block unchanged",
			input: "```\nblock\n```",
			want:  "```\nblock\n```",
		},
		{
			name:  "bold and italic together",
			input: "**bold** và *italic*",
			want:  "*bold* và _italic_",
		},
		{
			name:  "inline code protects asterisks inside",
			input: "`*not italic*`",
			want:  "`*not italic*`",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := markdownToGoogleChat(tc.input)
			if got != tc.want {
				t.Errorf("markdownToGoogleChat(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestChunkByBytes_ShortASCII(t *testing.T) {
	// ASCII string well under 3900 bytes → single chunk.
	input := strings.Repeat("a", 3000)
	chunks := chunkByBytes(input, 3900)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0] != input {
		t.Error("chunk content differs from input")
	}
}

func TestChunkByBytes_LongASCII(t *testing.T) {
	// ASCII string over 3900 bytes → multiple chunks, each ≤ 3900 bytes.
	input := strings.Repeat("a", 8000)
	chunks := chunkByBytes(input, 3900)
	if len(chunks) < 2 {
		t.Fatalf("expected ≥2 chunks for 8000-byte ASCII, got %d", len(chunks))
	}
	for i, ch := range chunks {
		if len(ch) > 3900 {
			t.Errorf("chunk %d exceeds 3900 bytes: %d", i, len(ch))
		}
	}
	// Reassembled content must equal input.
	if strings.Join(chunks, "") != input {
		t.Error("reassembled chunks differ from input")
	}
}

func TestChunkByBytes_Vietnamese(t *testing.T) {
	// Vietnamese chars are 3 bytes each in UTF-8.
	// 1500 chars ≈ 4500 bytes → must split into ≥2 chunks.
	input := strings.Repeat("ắ", 1500)
	chunks := chunkByBytes(input, 3900)
	if len(chunks) < 2 {
		t.Fatalf("expected ≥2 chunks for ~4500-byte Vietnamese text, got %d", len(chunks))
	}
	for i, ch := range chunks {
		if len(ch) > 3900 {
			t.Errorf("chunk %d exceeds 3900 bytes: %d bytes", i, len(ch))
		}
		if !utf8.ValidString(ch) {
			t.Errorf("chunk %d is invalid UTF-8", i)
		}
	}
}

func TestChunkByBytes_SplitParagraph(t *testing.T) {
	// Two paragraphs each ~2200 bytes → should split at \n\n, not mid-paragraph.
	para1 := strings.Repeat("x", 2200)
	para2 := strings.Repeat("y", 2200)
	input := para1 + "\n\n" + para2
	chunks := chunkByBytes(input, 3900)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks (split at paragraph), got %d", len(chunks))
	}
	if chunks[0] != para1 {
		t.Errorf("first chunk should be para1, got len=%d", len(chunks[0]))
	}
	if chunks[1] != para2 {
		t.Errorf("second chunk should be para2, got len=%d", len(chunks[1]))
	}
}

func TestChunkByBytes_Emoji(t *testing.T) {
	// Family ZWJ emoji: 👨‍👩‍👧‍👦 — each is 25 bytes in UTF-8.
	// Repeat until > 3900 bytes, then check no chunk has invalid UTF-8.
	emoji := "👨‍👩‍👧‍👦"
	repeat := (3900/len(emoji) + 5)
	input := strings.Repeat(emoji, repeat)
	chunks := chunkByBytes(input, 3900)
	for i, ch := range chunks {
		if !utf8.ValidString(ch) {
			t.Errorf("chunk %d is invalid UTF-8", i)
		}
		if len(ch) > 3900 {
			t.Errorf("chunk %d exceeds 3900 bytes: %d", i, len(ch))
		}
	}
}

func TestChunkByBytes_SingleLongWord(t *testing.T) {
	// Single word without spaces, longer than maxBytes — must split at UTF-8 boundary.
	// Use Vietnamese chars (3 bytes each) to stress-test boundary detection.
	input := strings.Repeat("ắ", 2000) // ~6000 bytes, no spaces
	chunks := chunkByBytes(input, 3900)
	if len(chunks) < 2 {
		t.Fatalf("expected ≥2 chunks for single long word, got %d", len(chunks))
	}
	for i, ch := range chunks {
		if !utf8.ValidString(ch) {
			t.Errorf("chunk %d is invalid UTF-8", i)
		}
		if len(ch) > 3900 {
			t.Errorf("chunk %d exceeds 3900 bytes: %d", i, len(ch))
		}
	}
	// All content must be preserved.
	if strings.Join(chunks, "") != input {
		t.Error("reassembled chunks differ from input")
	}
}
