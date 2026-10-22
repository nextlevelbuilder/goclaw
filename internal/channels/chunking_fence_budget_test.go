package channels

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestChunkMarkdownFenceRepairRespectsLimit(t *testing.T) {
	for _, body := range []string{strings.Repeat("x", 400), strings.Repeat("界", 150)} {
		text := "```go\n" + body + "\n```"
		chunks := ChunkMarkdown(text, 100)
		if len(chunks) < 2 {
			t.Fatal("expected an oversized block to be split")
		}
		for i, chunk := range chunks {
			if len(chunk) > 100 {
				t.Errorf("chunk %d has %d bytes, limit is 100", i, len(chunk))
			}
			if !utf8.ValidString(chunk) {
				t.Errorf("chunk %d splits a UTF-8 rune", i)
			}
		}
	}
}
func TestChunkMarkdownSmallFenceBudgets(t *testing.T) {
	for _, limit := range []int{9, 10, 11, 12} {
		for _, runeText := range []string{"x", "界", "\U0001F642"} {
			text := "```\n" + strings.Repeat(runeText, 20) + "\n```"
			chunks := ChunkMarkdown(text, limit)
			if strings.Count(strings.Join(chunks, ""), runeText) != 20 {
				t.Errorf("limit %d did not preserve the body", limit)
			}
			for i, chunk := range chunks {
				if len(chunk) > limit || !utf8.ValidString(chunk) {
					t.Errorf("limit %d, chunk %d is invalid or oversized: %q", limit, i, chunk)
				}
			}
		}
	}
}

func TestChunkMarkdownSmallFenceBudgetsPreserveSpaces(t *testing.T) {
	text := "```\n" + strings.Repeat("x ", 20) + "\n```"
	chunks := ChunkMarkdown(text, 11)
	if spaces := strings.Count(strings.Join(chunks, ""), " "); spaces != 20 {
		t.Fatalf("code body has %d spaces, want 20", spaces)
	}
}

func TestChunkMarkdownSmallFenceBudgetsPreserveCodeBytes(t *testing.T) {
	for _, limit := range []int{9, 10, 11} {
		for _, body := range []string{
			strings.Repeat("x ", 20),
			strings.Repeat("界 \U0001F642\n", 20),
			strings.Repeat("  x\t\n\n", 20),
		} {
			text := "```\n" + body + "\n```\nfollowing text"
			chunks := ChunkMarkdown(text, limit)
			if got := strings.Join(chunks, ""); got != text {
				t.Errorf("limit %d changed code bytes: got %q, want %q", limit, got, text)
			}
		}
	}
}
