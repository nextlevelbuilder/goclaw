package max

import (
	"strings"
	"testing"
)

func TestChunkText_EmptyAndShort(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", nil},
		{"only whitespace", "   \n  \n  ", nil},
		{"short text", "hello", []string{"hello"}},
		{"trailing whitespace", "hello\n\n", []string{"hello"}},
		{"leading whitespace", "  \nhello", []string{"hello"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := chunkText(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("chunkText: got %d chunks, want %d: %#v", len(got), len(tt.want), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("chunk[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestChunkText_ExactlyMax(t *testing.T) {
	text := strings.Repeat("a", maxMessageBytes)
	got := chunkText(text)
	if len(got) != 1 {
		t.Fatalf("got %d chunks, want 1", len(got))
	}
	if got[0] != text {
		t.Errorf("chunk corrupted")
	}
}

func TestChunkText_OverMax_ParagraphSplit(t *testing.T) {
	// Two paragraphs separated by \n\n. Each fits within max alone.
	p1 := strings.Repeat("a ", 1500) // 3000 bytes
	p2 := strings.Repeat("b ", 1500) // 3000 bytes
	text := p1 + "\n\n" + p2

	got := chunkText(text)
	if len(got) != 2 {
		t.Fatalf("got %d chunks, want 2 (paragraph split): lengths=%v",
			len(got), chunkLengths(got))
	}
	for i, c := range got {
		if len(c) > maxMessageBytes {
			t.Errorf("chunk[%d] = %d bytes, exceeds max %d", i, len(c), maxMessageBytes)
		}
	}
}

func TestChunkText_OverMax_LineSplit(t *testing.T) {
	// Single paragraph, multiple lines, no \n\n boundaries.
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		sb.WriteString(strings.Repeat("x", 30))
		sb.WriteByte('\n')
	}
	text := sb.String()
	if len(text) <= maxMessageBytes {
		t.Fatalf("test setup wrong: text only %d bytes", len(text))
	}

	got := chunkText(text)
	if len(got) < 2 {
		t.Fatalf("expected splitting, got %d chunks", len(got))
	}
	for i, c := range got {
		if len(c) > maxMessageBytes {
			t.Errorf("chunk[%d] = %d bytes, exceeds max", i, len(c))
		}
	}
}

func TestChunkText_OverMax_SentenceSplit(t *testing.T) {
	// One long line consisting of many sentences.
	sentence := strings.Repeat("z", 80) + ". "
	var sb strings.Builder
	for sb.Len() < maxMessageBytes*2 {
		sb.WriteString(sentence)
	}
	got := chunkText(sb.String())
	if len(got) < 2 {
		t.Fatalf("expected splitting, got %d", len(got))
	}
	for i, c := range got {
		if len(c) > maxMessageBytes {
			t.Errorf("chunk[%d] = %d bytes", i, len(c))
		}
	}
}

func TestChunkText_OverMax_WordSplit(t *testing.T) {
	// A single sentence (no period), only word boundaries.
	text := strings.Repeat("word ", 1000) // 5000 bytes, no sentence breaks
	got := chunkText(text)
	if len(got) < 2 {
		t.Fatalf("expected splitting, got %d", len(got))
	}
	for i, c := range got {
		if len(c) > maxMessageBytes {
			t.Errorf("chunk[%d] = %d bytes", i, len(c))
		}
	}
}

func TestChunkText_HardCut_NoNaturalBoundary(t *testing.T) {
	// Single token with no whitespace at all — must hard-cut.
	text := strings.Repeat("a", maxMessageBytes+500)
	got := chunkText(text)
	if len(got) < 2 {
		t.Fatalf("expected splitting, got %d", len(got))
	}
	for i, c := range got {
		if len(c) > maxMessageBytes {
			t.Errorf("chunk[%d] = %d bytes", i, len(c))
		}
	}
}

func TestChunkText_PreservesUTF8(t *testing.T) {
	// Multi-byte chars; must not split codepoints.
	// "ё" = 2 bytes in UTF-8. Pad to force a hard cut.
	text := strings.Repeat("ё", maxMessageBytes/2+500)
	got := chunkText(text)
	for i, c := range got {
		if !isValidUTF8(c) {
			t.Errorf("chunk[%d] is not valid UTF-8", i)
		}
		if len(c) > maxMessageBytes {
			t.Errorf("chunk[%d] = %d bytes", i, len(c))
		}
	}
}

func TestSafeUTF8Cut(t *testing.T) {
	// "тест" — each Cyrillic letter is 2 bytes in UTF-8.
	// Total: 8 bytes. If we ask cut at 5, must round down to 4 (after "те").
	s := "тест"
	got := safeUTF8Cut(s, 5)
	if got != 4 {
		t.Errorf("safeUTF8Cut(%q, 5) = %d, want 4", s, got)
	}
	if !isValidUTF8(s[:got]) {
		t.Errorf("result is invalid UTF-8")
	}
}

// chunkLengths returns the byte length of each chunk for debug output.
func chunkLengths(chunks []string) []int {
	out := make([]int, len(chunks))
	for i, c := range chunks {
		out[i] = len(c)
	}
	return out
}

// isValidUTF8 wraps utf8.ValidString for cleaner test code.
func isValidUTF8(s string) bool {
	for i := 0; i < len(s); {
		r, size := decodeRune(s[i:])
		if r == 0xFFFD && size == 1 {
			return false
		}
		i += size
	}
	return true
}

// decodeRune decodes one rune. Wraps unicode/utf8 to keep test self-contained.
func decodeRune(s string) (rune, int) {
	for _, r := range s {
		return r, len(string(r))
	}
	return 0, 0
}
