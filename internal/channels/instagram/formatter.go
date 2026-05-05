package instagram

import (
	"regexp"
	"strings"
)

// instagramMaxChars is the maximum message length for Instagram DM (characters, not bytes).
const instagramMaxChars = 1000

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

// FormatMessage sanitizes agent output for Instagram DM and truncates to
// the platform's rune limit. Instagram DM does not render markdown or HTML;
// raw asterisks, backticks, and "[text](url)" would otherwise show literally.
// Rune-based truncation preserves UTF-8 validity for non-ASCII content
// (Vietnamese, Chinese, emoji) — byte slicing can split a multibyte
// codepoint and produce invalid UTF-8 that the Graph API rejects.
func FormatMessage(content string) string {
	content = reHeader.ReplaceAllString(content, "")
	content = reBoldDouble.ReplaceAllString(content, "$1")
	content = reBoldSingle.ReplaceAllString(content, "$1")
	content = reLink.ReplaceAllString(content, "$1 ($2)")
	content = reCode.ReplaceAllString(content, "$1")
	content = reHTML.ReplaceAllString(content, "")
	content = strings.TrimSpace(content)

	runes := []rune(content)
	if len(runes) <= instagramMaxChars {
		return content
	}
	return string(runes[:instagramMaxChars-3]) + "..."
}
