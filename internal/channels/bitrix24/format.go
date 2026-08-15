package bitrix24

import (
	"fmt"
	"regexp"
	"strings"
)

// --- Markdown → Bitrix24 BBCode conversion ---
//
// Bitrix24 chat (imbot.message.add / im.message.add) renders a restricted BBCode
// subset. LLMs emit Markdown by default, so raw ** and __ and ``` would surface
// as literal characters in the Bitrix chat bubble. This file converts Markdown
// to BBCode before handing the text to sendChunk.
//
// Supported Bitrix24 BBCode tags (confirmed against imbot messages):
//   [b]…[/b]            bold
//   [i]…[/i]            italic + inline tokens (markdown * / `…` / <code>, and LLM one-line [code]…[/code] in prose)
//   [u]…[/u]            underline
//   [s]…[/s]            strikethrough
//   [code]…[/code]      block code only (markdown ``` fences, or LLM [code] with newline after tag / multiline body)
//   [url=link]text[/url] named hyperlink
//   [url]link[/url]     bare hyperlink
//   [quote]…[/quote]    quote block
//
// NOT supported natively by Bitrix as Markdown: headers, tables, lists. These
// are adapted (headers → [b], lists → • bullets, pipe tables → labeled bullet
// blocks — Bitrix chat does not render [table]/[tr]/[td] BBCode).
//
// Code policy: ``` → [code]…[/code]; one-line [code]x[/code] in text → [i]x[/i] (see bxNormalizeLLMInlineCodeBBCodeSpans).
//
// Deliberate non-goals:
//   - [USER=id] mentions: LLM output never carries stable numeric IDs.
//   - [DISK=id] attachments: media goes through Phase 06, not text formatting.
//   - Colors / fonts / sizes: over-styling distracts from bot replies.

// bxLLMCodeSpanBBCodeRE matches LLM-emitted [code]…[/code] spans (any case).
var bxLLMCodeSpanBBCodeRE = regexp.MustCompile(`(?i)\[code\]([\s\S]*?)\[/code\]`)

// bxInboundUserMentionRE matches Bitrix24 `[USER=<id>]Display Name[/USER]` /
// `[BOT=<id>]…[/BOT]` mention tags emitted on group-chat webhooks via the
// MESSAGE_ORIGINAL field. The name body uses non-greedy match across multiple
// runes (no nested `[USER=`), stops at the closing tag. Mismatched opener/closer
// (e.g. `[USER=…][/BOT]`) is tolerated — some Bitrix clients mix them.
var bxInboundUserMentionRE = regexp.MustCompile(`(?s)\[(USER|BOT)=(\d+)\](.*?)\[/(?:USER|BOT)\]`)

// bxConvertUserMentionsToReadable rewrites inbound Bitrix24 BBCode mentions
// into an LLM-readable `@Name (ID:<id>)` form. The agent loop sees plain text,
// so leaving raw `[USER=62]Đặng Văn Tình[/USER]` in the prompt costs tokens and
// confuses retrieval / summarization. The "(ID:<id>)" annotation preserves the
// numeric identity in case the agent needs to mention the same user back —
// outbound formatting (see markdownToBitrixBBCode) does not synthesise mention
// BBCode, but downstream tools (MCP, future explicit mention support) can
// recover the id without a separate metadata channel.
//
// Empty display name (rare but observed when Bitrix sends `[USER=62][/USER]`)
// falls back to "@user-<id>" so the mention is still visible. Caller is
// responsible for stripping the bot's own mention BEFORE invoking this helper —
// otherwise the bot will see itself referenced and may reply to itself.
func bxConvertUserMentionsToReadable(text string) string {
	if text == "" || !strings.Contains(text, "[") {
		return text
	}
	return bxInboundUserMentionRE.ReplaceAllStringFunc(text, func(match string) string {
		m := bxInboundUserMentionRE.FindStringSubmatch(match)
		if len(m) < 4 {
			return match
		}
		id := m[2]
		name := strings.TrimSpace(m[3])
		if name == "" {
			return "@user-" + id
		}
		return "@" + name + " (ID:" + id + ")"
	})
}

// bxAfterLLMCodeOpenRE is true when the opening [code] tag is immediately
// followed by a line break (block / fenced-style BBCode from the model).
var bxAfterLLMCodeOpenRE = regexp.MustCompile(`(?i)^\[code\]\s*\r?\n`)

// bxNormalizeLLMInlineCodeBBCodeSpans turns one-line [code]x[/code] in prose
// into [i]x[/i]. Keeps [code] when the opening tag is followed by a newline
// or the inner text spans multiple lines (real snippets / JSON blocks).
func bxNormalizeLLMInlineCodeBBCodeSpans(text string) string {
	return bxLLMCodeSpanBBCodeRE.ReplaceAllStringFunc(text, func(full string) string {
		if bxAfterLLMCodeOpenRE.MatchString(full) {
			return full
		}
		m := bxLLMCodeSpanBBCodeRE.FindStringSubmatch(full)
		if len(m) < 2 {
			return full
		}
		inner := m[1]
		if strings.Contains(inner, "\n") || strings.Contains(inner, "\r") {
			return full
		}
		return "[i]" + strings.TrimSpace(inner) + "[/i]"
	})
}

// markdownToBitrixBBCode converts Markdown-formatted text (as emitted by the
// LLM) to the BBCode subset Bitrix24 chat renders. Pure function; safe to call
// on empty string. Preserves code block contents verbatim.
func markdownToBitrixBBCode(text string) string {
	if text == "" {
		return ""
	}

	// Sanitize NUL: we use \x00…\x00 framing for placeholders (CB/TB/IC).
	// If the input happens to carry a literal NUL (rare but possible from
	// mangled LLM output or binary-contaminated payloads) our placeholder
	// scheme would collide and corrupt restoration. Strip before anything.
	if strings.ContainsRune(text, 0) {
		text = strings.ReplaceAll(text, "\x00", "")
	}

	// Pre-process: LLMs sometimes emit raw HTML (e.g. <b>). Convert those
	// first so the Markdown → BBCode path handles them uniformly.
	text = bxHTMLToMarkdown(text)

	// Extract fenced code blocks FIRST, before any other regex runs. Code
	// contents must not be reinterpreted as Markdown (** inside code is
	// literal). Placeholders `\x00CB{i}\x00` are restored at the end as
	// [code]…[/code].
	fenced := bxExtractFencedCode(text)
	text = fenced.text

	// Extract Markdown pipe tables (bordered or borderless) and replace with
	// placeholders `\x00TB{i}\x00` for Bitrix-friendly rendering at restore time.
	tables := bxExtractTables(text)
	text = tables.text

	// Extract inline code spans next so backticks inside don't get matched
	// as italic/bold markers. Placeholders `\x00IC{i}\x00`.
	inline := bxExtractInlineCode(text)
	text = inline.text

	// Headers (#, ##, ###, …) → [b]text[/b] on their own line. Bitrix has
	// no header concept; bolding + line break is the closest visual.
	text = regexp.MustCompile(`(?m)^#{1,6}\s+(.+?)\s*$`).ReplaceAllString(text, "[b]$1[/b]")

	// Blockquotes: strip leading `> ` on each line, wrap the consecutive
	// block in [quote]…[/quote]. We do a simple pass: lines starting with
	// `>` turn into a marker, then collapse runs.
	text = bxWrapBlockquotes(text)

	// Links: [text](url) → [url=url]text[/url]. Skip image syntax ![…](…)
	// — Bitrix doesn't render inline images from URLs, and sending the
	// alt+URL as a named link is the least-confusing fallback.
	text = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`).ReplaceAllString(text, "[url=$2]$1[/url]")
	text = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`).ReplaceAllString(text, "[url=$2]$1[/url]")

	// Bold: **text** or __text__ → [b]text[/b]
	text = regexp.MustCompile(`\*\*(.+?)\*\*`).ReplaceAllString(text, "[b]$1[/b]")
	text = regexp.MustCompile(`__(.+?)__`).ReplaceAllString(text, "[b]$1[/b]")

	// Italic: *text* or _text_ → [i]text[/i]
	// Guard against intra-word underscores (snake_case identifiers) — only
	// match _text_ when flanked by non-word or string boundary. Markdown
	// itself skips intra-word underscores, so this matches expectation.
	//
	// NB: the regex consumes one flanking char on each side for the
	// non-intra-word assertion. That advances the scan past the separator,
	// so a second pair touching the first via only the eaten char
	// (e.g. "*a* *b*") is missed in a single pass. RE2 has no lookaround,
	// so we loop until stable — pairs strictly decrease each iteration,
	// convergence is bounded by input length.
	italicStar := regexp.MustCompile(`(^|[^\w*])\*([^*\n]+?)\*([^\w*]|$)`)
	italicUnder := regexp.MustCompile(`(^|[^\w_])_([^_\n]+?)_([^\w_]|$)`)
	for i := 0; i < 8; i++ {
		prev := text
		text = italicStar.ReplaceAllString(text, "$1[i]$2[/i]$3")
		text = italicUnder.ReplaceAllString(text, "$1[i]$2[/i]$3")
		if text == prev {
			break
		}
	}

	// Strikethrough: ~~text~~ → [s]text[/s]
	text = regexp.MustCompile(`~~(.+?)~~`).ReplaceAllString(text, "[s]$1[/s]")

	// Unordered list marker: `- item` / `* item` / `+ item` → `• item`
	// Bitrix has no list BBCode for imbot messages; a bullet char is
	// unambiguous and works in both DM and group renders.
	text = regexp.MustCompile(`(?m)^[\s]*[-*+]\s+`).ReplaceAllString(text, "• ")

	// Ordered list: keep `1. item` as-is — Bitrix renders numerals fine.

	// Horizontal rule: ---, ***, ___ on their own line → a divider line of
	// dashes (Bitrix has no [hr] equivalent).
	text = regexp.MustCompile(`(?m)^[\s]*(?:-{3,}|\*{3,}|_{3,})[\s]*$`).ReplaceAllString(text, "────────")

	// Restore inline code spans as [i]…[/i] (Bitrix: prose identifiers; fenced
	// blocks still use [code] below).
	for i, code := range inline.codes {
		text = strings.ReplaceAll(text,
			fmt.Sprintf("\x00IC%d\x00", i),
			"[i]"+code+"[/i]")
	}

	// Restore tables as labeled bullet blocks (Bitrix does not render [table]).
	// Malformed markdown tables use a plain aligned grid fallback.
	for i, tbl := range tables.blocks {
		tbl = bxRenderMarkdownTableToBBCode(tbl)
		text = strings.ReplaceAll(text,
			fmt.Sprintf("\x00TB%d\x00", i),
			tbl)
	}

	// Restore fenced code blocks last so their contents are completely
	// untouched by upstream regex passes.
	for i, code := range fenced.codes {
		code = strings.TrimRight(code, "\n")
		text = strings.ReplaceAll(text,
			fmt.Sprintf("\x00CB%d\x00", i),
			"[code]\n"+code+"\n[/code]")
	}

	// Collapse 3+ blank lines to 2 (LLM sometimes over-paragraphs).
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")

	text = bxNormalizeLLMInlineCodeBBCodeSpans(text)

	return strings.TrimSpace(text)
}

// bxWrapBlockquotes groups consecutive `> ` prefixed lines into a single
// [quote]…[/quote] block and strips the markers. Non-blockquote lines pass
// through unchanged.
func bxWrapBlockquotes(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	var buf []string
	flush := func() {
		if len(buf) == 0 {
			return
		}
		out = append(out, "[quote]"+strings.Join(buf, "\n")+"[/quote]")
		buf = buf[:0]
	}
	bqLine := regexp.MustCompile(`^\s*>\s?(.*)$`)
	for _, line := range lines {
		if m := bqLine.FindStringSubmatch(line); m != nil {
			buf = append(buf, m[1])
			continue
		}
		flush()
		out = append(out, line)
	}
	flush()
	return strings.Join(out, "\n")
}

// bxHTMLToMarkdown normalises common HTML emitted by LLMs into Markdown so
// the Markdown → BBCode pipeline handles it uniformly. Conservative: only
// covers the tags LLMs actually emit in practice.
var bxHTMLToMarkdownReplacers = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)<br\s*/?>`), "\n"},
	{regexp.MustCompile(`(?i)</?p\s*>`), "\n"},
	{regexp.MustCompile(`(?i)<b>([\s\S]*?)</b>`), "**${1}**"},
	{regexp.MustCompile(`(?i)<strong>([\s\S]*?)</strong>`), "**${1}**"},
	{regexp.MustCompile(`(?i)<i>([\s\S]*?)</i>`), "_${1}_"},
	{regexp.MustCompile(`(?i)<em>([\s\S]*?)</em>`), "_${1}_"},
	{regexp.MustCompile(`(?i)<s>([\s\S]*?)</s>`), "~~${1}~~"},
	{regexp.MustCompile(`(?i)<strike>([\s\S]*?)</strike>`), "~~${1}~~"},
	{regexp.MustCompile(`(?i)<del>([\s\S]*?)</del>`), "~~${1}~~"},
	// Normalise <code> to backticks so inline extraction renders as [i]…[/i].
	{regexp.MustCompile(`(?i)<code>([\s\S]*?)</code>`), "`${1}`"},
	{regexp.MustCompile(`(?i)<a\s+href="([^"]+)"[^>]*>([\s\S]*?)</a>`), "[${2}](${1})"},
}

func bxHTMLToMarkdown(text string) string {
	for _, r := range bxHTMLToMarkdownReplacers {
		text = r.re.ReplaceAllString(text, r.repl)
	}
	return text
}

// bxExtractedBlocks holds the stripped text plus the captured contents, to be
// stitched back together after the main Markdown → BBCode pass.
type bxExtractedBlocks struct {
	text  string
	codes []string
}

// bxExtractFencedCode pulls ```lang\n…``` blocks out of text and replaces each
// with a `\x00CB{i}\x00` placeholder. The language hint is discarded — Bitrix
// has no syntax highlighting, so it would only add noise.
//
// The prefix group `(?:[\w+.-]+\n|\n)?` covers three shapes without letting a
// single-line “ ```code``` “ mis-parse `code` as a lang hint:
//   - ```py\n…\n```     lang hint consumed with its trailing newline
//   - ```\n…\n```       bare newline after the fence
//   - ```code```        no prefix → content capture wins, `code` is content
func bxExtractFencedCode(text string) bxExtractedBlocks {
	re := regexp.MustCompile("```(?:[\\w+.-]+\\n|\\n)?([\\s\\S]*?)```")
	var codes []string
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		codes = append(codes, m[1])
	}
	i := 0
	text = re.ReplaceAllStringFunc(text, func(_ string) string {
		p := fmt.Sprintf("\x00CB%d\x00", i)
		i++
		return p
	})
	return bxExtractedBlocks{text: text, codes: codes}
}

// bxExtractInlineCode pulls `code` spans out of text, leaving
// `\x00IC{i}\x00` placeholders. Runs AFTER fenced extraction so single
// backticks inside fenced blocks are not disturbed.
func bxExtractInlineCode(text string) bxExtractedBlocks {
	// Single-backtick span. Double-backtick `` … `` is rare in LLM output;
	// handled by the same regex because the inner group is non-greedy.
	re := regexp.MustCompile("`([^`\\n]+?)`")
	var codes []string
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		codes = append(codes, m[1])
	}
	i := 0
	text = re.ReplaceAllStringFunc(text, func(_ string) string {
		p := fmt.Sprintf("\x00IC%d\x00", i)
		i++
		return p
	})
	return bxExtractedBlocks{text: text, codes: codes}
}

// bxExtractedTables is a named alias so the block restoration loop stays
// readable alongside code restoration.
type bxExtractedTables struct {
	text   string
	blocks []string
}

// bxExtractTables detects GitHub-style Markdown tables (header row + separator
// row + 1+ body rows) and replaces each with a `\x00TB{i}\x00` placeholder.
// Rows may be "bordered" (start with |) or "borderless" (no leading pipe) as
// long as cells are pipe-delimited and the separator row validates.
func bxExtractTables(text string) bxExtractedTables {
	lines := strings.Split(text, "\n")
	var blocks []string
	var out []string
	i := 0
	for i < len(lines) {
		block, end := bxExtractOneMarkdownTable(lines, i)
		if block != "" {
			blocks = append(blocks, block)
			out = append(out, fmt.Sprintf("\x00TB%d\x00", len(blocks)-1))
			i = end
			continue
		}
		out = append(out, lines[i])
		i++
	}
	joined := strings.Join(out, "\n")
	return bxExtractedTables{text: joined, blocks: blocks}
}

// bxExtractOneMarkdownTable returns a markdown table block starting at start,
// and the index of the first line after the table. If no table starts here,
// returns ("", start+1).
func bxExtractOneMarkdownTable(lines []string, start int) (block string, next int) {
	if start+2 >= len(lines) {
		return "", start + 1
	}
	hdr := strings.TrimSpace(lines[start])
	sep := strings.TrimSpace(lines[start+1])
	if hdr == "" || sep == "" {
		return "", start + 1
	}
	hCells := bxSplitTableRow(hdr)
	if len(hCells) < 1 {
		return "", start + 1
	}
	sCells := bxSplitTableRow(sep)
	if len(sCells) != len(hCells) || !bxIsSeparatorRow(sCells) {
		return "", start + 1
	}
	body0 := strings.TrimSpace(lines[start+2])
	if body0 == "" || !strings.Contains(body0, "|") {
		return "", start + 1
	}
	b0Cells := bxSplitTableRow(body0)
	if len(b0Cells) < 1 {
		return "", start + 1
	}
	end := start + 2
	for end+1 < len(lines) {
		nl := strings.TrimSpace(lines[end+1])
		if nl == "" {
			break
		}
		if !strings.Contains(nl, "|") {
			break
		}
		nextCells := bxSplitTableRow(nl)
		if len(nextCells) == len(hCells) && bxIsSeparatorRow(nextCells) {
			break
		}
		end++
	}
	var b strings.Builder
	for j := start; j <= end; j++ {
		if j > start {
			b.WriteByte('\n')
		}
		b.WriteString(lines[j])
	}
	return b.String(), end + 1
}

func bxRenderMarkdownTableToBBCode(raw string) string {
	header, rows, ok := bxParseMarkdownTable(raw)
	if !ok {
		return bxRenderMarkdownTableFallback(raw)
	}
	return bxRenderMarkdownTableAsLabeledBullets(header, rows)
}

// bxRenderMarkdownTableAsLabeledBullets turns a parsed pipe table into plain
// lines Bitrix24 chat can read: each body row becomes one record; the first
// field starts with "•", continuation fields with "—", each line
// "[b]Header[/b]: value".
func bxRenderMarkdownTableAsLabeledBullets(header []string, rows [][]string) string {
	var b strings.Builder
	for _, row := range rows {
		for ci, h := range header {
			label := bxRenderTableCellMarkdown(h)
			if label == "" {
				label = " "
			}
			val := ""
			if ci < len(row) {
				val = bxRenderTableCellMarkdown(row[ci])
			}
			if strings.TrimSpace(val) == "" {
				val = " "
			}
			prefix := "• "
			if ci > 0 {
				prefix = "— "
			}
			b.WriteString(prefix)
			b.WriteString("[b]")
			b.WriteString(label)
			b.WriteString("[/b]: ")
			b.WriteString(val)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func bxParseMarkdownTable(raw string) ([]string, [][]string, bool) {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) < 3 {
		return nil, nil, false
	}

	header := bxSplitTableRow(lines[0])
	sep := bxSplitTableRow(lines[1])
	if len(header) == 0 || len(sep) != len(header) || !bxIsSeparatorRow(sep) {
		return nil, nil, false
	}

	rows := make([][]string, 0, len(lines)-2)
	for _, line := range lines[2:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		row := bxSplitTableRow(line)
		if len(row) == 0 {
			continue
		}
		if len(row) < len(header) {
			row = append(row, make([]string, len(header)-len(row))...)
		}
		if len(row) > len(header) {
			row = row[:len(header)]
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, nil, false
	}
	return header, rows, true
}

func bxSplitTableRow(row string) []string {
	row = strings.TrimSpace(row)
	if row == "" {
		return nil
	}

	var out []string
	var cell strings.Builder
	escaped := false
	for _, r := range row {
		if escaped {
			cell.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '|' {
			out = append(out, strings.TrimSpace(cell.String()))
			cell.Reset()
			continue
		}
		cell.WriteRune(r)
	}
	if escaped {
		cell.WriteRune('\\')
	}
	out = append(out, strings.TrimSpace(cell.String()))

	// Drop boundary empties for canonical "| a | b |" rows.
	if len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	if len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

func bxIsSeparatorRow(cells []string) bool {
	sepRe := regexp.MustCompile(`^:?-{3,}:?$`)
	for _, c := range cells {
		c = strings.ReplaceAll(strings.TrimSpace(c), " ", "")
		if !sepRe.MatchString(c) {
			return false
		}
	}
	return true
}

func bxRenderTableCellMarkdown(cell string) string {
	cell = strings.TrimSpace(cell)
	if cell == "" {
		return " "
	}
	cell = bxHTMLToMarkdown(cell)
	cell = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`).ReplaceAllString(cell, "[url=$2]$1[/url]")
	cell = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`).ReplaceAllString(cell, "[url=$2]$1[/url]")
	cell = regexp.MustCompile(`\*\*(.+?)\*\*`).ReplaceAllString(cell, "[b]$1[/b]")
	cell = regexp.MustCompile(`__(.+?)__`).ReplaceAllString(cell, "[b]$1[/b]")
	cell = regexp.MustCompile(`~~(.+?)~~`).ReplaceAllString(cell, "[s]$1[/s]")
	cell = regexp.MustCompile("`([^`\\n]+?)`").ReplaceAllString(cell, "[i]$1[/i]")

	italicStar := regexp.MustCompile(`(^|[^\w*])\*([^*\n]+?)\*([^\w*]|$)`)
	italicUnder := regexp.MustCompile(`(^|[^\w_])_([^_\n]+?)_([^\w_]|$)`)
	for i := 0; i < 8; i++ {
		prev := cell
		cell = italicStar.ReplaceAllString(cell, "$1[i]$2[/i]$3")
		cell = italicUnder.ReplaceAllString(cell, "$1[i]$2[/i]$3")
		if cell == prev {
			break
		}
	}
	return strings.TrimSpace(cell)
}

func bxRenderMarkdownTableFallback(raw string) string {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) == 0 {
		return ""
	}

	var rows [][]string
	for i, line := range lines {
		if i == 1 {
			// Skip markdown separator row in fallback.
			continue
		}
		cells := bxSplitTableRow(line)
		if len(cells) == 0 {
			continue
		}
		for idx, c := range cells {
			cells[idx] = bxRenderTableCellMarkdown(c)
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 {
		return strings.TrimSpace(raw)
	}

	colCount := 0
	for _, row := range rows {
		if len(row) > colCount {
			colCount = len(row)
		}
	}
	if colCount == 0 {
		return strings.TrimSpace(raw)
	}

	widths := make([]int, colCount)
	for _, row := range rows {
		for i := 0; i < colCount; i++ {
			val := ""
			if i < len(row) {
				val = row[i]
			}
			if l := len([]rune(val)); l > widths[i] {
				widths[i] = l
			}
		}
	}

	renderRow := func(row []string) string {
		parts := make([]string, colCount)
		for i := 0; i < colCount; i++ {
			val := ""
			if i < len(row) {
				val = row[i]
			}
			pad := widths[i] - len([]rune(val))
			if pad > 0 {
				val += strings.Repeat(" ", pad)
			}
			parts[i] = val
		}
		return strings.Join(parts, " | ")
	}

	dividerParts := make([]string, colCount)
	for i, w := range widths {
		if w <= 0 {
			w = 1
		}
		dividerParts[i] = strings.Repeat("-", w)
	}
	divider := strings.Join(dividerParts, "-+-")

	var out []string
	out = append(out, renderRow(rows[0]))
	out = append(out, divider)
	for _, row := range rows[1:] {
		out = append(out, renderRow(row))
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
