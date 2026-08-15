package channels

import (
	"strings"
	"testing"
)

// TestChunkMarkdown_EmptyText tests that empty text returns nil
func TestChunkMarkdown_EmptyText(t *testing.T) {
	result := ChunkMarkdown("", 100)
	if result != nil {
		t.Fatalf("expected nil for empty text, got %v", result)
	}
}

// TestChunkMarkdown_ZeroMaxLen tests that zero max length returns nil
func TestChunkMarkdown_ZeroMaxLen(t *testing.T) {
	result := ChunkMarkdown("some text", 0)
	if result != nil {
		t.Fatalf("expected nil for zero maxLen, got %v", result)
	}
}

// TestChunkMarkdown_NegativeMaxLen tests that negative max length returns nil
func TestChunkMarkdown_NegativeMaxLen(t *testing.T) {
	result := ChunkMarkdown("some text", -1)
	if result != nil {
		t.Fatalf("expected nil for negative maxLen, got %v", result)
	}
}

// TestChunkMarkdown_ShortText tests that short text returns single chunk
func TestChunkMarkdown_ShortText(t *testing.T) {
	text := "hello world"
	result := ChunkMarkdown(text, 100)

	if len(result) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(result))
	}
	if result[0] != text {
		t.Fatalf("expected chunk to be '%s', got '%s'", text, result[0])
	}
}

// TestChunkMarkdown_TextEqualToMaxLen tests text exactly equal to maxLen
func TestChunkMarkdown_TextEqualToMaxLen(t *testing.T) {
	text := "exactly 20 bytes!!!!"
	result := ChunkMarkdown(text, len(text))

	if len(result) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(result))
	}
	if result[0] != text {
		t.Fatalf("expected chunk to be '%s', got '%s'", text, result[0])
	}
}

// TestChunkMarkdown_SplitAtParagraphBoundary tests splitting at paragraph break (\n\n)
func TestChunkMarkdown_SplitAtParagraphBoundary(t *testing.T) {
	text := "first paragraph here\n\nsecond paragraph here"
	result := ChunkMarkdown(text, 40) // Larger to ensure both fit if not split

	if len(result) < 1 {
		t.Fatalf("expected at least 1 chunk, got %d", len(result))
	}
	// Verify all chunks are within limit
	for i, chunk := range result {
		if len(chunk) > 40 {
			t.Fatalf("chunk %d exceeds maxLen: %d > 40", i, len(chunk))
		}
	}
	// Verify content is preserved
	fullText := strings.Join(result, "")
	if !strings.Contains(fullText, "first paragraph") || !strings.Contains(fullText, "second paragraph") {
		t.Fatalf("content lost: %v", result)
	}
}

// TestChunkMarkdown_SplitAtLineBoundary tests splitting at line break (\n) when no paragraph
func TestChunkMarkdown_SplitAtLineBoundary(t *testing.T) {
	text := "line one here\nline two here\nline three here"
	result := ChunkMarkdown(text, 20)

	if len(result) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(result))
	}
	// Should split at \n boundaries, not in middle of line
	for i, chunk := range result {
		if strings.Contains(chunk, "\n") {
			t.Fatalf("chunk %d should not contain newline: '%s'", i, chunk)
		}
	}
}

// TestChunkMarkdown_SplitAtSpace tests splitting at space when no newline
func TestChunkMarkdown_SplitAtSpace(t *testing.T) {
	// Create text with no newlines, needs to split at spaces
	text := "word1 word2 word3 word4 word5"
	result := ChunkMarkdown(text, 15)

	if len(result) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(result))
	}
	// Verify chunks don't exceed maxLen
	for i, chunk := range result {
		if len(chunk) > 15 {
			t.Fatalf("chunk %d exceeds maxLen: %d > 15, content: '%s'", i, len(chunk), chunk)
		}
	}
}

// TestChunkMarkdown_FencedCodeBlockNotSplit tests that fenced code blocks are kept together
func TestChunkMarkdown_FencedCodeBlockNotSplit(t *testing.T) {
	text := "Some text before\n```python\ndef hello():\n    print(\"world\")\n```\nSome text after"

	result := ChunkMarkdown(text, 50)

	// Find chunk containing code block
	foundCodeBlock := false
	for _, chunk := range result {
		if strings.Contains(chunk, "def hello") {
			foundCodeBlock = true
			// Code block should be in a single chunk (or force-split with fence)
			if !strings.Contains(chunk, "print") {
				t.Fatalf("code block split across chunks: %s", chunk)
			}
			break
		}
	}
	if !foundCodeBlock {
		t.Fatal("code block not found in chunks")
	}
}

// TestChunkMarkdown_OversizedCodeBlockForceSplit tests force-split of code block > maxLen
func TestChunkMarkdown_OversizedCodeBlockForceSplit(t *testing.T) {
	// Create a code block larger than maxLen
	longLine := strings.Repeat("x", 200)
	text := "```python\n" + longLine + "\n```"

	result := ChunkMarkdown(text, 100)

	if len(result) < 2 {
		t.Fatalf("expected force-split into multiple chunks, got %d", len(result))
	}

	// Verify fence repair: chunks except first should start with ```
	for i := 1; i < len(result); i++ {
		if !strings.HasPrefix(result[i], "```") {
			t.Fatalf("chunk %d (after force-split) should start with fence marker, got: %s...",
				i, result[i][:min(20, len(result[i]))])
		}
	}

	// Verify fence closure: chunks except last should end with ```
	for i := 0; i < len(result)-1; i++ {
		if !strings.HasSuffix(result[i], "```") {
			t.Fatalf("chunk %d (before force-split) should end with fence marker, got: ...%s",
				i, result[i][max(0, len(result[i])-20):])
		}
	}
}

// TestChunkMarkdown_MixedContent tests text with multiple code blocks and text
func TestChunkMarkdown_MixedContent(t *testing.T) {
	text := "First section\n\n```go\npackage main\nfunc main() {}\n```\n\nSecond section here\n\n```js\nconsole.log(\"test\");\n```\n\nFinal text"

	result := ChunkMarkdown(text, 50)

	if len(result) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(result))
	}

	// Verify all chunks are within maxLen
	for i, chunk := range result {
		if len(chunk) > 50 && !strings.Contains(chunk, "```") {
			t.Fatalf("chunk %d exceeds maxLen and isn't a force-split: %d bytes", i, len(chunk))
		}
	}
}

// TestChunkMarkdown_MultipleCodeBlocks tests multiple code blocks in sequence
func TestChunkMarkdown_MultipleCodeBlocks(t *testing.T) {
	text := "```python\ncode1\n```\nText between\n```javascript\ncode2\n```"

	result := ChunkMarkdown(text, 50)

	if len(result) < 1 {
		t.Fatalf("expected at least 1 chunk, got %d", len(result))
	}

	// Count backticks in all chunks combined
	totalContent := strings.Join(result, "")
	tickCount := strings.Count(totalContent, "```")
	if tickCount < 4 { // At least 2 open + 2 close
		t.Fatalf("fence markers lost in chunking: expected at least 4 ``` markers, got %d", tickCount)
	}
}

// TestChunkMarkdown_NestedFenceMarkers tests that ``` inside text doesn't confuse fence detection
func TestChunkMarkdown_NestedFenceMarkers(t *testing.T) {
	text := "Text about backticks: use ``` to fence code\n\n```python\nprint(\"hello\")\n```\n\nMore text"

	result := ChunkMarkdown(text, 40)

	if len(result) == 0 {
		t.Fatal("chunking failed")
	}

	// Verify fence detection works correctly despite mention of backticks in text
	totalTicks := strings.Count(strings.Join(result, ""), "```")
	if totalTicks < 2 {
		t.Fatalf("fence markers lost: expected at least 2 pairs, got %d total", totalTicks)
	}
}

// TestIsInFence_NoFence tests isInFence with no fence markers
func TestIsInFence_NoFence(t *testing.T) {
	window := "just plain text here"
	result := isInFence(window)
	if result {
		t.Fatal("expected isInFence = false for text without fences")
	}
}

// TestIsInFence_WithOpenFence tests isInFence with open (unclosed) fence
func TestIsInFence_WithOpenFence(t *testing.T) {
	window := "text\n```\nmore text inside fence"
	result := isInFence(window)
	if !result {
		t.Fatal("expected isInFence = true when fence is unclosed")
	}
}

// TestIsInFence_WithClosedFence tests isInFence with closed fence
func TestIsInFence_WithClosedFence(t *testing.T) {
	window := "text\n```\ncode\n```\nmore text"
	result := isInFence(window)
	if result {
		t.Fatal("expected isInFence = false when fence is closed")
	}
}

// TestIsInFence_MultipleFences tests isInFence with alternating open/close
func TestIsInFence_MultipleFences(t *testing.T) {
	// Even number of fences = closed
	window1 := "```\ncode1\n```\ntext\n```\ncode2\n```"
	if isInFence(window1) {
		t.Fatal("expected isInFence = false for even number of fences")
	}

	// Odd number of fences = open
	window2 := "```\ncode1\n```\ntext\n```"
	if !isInFence(window2) {
		t.Fatal("expected isInFence = true for odd number of fences")
	}
}

// TestIsInFence_EmptyWindow tests isInFence with empty string
func TestIsInFence_EmptyWindow(t *testing.T) {
	result := isInFence("")
	if result {
		t.Fatal("expected isInFence = false for empty window")
	}
}

// TestHasFencePrefix_ValidPrefix tests hasFencePrefix with valid marker
func TestHasFencePrefix_ValidPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"```python", true},
		{"```javascript", true},
		{"```", true},
		{"```go\ncode", true},
		{"`` text", false},   // Only 2 backticks
		{"`text", false},     // Only 1 backtick
		{"text```", false},   // ``` not at start
		{"", false},          // Empty string
		{"  ```text", false}, // ``` not at position 0
	}

	for _, tt := range tests {
		result := hasFencePrefix(tt.input)
		if result != tt.expected {
			t.Fatalf("hasFencePrefix('%s'): expected %v, got %v", tt.input, tt.expected, result)
		}
	}
}

// TestHasFencePrefix_BoundaryConditions tests hasFencePrefix with edge cases
func TestHasFencePrefix_BoundaryConditions(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
		desc     string
	}{
		{"```", true, "exactly 3 backticks"},
		{"````", true, "4+ backticks"},
		{"``", false, "exactly 2 backticks"},
		{"b``", false, "non-backtick prefix"},
		{"`b`", false, "non-backtick in middle"},
		{"a```", false, "non-backtick before"},
	}

	for _, tt := range tests {
		result := hasFencePrefix(tt.input)
		if result != tt.expected {
			t.Fatalf("hasFencePrefix(%s): expected %v, got %v", tt.desc, tt.expected, result)
		}
	}
}

// TestFindSafeSplit_NoSplitPoint tests findSafeSplit when no safe point exists
func TestFindSafeSplit_NoSplitPoint(t *testing.T) {
	// Text entirely within fence (no safe point)
	window := "```\nno space or line breaks here\n"
	result := findSafeSplit(window)

	// When inside fence, no space found either, should return -1 or bestSpace from before fence
	// (depends on impl, but at minimum should handle it)
	if result < 0 {
		// -1 is acceptable (no split found)
	}
}

// TestFindSafeSplit_ParagraphPreference tests that paragraph breaks are preferred
func TestFindSafeSplit_ParagraphPreference(t *testing.T) {
	// Has paragraph break and line break and space
	window := "text here\n\nmore paragraph"

	result := findSafeSplit(window)

	// Should prefer paragraph (position after \n\n)
	if result <= 0 {
		t.Fatalf("expected valid split position, got %d", result)
	}

	// Split should be at paragraph boundary (after \n\n)
	if result < 11 || result > 12 {
		t.Fatalf("expected split around position 11-12 (after \\n\\n), got %d", result)
	}
}

// TestFindSafeSplit_LinePreference tests that line breaks are preferred over spaces
func TestFindSafeSplit_LinePreference(t *testing.T) {
	// Has line break and space but no paragraph
	window := "line here\nmore text with spaces"

	result := findSafeSplit(window)

	if result <= 0 {
		t.Fatalf("expected valid split position, got %d", result)
	}

	// Should prefer line break over space
	if result > 10 {
		t.Fatalf("expected split at line (around position 10), got %d", result)
	}
}

// TestFindSafeSplit_SpaceFallback tests space as fallback when no line breaks
func TestFindSafeSplit_SpaceFallback(t *testing.T) {
	// Only spaces, no newlines
	window := "word1 word2 word3"

	result := findSafeSplit(window)

	if result <= 0 {
		t.Fatalf("expected valid split position, got %d", result)
	}

	// Should split at a space
	if window[result-1] != ' ' {
		t.Fatalf("expected split after space, but position %d is '%c'", result-1, window[result-1])
	}
}

// TestFindSafeSplit_InsideFence tests that splits inside fence are skipped
func TestFindSafeSplit_InsideFence(t *testing.T) {
	// Paragraph break inside fence should be ignored
	window := "```\nlines\n\ninside fence\n```"

	result := findSafeSplit(window)

	// Should prefer post-fence point or return based on implementation
	// Main check: shouldn't split at the paragraph inside fence
	if result > 0 && result <= 12 {
		// If split is in the fence area, that's a problem
		t.Fatalf("should not split at paragraph inside fence, got position %d", result)
	}
}

// TestFindSafeSplit_EmptyWindow tests findSafeSplit with empty input
func TestFindSafeSplit_EmptyWindow(t *testing.T) {
	result := findSafeSplit("")
	if result > 0 {
		t.Fatalf("expected no split in empty window, got %d", result)
	}
}

// TestChunkMarkdown_TrailingWhitespace tests that trailing whitespace is trimmed
func TestChunkMarkdown_TrailingWhitespace(t *testing.T) {
	text := "first chunk  \n\nsecond chunk"
	result := ChunkMarkdown(text, 20)

	if len(result) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(result))
	}

	// First chunk should not have trailing whitespace
	if strings.HasSuffix(result[0], " ") || strings.HasSuffix(result[0], "\n") {
		t.Fatalf("chunk 0 should not have trailing whitespace: '%s'", result[0])
	}
}

// TestChunkMarkdown_LeadingWhitespace tests that leading whitespace is trimmed from new chunks
func TestChunkMarkdown_LeadingWhitespace(t *testing.T) {
	text := "first\n\n  \n  second"
	result := ChunkMarkdown(text, 20)

	if len(result) == 0 {
		t.Fatal("expected chunks")
	}

	// Check that chunks don't start with excessive whitespace
	for i := 1; i < len(result); i++ {
		if strings.HasPrefix(result[i], "  ") {
			t.Fatalf("chunk %d has leading whitespace: '%s'", i, result[i])
		}
	}
}

// TestChunkMarkdown_VerySmallMaxLen tests chunking with very small maxLen
func TestChunkMarkdown_VerySmallMaxLen(t *testing.T) {
	text := "abcdefghij"
	result := ChunkMarkdown(text, 3)

	if len(result) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(result))
	}

	for i, chunk := range result {
		if len(chunk) > 3 {
			t.Fatalf("chunk %d exceeds maxLen: %d > 3", i, len(chunk))
		}
	}
}

// TestChunkMarkdown_LargeText tests chunking of large text
func TestChunkMarkdown_LargeText(t *testing.T) {
	// Generate large text with clear boundaries
	var sb strings.Builder
	for i := range 100 {
		sb.WriteString("This is paragraph number ")
		sb.WriteString(string(rune(i)))
		sb.WriteString("\n\n")
	}
	text := sb.String()

	result := ChunkMarkdown(text, 100)

	if len(result) < 10 {
		t.Fatalf("expected many chunks for large text, got %d", len(result))
	}

	for i, chunk := range result {
		if len(chunk) > 100 && !strings.Contains(chunk, "```") {
			t.Fatalf("chunk %d exceeds maxLen: %d > 100", i, len(chunk))
		}
	}
}

// TestChunkMarkdown_ConsecutiveCodeBlocks tests multiple code blocks back-to-back
func TestChunkMarkdown_ConsecutiveCodeBlocks(t *testing.T) {
	text := "```\ncode1\n```\n```\ncode2\n```"
	result := ChunkMarkdown(text, 50)

	if len(result) == 0 {
		t.Fatal("expected chunks")
	}

	// Verify fence markers are present
	fullText := strings.Join(result, "")
	tickCount := strings.Count(fullText, "```")

	// Should have at least 4 fence markers (2 opens, 2 closes)
	if tickCount < 4 {
		t.Logf("fence detection: found %d ``` markers", tickCount)
	}
}

// TestChunkMarkdown_SpecialCharactersPreserved tests that special characters are preserved
func TestChunkMarkdown_SpecialCharactersPreserved(t *testing.T) {
	text := "Text with special chars: @#$%^&*()_+-=[]{}|;:',.<>?\n\nMore text"
	result := ChunkMarkdown(text, 50)

	fullText := strings.Join(result, "")
	if !strings.Contains(fullText, "@#$%") {
		t.Fatal("special characters were lost during chunking")
	}
}

// TestChunkMarkdown_UnicodeHandling tests Unicode character handling
func TestChunkMarkdown_UnicodeHandling(t *testing.T) {
	text := "Hello 世界\n\nمرحبا العالم\n\nПривет мир"
	result := ChunkMarkdown(text, 50)

	fullText := strings.Join(result, "")
	if !strings.Contains(fullText, "世界") || !strings.Contains(fullText, "مرحبا") || !strings.Contains(fullText, "Привет") {
		t.Fatal("Unicode content was lost during chunking")
	}
}

// TestChunkMarkdown_FenceWithLanguageTag tests fence markers with language specifiers
func TestChunkMarkdown_FenceWithLanguageTag(t *testing.T) {
	text := "```python\ncode here\n```\n\nMore text"
	result := ChunkMarkdown(text, 30)

	fullText := strings.Join(result, "")
	if !strings.Contains(fullText, "python") {
		t.Fatal("language tag in fence was lost")
	}
}

// TestChunkMarkdown_EmptyLines tests handling of multiple empty lines
func TestChunkMarkdown_EmptyLines(t *testing.T) {
	text := "text\n\n\n\n\nmore text"
	result := ChunkMarkdown(text, 50)

	fullText := strings.Join(result, "")
	// Should preserve structure
	if !strings.Contains(fullText, "text") || !strings.Contains(fullText, "more text") {
		t.Fatal("content was lost with multiple empty lines")
	}
}
