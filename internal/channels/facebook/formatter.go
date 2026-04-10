package facebook

import (
	"regexp"
	"strings"
)

const (
	commentMaxChars   = 8000
	messengerMaxChars = 2000
)

var (
	// Two-pass bold: **text** first, then *text*, to avoid stray asterisk artifacts.
	reBoldDouble = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	reBoldSingle = regexp.MustCompile(`\*([^*]+)\*`)
	// markdownLink converts [text](url) → "text (url)".
	reLink = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	// HTML tags.
	reHTML = regexp.MustCompile(`<[^>]+>`)
	// Heading markers (# … at line start).
	reHeader = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	// Inline `code` backticks.
	reCode = regexp.MustCompile("`([^`]+)`")
)

// FormatForComment sanitizes agent output for posting as a Facebook comment.
// Facebook comments do not support markdown or HTML.
func FormatForComment(text string) string {
	text = reHeader.ReplaceAllString(text, "")
	text = reBoldDouble.ReplaceAllString(text, "$1") // ** first
	text = reBoldSingle.ReplaceAllString(text, "$1") // * second
	text = reLink.ReplaceAllString(text, "$1 ($2)")
	text = reCode.ReplaceAllString(text, "$1")
	text = reHTML.ReplaceAllString(text, "")
	text = strings.TrimSpace(text)
	return truncateRunes(text, commentMaxChars)
}

// FormatForMessenger sanitizes agent output for Messenger messages.
func FormatForMessenger(text string) string {
	text = reHeader.ReplaceAllString(text, "")
	text = reBoldDouble.ReplaceAllString(text, "$1")
	text = reBoldSingle.ReplaceAllString(text, "$1")
	text = reLink.ReplaceAllString(text, "$1 ($2)")
	text = reCode.ReplaceAllString(text, "$1")
	text = reHTML.ReplaceAllString(text, "")
	return strings.TrimSpace(text)
}

// splitMessage splits text into chunks of at most maxChars runes,
// preferring paragraph or sentence boundaries to avoid mid-word cuts.
// Uses plain-text splitting (not channels.ChunkMarkdown) because Facebook
// strips all markdown — fenced code block repair is unnecessary.
func splitMessage(text string, maxChars int) []string {
	runes := []rune(text) // convert once
	if len(runes) <= maxChars {
		return []string{text}
	}

	var parts []string
	for len(runes) > maxChars {
		chunk := string(runes[:maxChars])

		// Try paragraph boundary.
		if idx := strings.LastIndex(chunk, "\n\n"); idx > maxChars/2 {
			parts = append(parts, strings.TrimSpace(chunk[:idx]))
			runes = []rune(strings.TrimSpace(string(runes[idx+2:])))
			continue
		}

		// Try sentence boundary.
		split := false
		for _, sep := range []string{". ", "! ", "? ", "\n"} {
			if idx := strings.LastIndex(chunk, sep); idx > maxChars/2 {
				parts = append(parts, strings.TrimSpace(chunk[:idx+1]))
				runes = []rune(strings.TrimSpace(string(runes[idx+len(sep):])))
				split = true
				break
			}
		}
		if split {
			continue
		}

		// Hard cut at maxChars.
		parts = append(parts, strings.TrimSpace(chunk))
		runes = []rune(strings.TrimSpace(string(runes[maxChars:])))
	}

	if remaining := strings.TrimSpace(string(runes)); remaining != "" {
		parts = append(parts, remaining)
	}
	return parts
}

// truncateRunes truncates s to at most n Unicode code points.
func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
