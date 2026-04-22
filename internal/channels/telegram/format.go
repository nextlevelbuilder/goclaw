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
	// ${1}_ (not $1_) — Go regexp treats $1_ as a named group reference (identifier
	// characters include `_`), which drops the capture. Curly braces delimit the group.
	{regexp.MustCompile(`(?i)<i>([\s\S]*?)</i>`), "_${1}_"},
	{regexp.MustCompile(`(?i)<em>([\s\S]*?)</em>`), "_${1}_"},
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

	// Balance unclosed bold/italic markers BEFORE formatting. When the LLM gets
	// cut off mid-bold (max_tokens truncation, finish_reason=length, stream
	// terminate), the raw content ends with "... : **70" and the non-greedy
	// regex below silently passes it through as literal `**`. Users then see
	// `• Anh thực nhận: **70` on Telegram. Stripping the lone marker is
	// cleaner than appending a closing one (which would bold a partial word).
	text = balanceMarkdownMarker(text, "**")
	text = balanceMarkdownMarker(text, "__")

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

	return balanceChunkTags(chunks)
}

// balanceMarkdownMarker strips the last unclosed marker pair (e.g. "**" or "__").
// Telegram's non-greedy bold regex silently drops unmatched markers, so the
// literal "**" leaks through to the rendered message. When the LLM is cut off
// mid-bold, we'd rather lose the bold styling than render a literal "**".
func balanceMarkdownMarker(text, marker string) string {
	if !strings.Contains(text, marker) {
		return text
	}
	if strings.Count(text, marker)%2 == 0 {
		return text
	}
	last := strings.LastIndex(text, marker)
	if last < 0 {
		return text
	}
	return text[:last] + text[last+len(marker):]
}

// inlineFormattingTags enumerates Telegram HTML tags that can legitimately span
// cross-chunk if the content is long. We close them at the end of the chunk
// and re-open them at the start of the next so each chunk is a valid Telegram
// HTML fragment.
var inlineFormattingTags = []string{"b", "i", "u", "s", "code"}

// balanceChunkTags closes any still-open inline formatting tags at the end of
// each chunk and re-opens them at the start of the next. Without this, a
// paragraph like `<b>foo\n\nbar</b>` split at `\n\n` would produce chunk 1
// `<b>foo` (unclosed, Telegram rejects) and chunk 2 `bar</b>` (orphan close).
func balanceChunkTags(chunks []string) []string {
	if len(chunks) <= 1 {
		return chunks
	}
	out := make([]string, 0, len(chunks))
	var carry []string // tag names still open, to re-open at start of next chunk
	for _, chunk := range chunks {
		if len(carry) > 0 {
			var b strings.Builder
			for _, name := range carry {
				b.WriteString("<")
				b.WriteString(name)
				b.WriteString(">")
			}
			b.WriteString(chunk)
			chunk = b.String()
		}
		closed, stillOpen := closeUnclosedInlineTags(chunk)
		out = append(out, closed)
		carry = stillOpen
	}
	return out
}

// closeUnclosedInlineTags scans chunk, appends close tags for any unclosed
// inline formatting tag, and returns the list of tag names that were unclosed
// (so they can be re-opened in the next chunk). Tags with attributes (like
// `<a href>`) and non-inline tags (`<pre>`, `<code class=...>`) are ignored —
// the chunkHTML boundary logic already avoids splitting inside a tag.
func closeUnclosedInlineTags(chunk string) (string, []string) {
	var stack []string
	i := 0
	for i < len(chunk) {
		lt := strings.IndexByte(chunk[i:], '<')
		if lt < 0 {
			break
		}
		i += lt
		gt := strings.IndexByte(chunk[i:], '>')
		if gt < 0 {
			break
		}
		raw := chunk[i+1 : i+gt]
		i += gt + 1
		isClose := strings.HasPrefix(raw, "/")
		if isClose {
			raw = raw[1:]
		}
		// Tag name is everything up to the first space (strips attrs on opens).
		name := strings.ToLower(strings.TrimSpace(raw))
		if sp := strings.IndexByte(name, ' '); sp >= 0 {
			name = name[:sp]
		}
		if !isInlineFormattingTag(name) {
			continue
		}
		if isClose {
			// Pop the most recent matching open from the stack.
			for j := len(stack) - 1; j >= 0; j-- {
				if stack[j] == name {
					stack = append(stack[:j], stack[j+1:]...)
					break
				}
			}
		} else {
			stack = append(stack, name)
		}
	}
	if len(stack) == 0 {
		return chunk, nil
	}
	var b strings.Builder
	b.WriteString(chunk)
	// Close in reverse order to maintain proper nesting.
	for j := len(stack) - 1; j >= 0; j-- {
		b.WriteString("</")
		b.WriteString(stack[j])
		b.WriteString(">")
	}
	// Reopen list preserves original nesting order.
	return b.String(), append([]string(nil), stack...)
}

func isInlineFormattingTag(name string) bool {
	for _, t := range inlineFormattingTags {
		if t == name {
			return true
		}
	}
	return false
}
