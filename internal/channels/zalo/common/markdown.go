// Package common holds shared building blocks used by Zalo channel flavors.
package common

import (
	"regexp"
	"strings"
)

// StripMarkdown returns plain text with markdown artifacts removed —
// Zalo does not support any markup rendering.
//
// Semantics are dewee's original zalo.StripMarkdown, extended with GFM
// table rendering (tables become labeled bullet blocks). The reference
// implementation's dunder preservation (stripBoldUnder) is intentionally
// NOT ported: single-word __bold__ must keep stripping (existing pinned
// tests), and dunder identifiers inside fenced code blocks are already
// handled by reFencedCode.
func StripMarkdown(text string) string {
	if text == "" {
		return text
	}

	text = renderMarkdownTables(text)
	text = reFencedCode.ReplaceAllString(text, "$1")
	text = reInlineCode.ReplaceAllString(text, "$1")
	text = reImage.ReplaceAllString(text, "")
	text = reLink.ReplaceAllString(text, "$1 ($2)")
	text = reBoldItalicStar.ReplaceAllString(text, "$1")
	text = reBoldItalicUnder.ReplaceAllString(text, "$1")
	text = reBoldStar.ReplaceAllString(text, "$1")
	text = reBoldUnder.ReplaceAllString(text, "$1")
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
)

// renderMarkdownTables rewrites GFM-style tables into bullet blocks so Zalo
// (no monospace, no table support) shows labeled rows instead of raw pipes.
func renderMarkdownTables(text string) string {
	if !strings.Contains(text, "|") {
		return text
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		if i+1 < len(lines) && isTableRow(lines[i]) && isTableSeparatorRow(lines[i+1]) {
			headers := parseTableRow(lines[i])
			j := i + 2
			for j < len(lines) && isTableRow(lines[j]) {
				j++
			}
			if rendered := formatTableAsBlocks(headers, lines[i+2:j]); rendered != "" {
				out = append(out, rendered)
			}
			i = j - 1
			continue
		}
		out = append(out, lines[i])
	}
	return strings.Join(out, "\n")
}

func isTableRow(line string) bool {
	s := strings.TrimSpace(line)
	return strings.HasPrefix(s, "|") && strings.HasSuffix(s, "|") && strings.Count(s, "|") >= 2
}

func isTableSeparatorRow(line string) bool {
	if !isTableRow(line) {
		return false
	}
	s := strings.Trim(strings.TrimSpace(line), "|")
	sawDash := false
	for _, ch := range s {
		switch ch {
		case '-':
			sawDash = true
		case ':', '|', ' ', '\t':
		default:
			return false
		}
	}
	return sawDash
}

func parseTableRow(line string) []string {
	s := strings.Trim(strings.TrimSpace(line), "|")
	cells := strings.Split(s, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

func formatTableAsBlocks(headers, dataRows []string) string {
	var b strings.Builder
	first := true
	for _, row := range dataRows {
		cells := parseTableRow(row)
		if len(cells) == 0 || (len(cells) == 1 && cells[0] == "") {
			continue
		}
		if !first {
			b.WriteString("\n")
		}
		first = false
		b.WriteString("• ")
		b.WriteString(cells[0])
		for k := 1; k < len(cells); k++ {
			v := cells[k]
			if v == "" {
				continue
			}
			b.WriteString("\n  ")
			if k < len(headers) && headers[k] != "" {
				b.WriteString(headers[k])
				b.WriteString(": ")
			}
			b.WriteString(v)
		}
	}
	return b.String()
}
