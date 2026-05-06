package max

import (
	"strings"
	"unicode/utf8"
)

// maxMessageBytes is the Max API per-message text limit.
// Documented limit: 4000 characters. We treat this as a byte count to be
// conservative — Max counts UTF-8 codepoints, but byte count is always
// >= codepoint count, so staying under N bytes guarantees < N codepoints.
const maxMessageBytes = 4000

// chunkText splits text into chunks no larger than maxMessageBytes each.
//
// Splitting strategy (best-fit, greedy):
//  1. Whole text fits → return as-is.
//  2. Paragraph boundaries (\n\n).
//  3. Line boundaries (\n).
//  4. Sentence boundaries (. ! ? followed by space or newline).
//  5. Word boundaries (whitespace).
//  6. Hard cut at maxMessageBytes (preserving UTF-8 codepoint boundaries).
//
// Empty input returns empty slice. The returned chunks have all leading and
// trailing whitespace trimmed.
func chunkText(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if len(text) <= maxMessageBytes {
		return []string{text}
	}

	var chunks []string
	remaining := text

	for len(remaining) > maxMessageBytes {
		cut := findSplitPoint(remaining, maxMessageBytes)
		chunk := strings.TrimSpace(remaining[:cut])
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		remaining = strings.TrimSpace(remaining[cut:])
	}
	if remaining != "" {
		chunks = append(chunks, remaining)
	}
	return chunks
}

// findSplitPoint returns the index at which to split a string so the prefix
// is no longer than max bytes, preferring natural boundaries.
//
// Search order (from preferred → least preferred):
//   - "\n\n" (paragraph)
//   - "\n"   (line)
//   - ". ", "! ", "? " (sentence)
//   - " "    (word)
//   - hard codepoint-safe cut
func findSplitPoint(s string, max int) int {
	if len(s) <= max {
		return len(s)
	}

	// Prefer paragraph boundary.
	if idx := strings.LastIndex(s[:max], "\n\n"); idx > 0 {
		return idx
	}

	// Then line boundary.
	if idx := strings.LastIndex(s[:max], "\n"); idx > 0 {
		return idx
	}

	// Then sentence boundary. Look for ". " "! " "? " "…\n" etc.
	for _, pat := range []string{". ", "! ", "? ", ".\n", "!\n", "?\n"} {
		if idx := strings.LastIndex(s[:max], pat); idx > 0 {
			// Include the punctuation in the chunk; consume the space.
			return idx + len(pat)
		}
	}

	// Word boundary.
	if idx := strings.LastIndex(s[:max], " "); idx > 0 {
		return idx
	}

	// Hard cut, preserving UTF-8 codepoint boundary.
	return safeUTF8Cut(s, max)
}

// safeUTF8Cut returns the largest n <= max where s[:n] ends on a UTF-8
// codepoint boundary. Avoids producing invalid UTF-8 strings.
func safeUTF8Cut(s string, max int) int {
	if max >= len(s) {
		return len(s)
	}
	// Walk back from max until we find a byte that starts a codepoint
	// (high bits not 10xxxxxx).
	for i := max; i > 0; i-- {
		if utf8.RuneStart(s[i]) {
			return i
		}
	}
	return 0
}
