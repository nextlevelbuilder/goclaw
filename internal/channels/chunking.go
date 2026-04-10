package channels

import (
	"strings"
	"unicode/utf8"
)

// ChunkMarkdown splits markdown text into chunks of at most maxLen bytes,
// respecting fenced code blocks (``` ... ```). Prefers paragraph > line > space
// boundaries. Force-splits oversized code blocks with fence repair (close/reopen).
func ChunkMarkdown(text string, maxLen int) []string {
	if text == "" || maxLen <= 0 {
		return nil
	}
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	remaining := text
	needFenceReopen := false

	for len(remaining) > 0 {
		// Repair fence from previous force-split
		if needFenceReopen {
			remaining = "```\n" + remaining
			needFenceReopen = false
		}

		if len(remaining) <= maxLen {
			chunks = append(chunks, remaining)
			break
		}

		window := remaining[:maxLen]
		cutAt := findSafeSplit(window)

		if cutAt > 0 {
			// Safe split found outside fenced block
			chunks = append(chunks, strings.TrimRight(remaining[:cutAt], " \n"))
			remaining = strings.TrimLeft(remaining[cutAt:], "\n")
		} else {
			// No safe split — force at maxLen, adjust to rune boundary
			cutAt := maxLen
			for cutAt > 0 && !utf8.RuneStart(remaining[cutAt]) {
				cutAt--
			}
			if cutAt == 0 {
				cutAt = maxLen // shouldn't happen, but prevent infinite loop
			}

			if isInFence(window) {
				chunks = append(chunks, remaining[:cutAt]+"\n```")
				needFenceReopen = true
			} else {
				chunks = append(chunks, strings.TrimRight(remaining[:cutAt], " \n"))
			}
			remaining = remaining[cutAt:]
		}
	}

	return chunks
}

// findSafeSplit finds the best split position in window that is NOT inside
// a fenced code block. Returns -1 if no safe point exists.
// Preference: paragraph (\n\n) > line (\n) > space.
func findSafeSplit(window string) int {
	inFence := false
	bestPara := -1
	bestLine := -1
	bestSpace := -1

	for i := 0; i < len(window); i++ {
		// Detect fence lines (3+ backticks at start of line)
		if i == 0 || (i > 0 && window[i-1] == '\n') {
			if hasFencePrefix(window[i:]) {
				inFence = !inFence
			}
		}

		if inFence {
			continue
		}

		switch window[i] {
		case '\n':
			if i+1 < len(window) && window[i+1] == '\n' {
				bestPara = i + 2
			} else {
				bestLine = i + 1
			}
		case ' ':
			bestSpace = i + 1
		}
	}

	if bestPara > 0 {
		return bestPara
	}
	if bestLine > 0 {
		return bestLine
	}
	return bestSpace
}

// isInFence returns true if the end of window is inside an unclosed fenced code block.
func isInFence(window string) bool {
	inFence := false
	for i := 0; i < len(window); i++ {
		if i == 0 || (i > 0 && window[i-1] == '\n') {
			if hasFencePrefix(window[i:]) {
				inFence = !inFence
			}
		}
	}
	return inFence
}

// hasFencePrefix returns true if s starts with 3+ backticks (fenced code block marker).
func hasFencePrefix(s string) bool {
	return len(s) >= 3 && s[0] == '`' && s[1] == '`' && s[2] == '`'
}
