package pancake

import (
	"regexp"
	"strings"
)

// FormatOutbound formats agent response text for the target platform.
// Each platform has different formatting rules and supported markup.
func FormatOutbound(content string, platform string) string {
	switch platform {
	case "facebook":
		return formatForFacebook(content)
	case "whatsapp":
		return formatForWhatsApp(content)
	case "zalo", "instagram", "line":
		return stripMarkdown(content)
	case "tiktok":
		return stripMarkdown(truncateForTikTok(content))
	default:
		return stripMarkdown(content)
	}
}

// formatForFacebook allows basic HTML tags supported by Messenger.
// Strips unsupported tags, keeps bold/italic/links.
func formatForFacebook(content string) string {
	// Convert markdown bold (**text** or __text__) to plain (FB Messenger uses plain text)
	content = reBold.ReplaceAllString(content, "$1")
	content = reItalic.ReplaceAllString(content, "$1")
	// Strip markdown code blocks and inline code
	content = reCodeBlock.ReplaceAllString(content, "$1")
	content = reInlineCode.ReplaceAllString(content, "$1")
	// Strip markdown headers (## Heading → Heading)
	content = reHeader.ReplaceAllString(content, "$1")
	return strings.TrimSpace(content)
}

// formatForWhatsApp converts markdown to WhatsApp-native formatting.
// WhatsApp uses *bold*, _italic_, ~strikethrough~, ```code```.
func formatForWhatsApp(content string) string {
	// Convert **bold** → *bold* (WhatsApp format)
	content = reDoubleBold.ReplaceAllString(content, "*$1*")
	// Convert __italic__ → _italic_ (already matches WA format, just clean up __)
	content = reDoubleUnderline.ReplaceAllString(content, "_$1_")
	// Strip markdown headers
	content = reHeader.ReplaceAllString(content, "$1")
	// Strip inline code backticks (keep content)
	content = reInlineCode.ReplaceAllString(content, "$1")
	return strings.TrimSpace(content)
}

// stripMarkdown removes common markdown formatting, returning plain text.
func stripMarkdown(content string) string {
	content = reBold.ReplaceAllString(content, "$1")
	content = reItalic.ReplaceAllString(content, "$1")
	content = reCodeBlock.ReplaceAllString(content, "$1")
	content = reInlineCode.ReplaceAllString(content, "$1")
	content = reHeader.ReplaceAllString(content, "$1")
	content = reLink.ReplaceAllString(content, "$1")
	content = reImage.ReplaceAllString(content, "")
	return strings.TrimSpace(content)
}

// truncateForTikTok truncates content to TikTok DM limit (500 runes).
// Uses rune slicing to avoid corrupting multi-byte UTF-8 (CJK, Vietnamese, emoji).
func truncateForTikTok(content string) string {
	const limit = 500
	runes := []rune(content)
	if len(runes) <= limit {
		return content
	}
	return string(runes[:limit-3]) + "..."
}

// Compiled regexes for markdown stripping — package-level for efficiency.
var (
	reBold           = regexp.MustCompile(`(?:\*\*|__)(.+?)(?:\*\*|__)`)
	reDoubleBold     = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reDoubleUnderline = regexp.MustCompile(`__(.+?)__`)
	reItalic         = regexp.MustCompile(`(?:\*|_)(.+?)(?:\*|_)`)
	reCodeBlock      = regexp.MustCompile("(?s)```(?:[a-z]*)?\n?(.+?)```")
	reInlineCode     = regexp.MustCompile("`(.+?)`")
	reHeader         = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	reLink           = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	reImage          = regexp.MustCompile(`!\[[^\]]*\]\([^)]+\)`)
)
