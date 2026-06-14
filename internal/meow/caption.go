package meow

import (
	"html"
	"strings"
	"unicode/utf16"
)

// TelegramCaptionLimit is the max photo-caption length Telegram accepts,
// measured in UTF-16 code units (not bytes or runes).
const TelegramCaptionLimit = 1024

// TelegramMessageLimit is the max length of a follow-up text message.
const TelegramMessageLimit = 4096

// CaptionDivider separates the KO block from the EN block.
const CaptionDivider = "─────"

// captionLen counts s in UTF-16 code units, matching how Telegram measures
// caption/message length (BMP chars = 1, most emoji = 2).
func captionLen(s string) int {
	return len(utf16.Encode([]rune(s)))
}

// BuildCaption stacks the KO block over the EN block with a divider and
// HTML-escapes each block so draft text cannot inject Telegram HTML markup.
// The result is intended for ParseMode=HTML.
func BuildCaption(ko, en string) string {
	ko = strings.TrimSpace(ko)
	en = strings.TrimSpace(en)
	var b strings.Builder
	if ko != "" {
		b.WriteString(html.EscapeString(ko))
	}
	if ko != "" && en != "" {
		b.WriteString("\n" + CaptionDivider + "\n")
	}
	if en != "" {
		b.WriteString(html.EscapeString(en))
	}
	return b.String()
}

// SplitForPhoto returns the photo caption (≤ TelegramCaptionLimit) plus any
// overflow as follow-up message chunks (≤ TelegramMessageLimit each). Splitting
// happens on line boundaries so escaped HTML entities are never cut in half.
// When the whole caption fits, rest is nil.
func SplitForPhoto(caption string) (first string, rest []string) {
	if captionLen(caption) <= TelegramCaptionLimit {
		return caption, nil
	}
	lines := strings.Split(caption, "\n")
	first = packLines(&lines, TelegramCaptionLimit)
	for len(lines) > 0 {
		rest = append(rest, packLines(&lines, TelegramMessageLimit))
	}
	return first, rest
}

// packLines greedily consumes leading lines from *lines into one chunk that
// stays within limit (UTF-16 units). A single oversized line is hard-split by
// UTF-16 code units as a last resort.
func packLines(lines *[]string, limit int) string {
	var b strings.Builder
	for len(*lines) > 0 {
		line := (*lines)[0]
		add := line
		if b.Len() > 0 {
			add = "\n" + line
		}
		if captionLen(b.String())+captionLen(add) > limit {
			if b.Len() == 0 {
				// Single line longer than the limit — hard-split it.
				head, tail := splitUTF16(line, limit)
				(*lines)[0] = tail
				return head
			}
			break
		}
		b.WriteString(add)
		*lines = (*lines)[1:]
	}
	return b.String()
}

// splitUTF16 splits s at limit UTF-16 code units, returning head (≤limit) and
// the remainder. It never breaks a surrogate pair, and never cuts through an
// HTML entity (e.g. "&amp;") — a dangling entity start in head is moved to
// tail so escaped captions stay well-formed across the boundary.
func splitUTF16(s string, limit int) (head, tail string) {
	units := utf16.Encode([]rune(s))
	if len(units) <= limit {
		return s, ""
	}
	cut := limit
	// Don't split a surrogate pair.
	if cut > 0 && units[cut-1] >= 0xD800 && units[cut-1] <= 0xDBFF {
		cut--
	}
	head = string(utf16.Decode(units[:cut]))
	tail = string(utf16.Decode(units[cut:]))
	// If head ends mid-entity ("&" with no closing ";" after it), push the
	// fragment to tail. Guard amp>0 so head never becomes empty.
	if amp := strings.LastIndex(head, "&"); amp > 0 && amp > strings.LastIndex(head, ";") {
		tail = head[amp:] + tail
		head = head[:amp]
	}
	return head, tail
}
