package googlechat

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	// googleChatMaxMessageBytes is the safe byte limit per message (API hard limit is 4096B).
	// Conservative margin handles multi-byte UTF-8 sequences at chunk boundaries.
	googleChatMaxMessageBytes = 3900

	// codePlaceholderStart/End use STX/ETX control chars (0x02/0x03).
	// These never appear in LLM output text, ensuring safe placeholder delimiters.
	codePlaceholderStart = "\x02"
	codePlaceholderEnd   = "\x03"
)

var (
	reCodeBlock  = regexp.MustCompile("```[\\w]*\\n?([\\s\\S]*?)```")
	reInlineCode = regexp.MustCompile("`([^`]+)`")
	reBold       = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reStrike     = regexp.MustCompile(`~~(.+?)~~`)
	reLink       = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	reItalic     = regexp.MustCompile(`\*([^*\n]+)\*`)
)

// markdownToGoogleChat converts Markdown syntax to Google Chat message format.
//
// Conversion rules:
//
//	**bold**     → *bold*
//	*italic*     → _italic_
//	~~strike~~   → ~strike~
//	[text](url)  → <url|text>
//
// Code blocks and inline code are preserved unchanged.
func markdownToGoogleChat(text string) string {
	if text == "" {
		return ""
	}

	// Step 1: Protect code blocks and inline codes with STX/ETX placeholders.
	var codeProtected []string
	codeIdx := 0

	text = reCodeBlock.ReplaceAllStringFunc(text, func(match string) string {
		codeProtected = append(codeProtected, match)
		p := fmt.Sprintf("%s%d%s", codePlaceholderStart, codeIdx, codePlaceholderEnd)
		codeIdx++
		return p
	})
	text = reInlineCode.ReplaceAllStringFunc(text, func(match string) string {
		codeProtected = append(codeProtected, match)
		p := fmt.Sprintf("%s%d%s", codePlaceholderStart, codeIdx, codePlaceholderEnd)
		codeIdx++
		return p
	})

	// Step 2: Protect bold markers (**...**) before italic conversion.
	// Without this, italic regex would also match already-converted *bold* patterns.
	// Uses EOT char (0x04) as bold placeholder delimiter — distinct from code placeholders.
	const boldMark = "\x04"
	var boldInners []string
	boldIdx := 0

	text = reBold.ReplaceAllStringFunc(text, func(match string) string {
		inner := reBold.FindStringSubmatch(match)[1]
		boldInners = append(boldInners, inner)
		p := fmt.Sprintf("%s%d%s", boldMark, boldIdx, boldMark)
		boldIdx++
		return p
	})

	// Step 3: Convert italic — safe now that bold markers are protected.
	text = convertItalic(text)

	// Step 4: Restore bold as Google Chat format (*bold*).
	for idx, inner := range boldInners {
		text = strings.ReplaceAll(text, fmt.Sprintf("%s%d%s", boldMark, idx, boldMark), "*"+inner+"*")
	}

	// Step 5: Remaining conversions.
	text = reStrike.ReplaceAllString(text, "~$1~")
	text = reLink.ReplaceAllString(text, "<$2|$1>")

	// Step 6: Restore protected code blocks and inline codes.
	for idx, content := range codeProtected {
		text = strings.ReplaceAll(text, fmt.Sprintf("%s%d%s", codePlaceholderStart, idx, codePlaceholderEnd), content)
	}

	return text
}

// convertItalic converts *italic* to _italic_.
// Assumes bold markers (**...**) have been protected before calling.
// Uses ${1} (not $1) to avoid Go regexp parsing "_$1_" as variable "$1_".
func convertItalic(s string) string {
	return reItalic.ReplaceAllString(s, "_${1}_")
}

// chunkByBytes splits text into chunks where each chunk is at most maxBytes bytes.
// Split priority: paragraph break (\n\n) → line break (\n) → word boundary → UTF-8 boundary.
func chunkByBytes(text string, maxBytes int) []string {
	if text == "" {
		return nil
	}
	if len(text) <= maxBytes {
		return []string{text}
	}

	var chunks []string
	remaining := text

	for len(remaining) > 0 {
		if len(remaining) <= maxBytes {
			chunks = append(chunks, remaining)
			break
		}

		// Inspect first maxBytes as a byte window. ASCII delimiters (\n, space) are safe
		// to search even if the window cuts in the middle of a multi-byte UTF-8 sequence.
		window := remaining[:maxBytes]

		cutAt, skip := findCutPoint(window)
		if cutAt > 0 {
			chunks = append(chunks, strings.TrimRight(remaining[:cutAt], " \t"))
			remaining = strings.TrimLeft(remaining[cutAt+skip:], " \t\n")
			continue
		}

		// No ASCII delimiter found — fall back to UTF-8 rune boundary.
		boundary := utf8SafeBoundary(remaining, maxBytes)
		chunks = append(chunks, remaining[:boundary])
		remaining = remaining[boundary:]
	}

	return chunks
}

// findCutPoint returns the best byte offset to cut and the number of delimiter bytes to skip.
// Priority: \n\n > \n > space. Returns (0, 0) if no delimiter found.
func findCutPoint(s string) (cutAt, skip int) {
	if idx := strings.LastIndex(s, "\n\n"); idx > 0 {
		return idx, 2
	}
	if idx := strings.LastIndex(s, "\n"); idx > 0 {
		return idx, 1
	}
	if idx := strings.LastIndex(s, " "); idx > 0 {
		return idx, 1
	}
	return 0, 0
}

// utf8SafeBoundary returns the largest byte index ≤ maxBytes that is a valid UTF-8 rune start.
func utf8SafeBoundary(s string, maxBytes int) int {
	cutAt := maxBytes
	for cutAt > 0 && !utf8.RuneStart(s[cutAt]) {
		cutAt--
	}
	if cutAt == 0 {
		// Single rune larger than maxBytes — take it whole to avoid infinite loop.
		_, size := utf8.DecodeRuneInString(s)
		return size
	}
	return cutAt
}

// chunkByLines splits text at line boundaries, keeping each chunk under maxBytes.
func chunkByLines(text string, maxBytes int) []string {
	lines := strings.Split(text, "\n")
	var chunks []string
	var cur strings.Builder

	for _, line := range lines {
		need := len(line) + 1 // +1 for \n
		if cur.Len()+need > maxBytes && cur.Len() > 0 {
			chunks = append(chunks, strings.TrimRight(cur.String(), "\n"))
			cur.Reset()
		}
		cur.WriteString(line)
		cur.WriteByte('\n')
	}
	if cur.Len() > 0 {
		chunks = append(chunks, strings.TrimRight(cur.String(), "\n"))
	}
	return chunks
}

// chunkByWords splits text at word boundaries, keeping each chunk under maxBytes.
func chunkByWords(text string, maxBytes int) []string {
	words := strings.Fields(text)
	var chunks []string
	var cur strings.Builder

	for _, word := range words {
		need := len(word)
		if cur.Len() > 0 {
			need++ // space separator
		}
		if cur.Len()+need > maxBytes && cur.Len() > 0 {
			chunks = append(chunks, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(word)
	}
	if cur.Len() > 0 {
		chunks = append(chunks, cur.String())
	}
	return chunks
}

// splitAtUTF8Boundary splits a string into chunks of at most maxBytes bytes,
// cutting only at valid UTF-8 rune boundaries. Used as last resort when no
// word or line delimiters exist within maxBytes.
func splitAtUTF8Boundary(word string, maxBytes int) []string {
	if len(word) <= maxBytes {
		return []string{word}
	}
	var chunks []string
	for len(word) > maxBytes {
		cut := utf8SafeBoundary(word, maxBytes)
		chunks = append(chunks, word[:cut])
		word = word[cut:]
	}
	if len(word) > 0 {
		chunks = append(chunks, word)
	}
	return chunks
}
