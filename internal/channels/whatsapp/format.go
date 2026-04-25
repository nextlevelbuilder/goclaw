package whatsapp

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// markdownToWhatsApp converts Markdown-formatted LLM output to WhatsApp's native
// formatting syntax. WhatsApp supports: *bold*, _italic_, ~strikethrough~, ```code```.
// Unsupported features are simplified: headers → bold, links → "text url".
// Tables are rendered according to tableMode (default "auto" → cards for wide tables).
func markdownToWhatsApp(text, tableMode string) string {
	if text == "" {
		return ""
	}

	// Pre-process: convert HTML tags from LLM output to Markdown equivalents.
	text = htmlTagToWaMd(text)

	// Extract and render markdown tables before the rest of the pipeline.
	text = waRenderTables(text, tableMode)

	// Extract and protect fenced code blocks — WhatsApp renders ``` the same way.
	codeBlocks := waExtractCodeBlocks(text)
	text = codeBlocks.text

	// Headers (##, ###, etc.) → *bold text* (WhatsApp has no header concept).
	text = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`).ReplaceAllString(text, "*$1*")

	// Blockquotes → plain text.
	text = regexp.MustCompile(`(?m)^>\s*(.*)$`).ReplaceAllString(text, "$1")

	// Links [text](url) → "text url" (WhatsApp doesn't support markdown links).
	text = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`).ReplaceAllString(text, "$1 $2")

	// Bold: **text** or __text__ → *text*
	text = regexp.MustCompile(`\*\*(.+?)\*\*`).ReplaceAllString(text, "*$1*")
	text = regexp.MustCompile(`__(.+?)__`).ReplaceAllString(text, "*$1*")

	// Strikethrough: ~~text~~ → ~text~
	text = regexp.MustCompile(`~~(.+?)~~`).ReplaceAllString(text, "~$1~")

	// Inline code: `code` → ```code``` (WhatsApp has no inline code, only blocks).
	text = regexp.MustCompile("`([^`]+)`").ReplaceAllString(text, "```$1```")

	// List items: leading - or * → bullet •
	text = regexp.MustCompile(`(?m)^[-*]\s+`).ReplaceAllString(text, "• ")

	// Restore code blocks as ``` … ``` preserving original content.
	for i, code := range codeBlocks.codes {
		// Trim trailing newline from extracted content — we add our own.
		code = strings.TrimRight(code, "\n")
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00CB%d\x00", i), "```\n"+code+"\n```")
	}

	// Collapse 3+ blank lines to 2.
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")

	return strings.TrimSpace(text)
}

var (
	waTblLineRe = regexp.MustCompile(`^\s*\|.*\|\s*$`)
	waTblSepRe  = regexp.MustCompile(`^\s*\|[\s:]*-+[\s:]*(\|[\s:]*-+[\s:]*)*\|\s*$`)
)

// waRenderTables finds markdown tables in text and replaces them with the
// rendered form according to tableMode. Default mode is "auto" (cards for
// wide tables, plain bullet list for narrow). Mode "off" passes tables through.
func waRenderTables(text, tableMode string) string {
	mode := channels.ParseTableMode(tableMode)
	if mode == channels.TableModeOff {
		return text
	}
	// Default WhatsApp to "cards" when mode is "auto" — WhatsApp is mobile-first
	// and ASCII tables never render well in WhatsApp's monospace-free format.
	if mode == channels.TableModeAuto {
		mode = channels.TableModeCards
	}

	lines := strings.Split(text, "\n")
	var result []string
	i := 0
	for i < len(lines) {
		if i+1 < len(lines) && waTblLineRe.MatchString(lines[i]) && waTblSepRe.MatchString(lines[i+1]) {
			start := i
			i += 2 // skip header + separator
			for i < len(lines) && waTblLineRe.MatchString(lines[i]) {
				i++
			}
			parsed := channels.ParseMarkdownTableRows(lines[start:i])
			result = append(result, channels.RenderTable(mode, parsed))
		} else {
			result = append(result, lines[i])
			i++
		}
	}
	return strings.Join(result, "\n")
}

// htmlTagToWaMd converts common HTML tags in LLM output to Markdown equivalents
// so they are then processed by the markdown → WhatsApp pipeline above.
var htmlToWaMdReplacers = []struct {
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

func htmlTagToWaMd(text string) string {
	for _, r := range htmlToWaMdReplacers {
		text = r.re.ReplaceAllString(text, r.repl)
	}
	return text
}

type waCodeBlockMatch struct {
	text  string
	codes []string
}

// waExtractCodeBlocks pulls fenced code blocks out of text and replaces them with
// \x00CB{n}\x00 placeholders so other regex passes don't mangle their contents.
func waExtractCodeBlocks(text string) waCodeBlockMatch {
	re := regexp.MustCompile("```[\\w]*\\n?([\\s\\S]*?)```")
	matches := re.FindAllStringSubmatch(text, -1)

	codes := make([]string, 0, len(matches))
	for _, m := range matches {
		codes = append(codes, m[1])
	}

	i := 0
	text = re.ReplaceAllStringFunc(text, func(_ string) string {
		placeholder := fmt.Sprintf("\x00CB%d\x00", i)
		i++
		return placeholder
	})

	return waCodeBlockMatch{text: text, codes: codes}
}

// chunkText splits text into pieces that fit within maxLen,
// preferring to split at paragraph (\n\n) or line (\n) boundaries.
func chunkText(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	for len(text) > 0 {
		if len(text) <= maxLen {
			chunks = append(chunks, text)
			break
		}
		// Find the best split point: paragraph > line > space > hard cut.
		cutAt := maxLen
		if idx := strings.LastIndex(text[:maxLen], "\n\n"); idx > 0 {
			cutAt = idx
		} else if idx := strings.LastIndex(text[:maxLen], "\n"); idx > 0 {
			cutAt = idx
		} else if idx := strings.LastIndex(text[:maxLen], " "); idx > 0 {
			cutAt = idx
		}
		chunks = append(chunks, strings.TrimRight(text[:cutAt], " \n"))
		text = strings.TrimLeft(text[cutAt:], " \n")
	}
	return chunks
}
