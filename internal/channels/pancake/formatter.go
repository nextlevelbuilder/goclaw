package pancake

import (
	"log/slog"
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
	case "tiktok", "shopee":
		return stripMarkdown(truncateRuneSafe(content, 500))
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

// truncateRuneSafe truncates content to `limit` runes, avoiding multi-byte
// UTF-8 corruption (CJK, Vietnamese, emoji). Used by platforms with short
// DM limits (TikTok, Shopee: 500 runes). Logs a warning when truncation
// occurs so the user isn't silently trimmed (M7).
func truncateRuneSafe(content string, limit int) string {
	runes := []rune(content)
	if len(runes) <= limit {
		return content
	}
	slog.Warn("pancake: message truncated",
		"orig_runes", len(runes),
		"limit", limit)
	if limit <= 3 {
		return string(runes[:limit])
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
