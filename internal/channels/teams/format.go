package teams

import (
	"regexp"
	"strings"
)

const teamsMaxMessageBytes = 80000 // 80KB safe margin (official limit 40-100KB)

// sanitizeForTeams cleans LLM markdown output for Teams consumption.
// Teams renders markdown natively (textFormat: "markdown"), so this only
// strips unsupported or dangerous elements.
func sanitizeForTeams(text string) string {
	text = stripHTMLTagsOutsideCode(text)
	text = stripStrikethrough(text)
	text = convertTablesToCodeBlocks(text)
	return strings.TrimSpace(text)
}

var reHTMLTag = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)

// stripHTMLTagsOutsideCode removes raw HTML tags from LLM output but preserves
// HTML inside code blocks (``` fences) and inline code (` backticks).
func stripHTMLTagsOutsideCode(text string) string {
	var result strings.Builder
	result.Grow(len(text))

	inCodeFence := false
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i > 0 {
			result.WriteByte('\n')
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeFence = !inCodeFence
			result.WriteString(line)
			continue
		}
		if inCodeFence {
			result.WriteString(line)
			continue
		}
		// Outside code fence: strip HTML but preserve inline code
		result.WriteString(stripHTMLPreservingInlineCode(line))
	}
	return result.String()
}

// stripHTMLPreservingInlineCode strips HTML tags from a line but preserves content inside backticks.
func stripHTMLPreservingInlineCode(line string) string {
	var result strings.Builder
	inInlineCode := false
	for i := 0; i < len(line); i++ {
		if line[i] == '`' {
			inInlineCode = !inInlineCode
			result.WriteByte('`')
			continue
		}
		if inInlineCode {
			result.WriteByte(line[i])
			continue
		}
		// Outside inline code: check for HTML tag
		if line[i] == '<' {
			end := strings.IndexByte(line[i:], '>')
			if end > 0 && reHTMLTag.MatchString(line[i:i+end+1]) {
				i += end // skip the tag
				continue
			}
		}
		result.WriteByte(line[i])
	}
	return result.String()
}

var reStrikethrough = regexp.MustCompile(`~~(.+?)~~`)

// stripStrikethrough removes ~~ markers (inconsistent across Teams platforms).
func stripStrikethrough(text string) string {
	return reStrikethrough.ReplaceAllString(text, "$1")
}

// reTableRow matches a markdown table row: | col1 | col2 |
var reTableRow = regexp.MustCompile(`^\|.*\|$`)

// reTableSep matches a markdown table separator: |---|---|
var reTableSep = regexp.MustCompile(`^\|[-:\s|]+\|$`)

// convertTablesToCodeBlocks wraps markdown tables in code fences for monospace rendering.
// Skips tables already inside code fences.
func convertTablesToCodeBlocks(text string) string {
	lines := strings.Split(text, "\n")
	var result []string
	inTable := false
	inCodeFence := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track existing code fences — don't wrap tables inside them
		if strings.HasPrefix(trimmed, "```") {
			inCodeFence = !inCodeFence
			if inTable {
				inTable = false
				result = append(result, "```")
			}
			result = append(result, line)
			continue
		}

		if inCodeFence {
			result = append(result, line)
			continue
		}

		isTableLine := reTableRow.MatchString(trimmed) || reTableSep.MatchString(trimmed)

		if isTableLine && !inTable {
			inTable = true
			result = append(result, "```")
		} else if !isTableLine && inTable {
			inTable = false
			result = append(result, "```")
		}
		result = append(result, line)
	}

	if inTable {
		result = append(result, "```")
	}

	return strings.Join(result, "\n")
}

// chunkMarkdown splits markdown text into chunks that fit within maxLen bytes.
// Split priority: paragraph (\n\n) > line (\n) > word (space) > force.
// Preserves code fence (```) continuity across chunks.
func chunkMarkdown(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	for len(text) > 0 {
		if len(text) <= maxLen {
			chunks = append(chunks, text)
			break
		}

		cutAt := findSplitPoint(text, maxLen)
		chunk := text[:cutAt]
		text = strings.TrimLeft(text[cutAt:], "\n ")

		if chunk = strings.TrimSpace(chunk); chunk != "" {
			chunks = append(chunks, chunk)
		}
	}

	return closeAndReopenFences(chunks)
}

// findSplitPoint finds the best position to split text at or before maxLen.
func findSplitPoint(text string, maxLen int) int {
	// Try paragraph boundary
	if idx := strings.LastIndex(text[:maxLen], "\n\n"); idx > 0 {
		return idx
	}
	// Try line boundary
	if idx := strings.LastIndex(text[:maxLen], "\n"); idx > 0 {
		return idx
	}
	// Try word boundary
	if idx := strings.LastIndex(text[:maxLen], " "); idx > 0 {
		return idx
	}
	// Force split at maxLen (monolithic block)
	return maxLen
}

// closeAndReopenFences ensures code fences (```) are properly closed/reopened
// when a chunk splits inside a fenced code block.
func closeAndReopenFences(chunks []string) []string {
	for i := range chunks {
		opens := strings.Count(chunks[i], "```")
		if opens%2 == 1 && i < len(chunks)-1 {
			// Odd count = unclosed fence in this chunk
			chunks[i] += "\n```"
			chunks[i+1] = "```\n" + chunks[i+1]
		}
	}
	return chunks
}
