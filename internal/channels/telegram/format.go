package telegram

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mattn/go-runewidth"
)

// --- Markdown to Telegram HTML conversion ---
// Adapted from PicoClaw's telegram.go, extended with table support (matching TS "code" mode).

// htmlTagToMarkdown converts common HTML tags in LLM output to markdown equivalents
// so they survive the escapeHTML step and get re-converted by the markdown pipeline.
var htmlToMdReplacers = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)<br\s*/?>`), "\n"},
	{regexp.MustCompile(`(?i)</?p\s*>`), "\n"},
	{regexp.MustCompile(`(?i)<b>([\s\S]*?)</b>`), "**$1**"},
	{regexp.MustCompile(`(?i)<strong>([\s\S]*?)</strong>`), "**$1**"},
	{regexp.MustCompile(`(?i)<i>([\s\S]*?)</i>`), "_$1_"},
	{regexp.MustCompile(`(?i)<em>([\s\S]*?)</em>`), "_$1_"},
	{regexp.MustCompile(`(?i)<s>([\s\S]*?)</s>`), "~~$1~~"},
	{regexp.MustCompile(`(?i)<strike>([\s\S]*?)</strike>`), "~~$1~~"},
	{regexp.MustCompile(`(?i)<del>([\s\S]*?)</del>`), "~~$1~~"},
	{regexp.MustCompile(`(?i)<code>([\s\S]*?)</code>`), "`$1`"},
	{regexp.MustCompile(`(?i)<a\s+href="([^"]+)"[^>]*>([\s\S]*?)</a>`), "[$2]($1)"},
}

func htmlTagToMarkdown(text string) string {
	for _, r := range htmlToMdReplacers {
		text = r.re.ReplaceAllString(text, r.repl)
	}
	return text
}

func markdownToTelegramHTML(text string) string {
	if text == "" {
		return ""
	}

	// Pre-process: convert any HTML tags in LLM output to markdown equivalents.
	// LLMs sometimes output raw HTML (e.g. <b>bold</b>) which would get escaped
	// by escapeHTML() and displayed as literal "<b>bold</b>" text.
	text = htmlTagToMarkdown(text)

	// Extract markdown tables FIRST — uses dedicated \x00TB placeholders.
	// Tables render as <pre> (monospace block) WITHOUT <code> wrapper,
	// so Telegram shows them as preformatted text, not as "code" with copy button.
	tables := extractMarkdownTables(text)
	text = tables.text

	// Extract and protect code blocks
	codeBlocks := extractCodeBlocks(text)
	text = codeBlocks.text

	// Extract and protect inline code
	inlineCodes := extractInlineCodes(text)
	text = inlineCodes.text


	// Extract and protect bare URLs from italic parsing.
	// URLs with underscores (e.g. syngas_dailymail_2026_ai) get broken by
	// the italic regex which matches _text_ patterns inside URLs.
	var urlPlaceholders []string
	reURL := regexp.MustCompile(`https?://[^\s<>\)\]]+`)
	text = reURL.ReplaceAllStringFunc(text, func(s string) string {
		idx := len(urlPlaceholders)
		urlPlaceholders = append(urlPlaceholders, s)
		return fmt.Sprintf("\x00URL%d\x00", idx)
	})
	// Strip markdown headers
	text = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`).ReplaceAllString(text, "$1")

	// Strip blockquotes
	text = regexp.MustCompile(`(?m)^>\s*(.*)$`).ReplaceAllString(text, "$1")

	// Escape HTML
	text = escapeHTML(text)

	// Convert markdown links
	text = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`).ReplaceAllString(text, `<a href="$2">$1</a>`)

	// Bold
	text = regexp.MustCompile(`\*\*(.+?)\*\*`).ReplaceAllString(text, "<b>$1</b>")
	text = regexp.MustCompile(`__(.+?)__`).ReplaceAllString(text, "<b>$1</b>")

	// Protect @mentions from italic conversion and convert to clickable Telegram links.
	// In HTML parse_mode, Telegram does NOT auto-link @username — we must use <a> tags.
	// Uses (^|\W) prefix to avoid matching emails like user@domain.com.
	var mentionPlaceholders []string
	reMention := regexp.MustCompile(`(^|\W)(@\w+)`)
	text = reMention.ReplaceAllStringFunc(text, func(s string) string {
		match := reMention.FindStringSubmatch(s)
		if len(match) < 3 {
			return s
		}
		idx := len(mentionPlaceholders)
		mentionPlaceholders = append(mentionPlaceholders, match[2])
		return match[1] + fmt.Sprintf("\x00MN%d\x00", idx)
	})

	// Italic
	reItalic := regexp.MustCompile(`_([^_]+)_`)
	text = reItalic.ReplaceAllStringFunc(text, func(s string) string {
		match := reItalic.FindStringSubmatch(s)
		if len(match) < 2 {
			return s
		}
		return "<i>" + match[1] + "</i>"
	})

	// Strikethrough
	text = regexp.MustCompile(`~~(.+?)~~`).ReplaceAllString(text, "<s>$1</s>")

	// Restore @mentions as plain text (protected from italic conversion above).
	// Do NOT wrap in <a href="https://t.me/..."> — LLM @mentions are not
	// necessarily Telegram usernames and auto-linking shows unwanted profile cards.
	for i, mention := range mentionPlaceholders {
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00MN%d\x00", i), mention)
	}

	// List items
	text = regexp.MustCompile(`(?m)^[-*]\s+`).ReplaceAllString(text, "• ")

	// Restore bare URLs (protected from italic parsing above).
	for i, u := range urlPlaceholders {
		escaped := escapeHTML(u)
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00URL%d\x00", i), escaped)
	}

	// Restore inline code
	for i, code := range inlineCodes.codes {
		escaped := escapeHTML(code)
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00IC%d\x00", i), fmt.Sprintf("<code>%s</code>", escaped))
	}

	// Restore code blocks (real code → <pre><code>)
	for i, code := range codeBlocks.codes {
		escaped := escapeHTML(code)
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00CB%d\x00", i), fmt.Sprintf("<pre><code>%s</code></pre>", escaped))
	}

	// Restore tables (→ <pre> only, no <code> wrapper)
	for i, table := range tables.rendered {
		escaped := escapeHTML(table)
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00TB%d\x00", i), fmt.Sprintf("<pre>%s</pre>", escaped))
	}

	return text
}

type codeBlockMatch struct {
	text  string
	codes []string
}

func extractCodeBlocks(text string) codeBlockMatch {
	re := regexp.MustCompile("```[\\w]*\\n?([\\s\\S]*?)```")
	matches := re.FindAllStringSubmatch(text, -1)

	codes := make([]string, 0, len(matches))
	for _, match := range matches {
		codes = append(codes, match[1])
	}

	i := 0
	text = re.ReplaceAllStringFunc(text, func(_ string) string {
		placeholder := fmt.Sprintf("\x00CB%d\x00", i)
		i++
		return placeholder
	})

	return codeBlockMatch{text: text, codes: codes}
}

type inlineCodeMatch struct {
	text  string
	codes []string
}

func extractInlineCodes(text string) inlineCodeMatch {
	re := regexp.MustCompile("`([^`]+)`")
	matches := re.FindAllStringSubmatch(text, -1)

	codes := make([]string, 0, len(matches))
	for _, match := range matches {
		codes = append(codes, match[1])
	}

	i := 0
	text = re.ReplaceAllStringFunc(text, func(_ string) string {
		placeholder := fmt.Sprintf("\x00IC%d\x00", i)
		i++
		return placeholder
	})

	return inlineCodeMatch{text: text, codes: codes}
}

func escapeHTML(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	return text
}

// --- Markdown table extraction and rendering ---

// tableLineRe matches a markdown table row: | col1 | col2 | ...
var tableLineRe = regexp.MustCompile(`^\s*\|.*\|\s*$`)

// tableSepRe matches a markdown table separator: |---|---|
var tableSepRe = regexp.MustCompile(`^\s*\|[\s:]*-+[\s:]*(\|[\s:]*-+[\s:]*)*\|\s*$`)

type tableMatch struct {
	text     string   // text with \x00TB0\x00 placeholders
	rendered []string // rendered ASCII tables (one per placeholder)
}

// extractMarkdownTables finds markdown tables, renders them as ASCII-aligned text,
// and replaces them with \x00TBn\x00 placeholders. Tables are restored later as
// <pre> (not <pre><code>) so Telegram shows them as preformatted text.
func extractMarkdownTables(text string) tableMatch {
	lines := strings.Split(text, "\n")
	var result []string
	var rendered []string
	idx := 0
	i := 0

	for i < len(lines) {
		// Look for table start: a table line followed by a separator line
		if i+1 < len(lines) && tableLineRe.MatchString(lines[i]) && tableSepRe.MatchString(lines[i+1]) {
			// Collect all contiguous table lines
			tableStart := i
			i++ // skip header
			i++ // skip separator
			for i < len(lines) && tableLineRe.MatchString(lines[i]) {
				i++
			}

			// Parse and render the table as ASCII-aligned text
			tableLines := lines[tableStart:i]
			rendered = append(rendered, renderTableAsCode(tableLines))
			result = append(result, fmt.Sprintf("\x00TB%d\x00", idx))
			idx++
		} else {
			result = append(result, lines[i])
			i++
		}
	}

	return tableMatch{text: strings.Join(result, "\n"), rendered: rendered}
}

// renderTableAsCode converts parsed markdown table lines into ASCII-aligned text.
// Matching TS renderTableAsCode(): calculates column widths, pads cells.
func renderTableAsCode(lines []string) string {
	if len(lines) < 2 {
		return strings.Join(lines, "\n")
	}

	// Parse all rows into cells (skip separator line at index 1)
	var rows [][]string
	for i, line := range lines {
		if i == 1 {
			continue // skip separator
		}
		rows = append(rows, parseTableRow(line))
	}

	if len(rows) == 0 {
		return ""
	}

	// Determine number of columns and max width per column
	numCols := 0
	for _, row := range rows {
		if len(row) > numCols {
			numCols = len(row)
		}
	}

	colWidths := make([]int, numCols)
	for _, row := range rows {
		for j := 0; j < numCols && j < len(row); j++ {
			w := displayWidth(row[j])
			if w > colWidths[j] {
				colWidths[j] = w
			}
		}
	}

	// Render header
	var out []string
	out = append(out, renderRow(rows[0], colWidths))

	// Render separator
	var sepParts []string
	for _, w := range colWidths {
		sepParts = append(sepParts, strings.Repeat("-", w+2))
	}
	out = append(out, "|"+strings.Join(sepParts, "|")+"|")

	// Render data rows
	for _, row := range rows[1:] {
		out = append(out, renderRow(row, colWidths))
	}

	return strings.Join(out, "\n")
}

// parseTableRow splits a markdown table row into trimmed cell strings.
// Inline markdown (bold, italic, strikethrough, code) is stripped since
// tables render inside <pre><code> where HTML tags have no effect.
func parseTableRow(line string) []string {
	line = strings.TrimSpace(line)
	// Remove leading/trailing pipes
	if strings.HasPrefix(line, "|") {
		line = line[1:]
	}
	if strings.HasSuffix(line, "|") {
		line = line[:len(line)-1]
	}

	parts := strings.Split(line, "|")
	cells := make([]string, len(parts))
	for i, p := range parts {
		cells[i] = stripInlineMarkdown(strings.TrimSpace(p))
	}
	return cells
}

// stripInlineMarkdown removes common inline markdown markers from text.
// Used for table cells that render inside code blocks where formatting has no effect.
var (
	reStripBoldAsterisks    = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reStripBoldUnderscores  = regexp.MustCompile(`__(.+?)__`)
	reStripItalicAsterisk   = regexp.MustCompile(`\*([^*]+)\*`)
	reStripItalicUnderscore = regexp.MustCompile(`_([^_]+)_`)
	reStripStrikethrough    = regexp.MustCompile(`~~(.+?)~~`)
	reStripInlineCode       = regexp.MustCompile("`([^`]+)`")
)

func stripInlineMarkdown(s string) string {
	s = reStripBoldAsterisks.ReplaceAllString(s, "$1")
	s = reStripBoldUnderscores.ReplaceAllString(s, "$1")
	s = reStripStrikethrough.ReplaceAllString(s, "$1")
	s = reStripInlineCode.ReplaceAllString(s, "$1")
	s = reStripItalicAsterisk.ReplaceAllString(s, "$1")
	s = reStripItalicUnderscore.ReplaceAllString(s, "$1")
	return s
}

// renderRow renders a single table row with padded cells.
func renderRow(cells []string, colWidths []int) string {
	var parts []string
	for j, w := range colWidths {
		cell := ""
		if j < len(cells) {
			cell = cells[j]
		}
		// Pad with spaces to align columns
		padding := max(w-displayWidth(cell), 0)
		parts = append(parts, " "+cell+strings.Repeat(" ", padding)+" ")
	}
	return "|" + strings.Join(parts, "|") + "|"
}

// displayWidth returns the display width of a string, accounting for
// East Asian wide characters (CJK), emoji, and other double-width glyphs.
// Uses go-runewidth which implements Unicode East Asian Width properly,
// unlike the naive utf8.RuneLen() approach which misclassifies Vietnamese
// diacritics (3-byte UTF-8 but single-width) as double-width.
func displayWidth(s string) int {
	return runewidth.StringWidth(s)
}

// --- Message chunking ---

// chunkHTML splits HTML text into chunks that fit within maxLen.
// Prefers splitting at paragraph boundaries (\n\n), then line boundaries (\n),
// then word boundaries (space). Matching TS chunkText() logic.
// chunkPlainText splits plain text into chunks that fit within maxLen,
// preferring to split at paragraph or line boundaries.
func chunkPlainText(text string, maxLen int) []string {
	return chunkHTML(text, maxLen)
}

func chunkHTML(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	remaining := text

	for len(remaining) > 0 {
		if len(remaining) <= maxLen {
			chunks = append(chunks, remaining)
			break
		}

		// Strategy: search backwards for best natural breakpoint within maxLen.
		cutAt := maxLen

		// 1. Look for preferred boundaries: paragraph, then newline, then space.
		if idx := strings.LastIndex(remaining[:cutAt], "\n\n"); idx > 0 {
			cutAt = idx + 2
		} else if idx := strings.LastIndex(remaining[:cutAt], "\n"); idx > 0 {
			cutAt = idx + 1
		} else if idx := strings.LastIndex(remaining[:cutAt], " "); idx > 0 {
			cutAt = idx + 1
		}

		// 2. Safety: ensure we don't cut in the middle of an HTML tag or entity.
		// Tag check: find last '<' and see if it was closed before cutAt.
		if lastOpen := strings.LastIndex(remaining[:cutAt], "<"); lastOpen != -1 {
			lastClose := strings.LastIndex(remaining[:cutAt], ">")
			if lastOpen > lastClose {
				// We're inside a tag (e.g. "<a hre"). Move cutAt back to start of tag.
				// This ensures the tag remains whole in the next chunk.
				cutAt = lastOpen
			}
		}

		// Entity check: find last '&' and see if it was closed before cutAt.
		if lastOpen := strings.LastIndex(remaining[:cutAt], "&"); lastOpen != -1 {
			lastClose := strings.LastIndex(remaining[:cutAt], ";")
			if lastOpen > lastClose {
				// Inside an entity (e.g. "&am"). Move cutAt back to start of entity.
				cutAt = lastOpen
			}
		}

		// 3. Fallback for monolithic blocks: if boundaries or safety moved cutAt to 0,
		// force progress by using maxLen anyway. This avoids infinite loops.
		if cutAt <= 0 {
			cutAt = maxLen
		}

		chunks = append(chunks, strings.TrimRight(remaining[:cutAt], " \n"))
		remaining = strings.TrimLeft(remaining[cutAt:], " \n")
	}

	return chunks
}
