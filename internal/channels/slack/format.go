package slack

import (
	"fmt"
	"regexp"
	"strings"
)

// markdownToSlackMrkdwn converts standard markdown (LLM output) to Slack mrkdwn syntax.
// Pipeline order (CRITICAL):
//  1. htmlTagsToMarkdown  -- convert HTML tags to markdown
//  2. extractSlackTokens  -- protect <@U123>, <#C456>, <url> BEFORE escaping
//  3. escapeHTMLEntities  -- escape &, <, > (won't touch protected tokens)
//  4. extractCodeBlocks   -- protect code fences from formatting
//  5. extractInlineCodes  -- protect inline code
//  6. convertTablesToCodeBlocks -- render tables as code blocks
func markdownToSlackMrkdwn(text string) string {
	if text == "" {
		return ""
	}

	text = htmlTagsToMarkdown(text)

	// Preserve Slack-native tokens BEFORE HTML entity escaping
	slackTokens, text := extractSlackTokens(text)

	text = escapeHTMLEntities(text)

	codeBlocks, text := extractCodeBlocks(text)
	inlineCodes, text := extractInlineCodes(text)

	text = convertTablesToCodeBlocks(text)

	// Convert markdown links: [text](url) -> <url|text>
	text = reLink.ReplaceAllString(text, "<$2|$1>")

	// Convert bold: **text** or __text__ -> *text*
	text = reBoldDouble.ReplaceAllString(text, "*$1*")
	text = reBoldUnderscore.ReplaceAllString(text, "*$1*")

	// Convert strikethrough: ~~text~~ -> ~text~
	text = reStrike.ReplaceAllString(text, "~$1~")

	// Convert headers: # Header -> *Header* (no native header in mrkdwn)
	text = reHeader.ReplaceAllString(text, "*$1*")

	// Restore Slack tokens
	for i, token := range slackTokens {
		text = strings.Replace(text, fmt.Sprintf("\x00ST%d\x00", i), token, 1)
	}

	// Restore inline code
	for i, code := range inlineCodes {
		text = strings.Replace(text, fmt.Sprintf("\x00IC%d\x00", i), "`"+code+"`", 1)
	}

	// Restore code blocks
	for i, block := range codeBlocks {
		text = strings.Replace(text, fmt.Sprintf("\x00CB%d\x00", i), "```"+block+"```", 1)
	}

	return text
}

// Compiled regex patterns.
var (
	reLink           = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	reBoldDouble     = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reBoldUnderscore = regexp.MustCompile(`__(.+?)__`)
	reStrike         = regexp.MustCompile(`~~(.+?)~~`)
	reHeader         = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	reHTMLBold       = regexp.MustCompile(`(?i)<(?:b|strong)>([\s\S]*?)</(?:b|strong)>`)
	reHTMLItalic     = regexp.MustCompile(`(?i)<(?:i|em)>([\s\S]*?)</(?:i|em)>`)
	reHTMLStrike     = regexp.MustCompile(`(?i)<(?:s|strike|del)>([\s\S]*?)</(?:s|strike|del)>`)
	reHTMLCode       = regexp.MustCompile(`(?i)<code>([\s\S]*?)</code>`)
	reHTMLLink       = regexp.MustCompile(`(?i)<a\s+href="([^"]+)"[^>]*>([\s\S]*?)</a>`)
	reHTMLBreak      = regexp.MustCompile(`(?i)<br\s*/?>`)
	reHTMLPara       = regexp.MustCompile(`(?i)</?p\s*>`)
	reTableRow       = regexp.MustCompile(`(?m)^\|(.+)\|$`)
	reTableSep       = regexp.MustCompile(`(?m)^\|[\s:]*-+[\s:|-]*\|$`)
	reSlackToken     = regexp.MustCompile(`<[@#!][^>]+>|<(?:mailto|tel|https?|ftp):[^>]+>`)
)

func escapeHTMLEntities(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	return text
}

func extractSlackTokens(text string) (tokens []string, result string) {
	result = reSlackToken.ReplaceAllStringFunc(text, func(match string) string {
		tokens = append(tokens, match)
		return fmt.Sprintf("\x00ST%d\x00", len(tokens)-1)
	})
	return tokens, result
}

func htmlTagsToMarkdown(text string) string {
	text = reHTMLBreak.ReplaceAllString(text, "\n")
	text = reHTMLPara.ReplaceAllString(text, "\n")
	text = reHTMLBold.ReplaceAllString(text, "**$1**")
	text = reHTMLItalic.ReplaceAllString(text, "_${1}_")
	text = reHTMLStrike.ReplaceAllString(text, "~~$1~~")
	text = reHTMLCode.ReplaceAllString(text, "`$1`")
	text = reHTMLLink.ReplaceAllString(text, "[$2]($1)")
	return text
}

func extractCodeBlocks(text string) (blocks []string, result string) {
	parts := strings.Split(text, "```")
	if len(parts) < 3 {
		return nil, text
	}

	var sb strings.Builder
	for i, part := range parts {
		if i%2 == 1 {
			blocks = append(blocks, part)
			sb.WriteString(fmt.Sprintf("\x00CB%d\x00", len(blocks)-1))
		} else {
			sb.WriteString(part)
		}
	}
	// If odd number of ```, the last unpaired one is literal
	if len(parts)%2 == 0 {
		sb.WriteString("```")
		sb.WriteString(parts[len(parts)-1])
	}
	return blocks, sb.String()
}

func extractInlineCodes(text string) (codes []string, result string) {
	var sb strings.Builder
	inCode := false
	codeStart := 0

	for i := 0; i < len(text); i++ {
		if text[i] == '`' {
			if inCode {
				codes = append(codes, text[codeStart:i])
				sb.WriteString(fmt.Sprintf("\x00IC%d\x00", len(codes)-1))
				inCode = false
			} else {
				inCode = true
				codeStart = i + 1
			}
		} else if !inCode {
			sb.WriteByte(text[i])
		}
	}

	if inCode {
		sb.WriteByte('`')
		sb.WriteString(text[codeStart:])
	}

	return codes, sb.String()
}

func convertTablesToCodeBlocks(text string) string {
	lines := strings.Split(text, "\n")
	var result []string
	var tableLines []string
	inTable := false

	for _, line := range lines {
		isTableRow := reTableRow.MatchString(line)

		if isTableRow {
			if !inTable {
				inTable = true
				tableLines = nil
			}
			if reTableSep.MatchString(line) {
				continue
			}
			tableLines = append(tableLines, line)
		} else {
			if inTable {
				result = append(result, "```")
				result = append(result, tableLines...)
				result = append(result, "```")
				inTable = false
				tableLines = nil
			}
			result = append(result, line)
		}
	}

	if inTable && len(tableLines) > 0 {
		result = append(result, "```")
		result = append(result, tableLines...)
		result = append(result, "```")
	}

	return strings.Join(result, "\n")
}
