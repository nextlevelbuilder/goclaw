package googlechat

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	codePlaceholder = "\x00"
	boldMarker      = "\x01" // temp marker for bold * to avoid italic conversion
)

var (
	reCodeBlock     = regexp.MustCompile("(?s)(```[\\s\\S]*?```)")
	reCodeInline    = regexp.MustCompile("(`[^`]+`)")
	reBoldItalic    = regexp.MustCompile(`\*\*\*(.+?)\*\*\*`)
	reBold          = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reStrike        = regexp.MustCompile(`~~(.+?)~~`)
	reLink          = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	reTable         = regexp.MustCompile(`(?m)^\|.+\|$\n^\|[-| :]+\|$`)
	reLongCodeBlock = regexp.MustCompile("(?s)```[\\w]*\\n(.{500,}?)\\n```")
	reHeading       = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+)$`)
)

// markdownToGoogleChat converts markdown to Google Chat plain-text markup.
// Used for simple text messages (non-CardV2).
func markdownToGoogleChat(text string) string {
	if text == "" {
		return ""
	}

	var codeBlocks []string
	protected := reCodeBlock.ReplaceAllStringFunc(text, func(match string) string {
		codeBlocks = append(codeBlocks, match)
		return codePlaceholder
	})
	var inlineCodes []string
	protected = reCodeInline.ReplaceAllStringFunc(protected, func(match string) string {
		inlineCodes = append(inlineCodes, match)
		return codePlaceholder
	})

	// Headers: ## text → *text* (bold in Google Chat)
	protected = reHeading.ReplaceAllString(protected, boldMarker+"${2}"+boldMarker)
	// Bold+italic: ***text*** → *_text_* (use boldMarker to protect from italic converter)
	protected = reBoldItalic.ReplaceAllString(protected, boldMarker+"_${1}_"+boldMarker)
	// Bold: **text** → *text* (use boldMarker to protect from italic converter)
	protected = reBold.ReplaceAllString(protected, boldMarker+"${1}"+boldMarker)
	// Italic: *text* → _text_ (only matches unprotected single *)
	protected = convertItalic(protected)
	// Restore boldMarker → *
	protected = strings.ReplaceAll(protected, boldMarker, "*")
	protected = reStrike.ReplaceAllString(protected, "~${1}~")
	protected = reLink.ReplaceAllString(protected, "<${2}|${1}>")

	codeIdx := 0
	inlineIdx := 0
	var result strings.Builder
	for _, r := range protected {
		if string(r) == codePlaceholder {
			if codeIdx < len(codeBlocks) {
				result.WriteString(codeBlocks[codeIdx])
				codeIdx++
			} else if inlineIdx < len(inlineCodes) {
				result.WriteString(inlineCodes[inlineIdx])
				inlineIdx++
			}
		} else {
			result.WriteRune(r)
		}
	}

	return result.String()
}

// markdownToGoogleChatHTML converts markdown to HTML subset supported by CardV2 textParagraph.
// Supports: <b>, <i>, <strike>, <a href="">, <br>, <font color="">
func markdownToGoogleChatHTML(text string) string {
	if text == "" {
		return ""
	}

	var codeBlocks []string
	protected := reCodeBlock.ReplaceAllStringFunc(text, func(match string) string {
		codeBlocks = append(codeBlocks, match)
		return codePlaceholder
	})
	var inlineCodes []string
	protected = reCodeInline.ReplaceAllStringFunc(protected, func(match string) string {
		inlineCodes = append(inlineCodes, match)
		return codePlaceholder
	})

	// Headers → bold HTML
	protected = reHeading.ReplaceAllString(protected, "<b>${2}</b>")
	// Bold+italic
	protected = reBoldItalic.ReplaceAllString(protected, "<b><i>${1}</i></b>")
	// Bold
	protected = reBold.ReplaceAllString(protected, "<b>${1}</b>")
	// Italic (single *)
	protected = convertItalicHTML(protected)
	// Strikethrough
	protected = reStrike.ReplaceAllString(protected, "<strike>${1}</strike>")
	// Links
	protected = reLink.ReplaceAllString(protected, `<a href="${2}">${1}</a>`)

	codeIdx := 0
	inlineIdx := 0
	var result strings.Builder
	for _, r := range protected {
		if string(r) == codePlaceholder {
			if codeIdx < len(codeBlocks) {
				block := codeBlocks[codeIdx]
				block = strings.TrimPrefix(block, "```")
				if idx := strings.IndexByte(block, '\n'); idx >= 0 {
					block = block[idx+1:]
				}
				block = strings.TrimSuffix(block, "```")
				block = strings.TrimSpace(block)
				result.WriteString("<pre>" + block + "</pre>")
				codeIdx++
			} else if inlineIdx < len(inlineCodes) {
				code := inlineCodes[inlineIdx]
				code = strings.Trim(code, "`")
				result.WriteString("<code>" + code + "</code>")
				inlineIdx++
			}
		} else {
			result.WriteRune(r)
		}
	}

	return result.String()
}

// convertItalicHTML converts single *text* to <i>text</i>.
func convertItalicHTML(s string) string {
	var result strings.Builder
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		if runes[i] == '*' {
			prevStar := i > 0 && runes[i-1] == '*'
			nextStar := i+1 < len(runes) && runes[i+1] == '*'
			if !prevStar && !nextStar {
				end := -1
				for j := i + 1; j < len(runes); j++ {
					if runes[j] == '*' {
						nextJ := j+1 < len(runes) && runes[j+1] == '*'
						prevJ := j > 0 && runes[j-1] == '*'
						if !nextJ && !prevJ {
							end = j
							break
						}
					}
				}
				if end > 0 {
					result.WriteString("<i>")
					result.WriteString(string(runes[i+1 : end]))
					result.WriteString("</i>")
					i = end + 1
					continue
				}
			}
		}
		result.WriteRune(runes[i])
		i++
	}
	return result.String()
}

func convertItalic(s string) string {
	var result strings.Builder
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		if runes[i] == '*' {
			prevStar := i > 0 && runes[i-1] == '*'
			nextStar := i+1 < len(runes) && runes[i+1] == '*'
			if !prevStar && !nextStar {
				end := -1
				for j := i + 1; j < len(runes); j++ {
					if runes[j] == '*' {
						nextJ := j+1 < len(runes) && runes[j+1] == '*'
						prevJ := j > 0 && runes[j-1] == '*'
						if !nextJ && !prevJ {
							end = j
							break
						}
					}
				}
				if end > 0 {
					result.WriteRune('_')
					result.WriteString(string(runes[i+1 : end]))
					result.WriteRune('_')
					i = end + 1
					continue
				}
			}
		}
		result.WriteRune(runes[i])
		i++
	}
	return result.String()
}

func detectStructuredContent(text string) bool {
	return reTable.MatchString(text) || reLongCodeBlock.MatchString(text)
}

// splitTableCells splits "| a | b | c |" into ["a", "b", "c"].
func splitTableCells(row string) []string {
	row = strings.TrimSpace(row)
	row = strings.Trim(row, "|")
	parts := strings.Split(row, "|")
	var cells []string
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells
}

// parseTableRows extracts header and data rows from collected table lines.
func parseTableRows(lines []string) (headers []string, rows [][]string) {
	if len(lines) == 0 {
		return nil, nil
	}
	for _, line := range lines {
		cells := splitTableCells(line)
		if len(cells) == 0 {
			continue
		}
		if headers == nil {
			headers = cells
		} else {
			rows = append(rows, cells)
		}
	}
	return
}

// buildTableWidget creates a CardV2 widget for a markdown table.
// 2-column tables use decoratedText (key-value); wider tables use aligned <pre>.
func buildTableWidget(tableLines []string) map[string]any {
	headers, rows := parseTableRows(tableLines)
	if len(headers) == 0 {
		return nil
	}

	// 2-column tables → decoratedText widgets stacked vertically
	if len(headers) == 2 {
		var widgets []map[string]any
		for _, row := range rows {
			if len(row) < 2 {
				continue
			}
			widgets = append(widgets, map[string]any{
				"decoratedText": map[string]any{
					"topLabel": markdownToGoogleChatHTML(row[0]),
					"text":     markdownToGoogleChatHTML(row[1]),
				},
			})
		}
		if len(widgets) > 0 {
			return map[string]any{"__widgets": widgets}
		}
	}

	// Wider tables → aligned monospace
	allRows := [][]string{headers}
	allRows = append(allRows, rows...)

	colWidths := make([]int, len(headers))
	for _, row := range allRows {
		for j, cell := range row {
			if j < len(colWidths) && len([]rune(cell)) > colWidths[j] {
				colWidths[j] = len([]rune(cell))
			}
		}
	}

	var sb strings.Builder
	for i, row := range allRows {
		for j, cell := range row {
			if j > 0 {
				sb.WriteString(" | ")
			}
			if j < len(colWidths) {
				padding := colWidths[j] - len([]rune(cell))
				sb.WriteString(cell)
				if padding > 0 {
					sb.WriteString(strings.Repeat(" ", padding))
				}
			} else {
				sb.WriteString(cell)
			}
		}
		sb.WriteString("\n")
		if i == 0 {
			for j := range colWidths {
				if j > 0 {
					sb.WriteString("-+-")
				}
				sb.WriteString(strings.Repeat("-", colWidths[j]))
			}
			sb.WriteString("\n")
		}
	}

	return map[string]any{
		"textParagraph": map[string]string{
			"text": fmt.Sprintf("<pre>%s</pre>", strings.TrimRight(sb.String(), "\n")),
		},
	}
}

func chunkByBytes(text string, maxBytes int) []string {
	if text == "" {
		return nil
	}
	if len([]byte(text)) <= maxBytes {
		return []string{text}
	}

	var chunks []string
	paragraphs := strings.Split(text, "\n\n")
	if len(paragraphs) > 1 {
		var current strings.Builder
		for i, p := range paragraphs {
			sep := ""
			if i > 0 {
				sep = "\n\n"
			}
			candidate := current.String() + sep + p
			if len([]byte(candidate)) > maxBytes && current.Len() > 0 {
				chunks = append(chunks, current.String())
				current.Reset()
				current.WriteString(p)
			} else {
				if current.Len() > 0 {
					current.WriteString(sep)
				}
				current.WriteString(p)
			}
		}
		if current.Len() > 0 {
			remaining := current.String()
			if len([]byte(remaining)) > maxBytes {
				chunks = append(chunks, chunkByLines(remaining, maxBytes)...)
			} else {
				chunks = append(chunks, remaining)
			}
		}
		return chunks
	}

	return chunkByLines(text, maxBytes)
}

func chunkByLines(text string, maxBytes int) []string {
	lines := strings.Split(text, "\n")
	if len(lines) <= 1 {
		return chunkByWords(text, maxBytes)
	}

	var chunks []string
	var current strings.Builder
	for i, line := range lines {
		sep := ""
		if i > 0 {
			sep = "\n"
		}
		candidate := current.String() + sep + line
		if len([]byte(candidate)) > maxBytes && current.Len() > 0 {
			chunks = append(chunks, current.String())
			current.Reset()
			if len([]byte(line)) > maxBytes {
				chunks = append(chunks, chunkByWords(line, maxBytes)...)
			} else {
				current.WriteString(line)
			}
		} else {
			if current.Len() > 0 {
				current.WriteString(sep)
			}
			current.WriteString(line)
		}
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}

func chunkByWords(text string, maxBytes int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{text}
	}

	var chunks []string
	var current strings.Builder
	for _, word := range words {
		sep := ""
		if current.Len() > 0 {
			sep = " "
		}
		candidate := current.String() + sep + word
		if len([]byte(candidate)) > maxBytes && current.Len() > 0 {
			chunks = append(chunks, current.String())
			current.Reset()
			if len([]byte(word)) > maxBytes {
				chunks = append(chunks, splitAtUTF8Boundary(word, maxBytes)...)
			} else {
				current.WriteString(word)
			}
		} else {
			if current.Len() > 0 {
				current.WriteString(sep)
			}
			current.WriteString(word)
		}
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}

func splitAtUTF8Boundary(word string, maxBytes int) []string {
	var chunks []string
	b := []byte(word)
	for len(b) > 0 {
		end := maxBytes
		if end > len(b) {
			end = len(b)
		}
		for end > 0 && !utf8.Valid(b[:end]) {
			end--
		}
		if end == 0 {
			end = 1
		}
		chunks = append(chunks, string(b[:end]))
		b = b[end:]
	}
	return chunks
}

func extractSummary(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return content
	}

	var heading string
	var bullets []string
	var textLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") && heading == "" {
			heading = strings.TrimPrefix(trimmed, "# ")
			continue
		}
		if strings.HasPrefix(trimmed, "## ") && heading != "" {
			break
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			if len(bullets) < 3 {
				bullets = append(bullets, trimmed)
			}
			continue
		}
		if trimmed != "" && heading != "" && len(bullets) == 0 {
			textLines = append(textLines, trimmed)
		}
	}

	var parts []string
	if heading != "" {
		parts = append(parts, heading)
	}
	if len(textLines) > 0 && len(bullets) == 0 {
		parts = append(parts, strings.Join(textLines, "\n"))
	}
	if len(bullets) > 0 {
		parts = append(parts, strings.Join(bullets, "\n"))
	}

	result := strings.Join(parts, "\n\n")
	if result == "" {
		return content
	}
	return result
}
