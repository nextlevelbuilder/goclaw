// Package common holds shared building blocks used by all Zalo channel
// flavors (zalo_bot, zalo_oa, zalo_personal).
package common

import (
	"regexp"
	"strings"
)

// StripMarkdown returns plain text with markdown artifacts removed —
// Zalo does not support any markup rendering.
func StripMarkdown(text string) string {
	if text == "" {
		return text
	}

	text = reFencedCode.ReplaceAllString(text, "$1")
	text = reInlineCode.ReplaceAllString(text, "$1")
	text = reImage.ReplaceAllString(text, "")
	text = reLink.ReplaceAllString(text, "$1 ($2)")
	text = reBoldItalicStar.ReplaceAllString(text, "$1")
	text = reBoldItalicUnder.ReplaceAllString(text, "$1")
	text = reBoldStar.ReplaceAllString(text, "$1")
	text = reBoldUnder.ReplaceAllStringFunc(text, stripBoldUnder)
	text = reStrikethrough.ReplaceAllString(text, "$1")
	text = reHeader.ReplaceAllString(text, "$1")
	text = reHorizontalRule.ReplaceAllString(text, "")
	text = reBlockquote.ReplaceAllString(text, "$1")
	text = reBullet.ReplaceAllString(text, "${1}• ")
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

	reIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// stripBoldUnder strips __bold__ but preserves identifier-shaped content like
// __init__ / __name__ where the underscores are part of the token, not markup.
func stripBoldUnder(match string) string {
	inner := match[2 : len(match)-2]
	if reIdentifier.MatchString(inner) {
		return match
	}
	return inner
}
