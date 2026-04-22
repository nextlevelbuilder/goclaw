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
//   [i]…[/i]            italic
//   [u]…[/u]            underline
//   [s]…[/s]            strikethrough
//   [code]…[/code]      inline OR block monospace (Bitrix decides by \n presence)
//   [url=link]text[/url] named hyperlink
//   [url]link[/url]     bare hyperlink
//   [quote]…[/quote]    quote block
//
// NOT supported natively: headers, tables, ordered/unordered lists. These are
// flattened sensibly (headers → [b], lists → • bullets, tables → [code] block).
//
// Deliberate non-goals:
//   - [USER=id] mentions: LLM output never carries stable numeric IDs.
//   - [DISK=id] attachments: media goes through Phase 06, not text formatting.
//   - Colors / fonts / sizes: over-styling distracts from bot replies.

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

	// Extract Markdown tables and render as preformatted [code] blocks —
	// Bitrix has no table tag, so monospace is the best approximation.
	// Placeholders `\x00TB{i}\x00`.
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

	// Restore inline code spans as [code]…[/code].
	for i, code := range inline.codes {
		text = strings.ReplaceAll(text,
			fmt.Sprintf("\x00IC%d\x00", i),
			"[code]"+code+"[/code]")
	}

	// Restore tables — wrap each in a preformatted [code] block. Trim any
	// trailing newline we captured so the [/code] stays on its own line.
	for i, tbl := range tables.blocks {
		tbl = strings.TrimRight(tbl, "\n")
		text = strings.ReplaceAll(text,
			fmt.Sprintf("\x00TB%d\x00", i),
			"[code]\n"+tbl+"\n[/code]")
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
// single-line `` ```code``` `` mis-parse `code` as a lang hint:
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
// The extracted text is kept verbatim and re-emitted inside [code]…[/code]
// since Bitrix has no table primitive.
//
// The regex is deliberately permissive: two or more pipe-delimited lines in a
// row is enough to qualify. Mis-detection only costs us a monospace block
// around something that was probably meant to look tabular anyway.
func bxExtractTables(text string) bxExtractedTables {
	// Match header | ... |, separator | --- |, then 1+ body rows.
	re := regexp.MustCompile(`(?m)^\|[^\n]*\|\s*\n\|[\s\-|:]+\|\s*\n(?:\|[^\n]*\|\s*\n?)+`)
	var blocks []string
	for _, m := range re.FindAllString(text, -1) {
		blocks = append(blocks, m)
	}
	i := 0
	text = re.ReplaceAllStringFunc(text, func(_ string) string {
		p := fmt.Sprintf("\x00TB%d\x00", i)
		i++
		return p
	})
	return bxExtractedTables{text: text, blocks: blocks}
}
