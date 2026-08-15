package zalo

import (
	"regexp"
	"strings"
)

// StripMarkdown removes markdown formatting artifacts from text, producing
// clean plain text suitable for Zalo which does not support any markup.
func StripMarkdown(text string) string {
	if text == "" {
		return text
	}

	// 1. Strip fenced code blocks — keep content, remove ``` delimiters
	text = reFencedCode.ReplaceAllString(text, "$1")

	// 2. Strip inline code backticks
	text = reInlineCode.ReplaceAllString(text, "$1")

	// 3. Strip images ![alt](url) — remove entirely
	text = reImage.ReplaceAllString(text, "")

	// 4. Strip links [text](url) → text (url)
	text = reLink.ReplaceAllString(text, "$1 ($2)")

	// 5. Strip bold+italic (***text*** or ___text___)
	text = reBoldItalicStar.ReplaceAllString(text, "$1")
	text = reBoldItalicUnder.ReplaceAllString(text, "$1")

	// 6. Strip bold (**text** or __text__)
	text = reBoldStar.ReplaceAllString(text, "$1")
	text = reBoldUnder.ReplaceAllString(text, "$1")

	// 7. Strip strikethrough ~~text~~
	text = reStrikethrough.ReplaceAllString(text, "$1")

	// 8. Strip headers (lines starting with #)
	text = reHeader.ReplaceAllString(text, "$1")

	// 9. Strip horizontal rules
	text = reHorizontalRule.ReplaceAllString(text, "")

	// 10. Strip blockquotes
	text = reBlockquote.ReplaceAllString(text, "$1")

	// 11. Replace bullet markers with •
	text = reBullet.ReplaceAllString(text, "${1}• ")

	// Clean up excessive blank lines (3+ → 2)
	text = reExcessiveNewlines.ReplaceAllString(text, "\n\n")

	return strings.TrimSpace(text)
}

var (
	reFencedCode      = regexp.MustCompile("(?s)```[a-zA-Z0-9]*\\n?(.*?)```")
	reInlineCode      = regexp.MustCompile("`([^`]+)`")
	reImage           = regexp.MustCompile(`!\[[^\]]*\]\([^)]+\)`)
	reLink            = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	reBoldItalicStar  = regexp.MustCompile(`\*{3}(.+?)\*{3}`)
	reBoldItalicUnder = regexp.MustCompile(`_{3}(.+?)_{3}`)
	reBoldStar        = regexp.MustCompile(`\*{2}(.+?)\*{2}`)
	reBoldUnder       = regexp.MustCompile(`_{2}(.+?)_{2}`)
	reStrikethrough   = regexp.MustCompile(`~~(.+?)~~`)
	reHeader          = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	reHorizontalRule  = regexp.MustCompile(`(?m)^[\s]*[-*_]{3,}[\s]*$`)
	reBlockquote      = regexp.MustCompile(`(?m)^>\s?(.*)$`)
	reBullet          = regexp.MustCompile(`(?m)^(\s*)[-*+]\s+`)

	reExcessiveNewlines = regexp.MustCompile(`\n{3,}`)
)
