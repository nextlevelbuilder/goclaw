// Package agent — response sanitization pipeline.
//
// Matching TS sanitization chain:
//
//	extractAssistantText() → per-block:
//	  1. stripMinimaxToolCallXml()        → Go: stripGarbledToolXML()
//	  2. stripDowngradedToolCallText()     → Go: stripDowngradedToolCallText()
//	  3. stripThinkingTagsFromText()       → Go: stripThinkingTags()
//	  then:
//	  4. sanitizeUserFacingText()          → Go: sanitizeUserFacingText()
//	     - stripFinalTagsFromText()        → Go: stripFinalTags()
//	     - collapseConsecutiveDuplicateBlocks()
//
// Additional Go-specific:
//	  5. stripEchoedSystemMessages()       → strip hallucinated [System Message] blocks
//	  6. stripGarbledToolXML()             → strip garbled XML from models like DeepSeek
package agent

import (
	"log/slog"
	"regexp"
	"strings"
)

// SanitizeAssistantContent applies the full sanitization pipeline to assistant
// response text before saving to session and sending to user.
// Matching TS extractAssistantText() + sanitizeUserFacingText().
func SanitizeAssistantContent(content string) string {
	if content == "" {
		return content
	}

	original := content

	// 1. Strip garbled tool-call XML (DeepSeek, GLM, Minimax)
	content = stripGarbledToolXML(content)
	if content == "" {
		return ""
	}

	// 2. Strip downgraded tool call text ([Tool Call: ...], [Tool Result ...])
	content = stripDowngradedToolCallText(content)

	// 3. Strip thinking/reasoning tags (<think>, <thinking>, <thought>, <antThinking>)
	content = stripThinkingTags(content)

	// 4. Strip <final> tags (keep content inside)
	content = stripFinalTags(content)

	// 5. Strip echoed [System Message] blocks
	content = stripEchoedSystemMessages(content)

	// 6. Collapse consecutive duplicate blocks
	content = collapseConsecutiveDuplicateBlocks(content)

	// 7. Strip MEDIA: paths from LLM output (media delivered separately)
	content = stripMediaPaths(content)

	// 8. Strip leading blank lines (preserve indentation)
	content = stripLeadingBlankLines(content)

	content = strings.TrimSpace(content)

	if content != original {
		slog.Debug("sanitized assistant content",
			"original_len", len(original),
			"cleaned_len", len(content),
		)
	}

	return content
}

// --- 1. Garbled tool-call XML ---

// garbledToolXMLPattern matches XML-like tool call artifacts that some models
// (DeepSeek, GLM, etc.) emit as text content instead of proper tool calls.
var garbledToolXMLPattern = regexp.MustCompile(
	`(?s)</?(?:function_calls?|functioninvoke|invoke|invfunction_calls|tool_call|tool_use|parameter|minimax:tool_call)[^>]*>`,
)

var garbledToolXMLIndicators = []string{
	"invfunction_calls",
	"functioninvoke",
	"<parameter name=",
	"</parameter",
	"<function_call",
	"<tool_call",
	"<tool_use",
	"<minimax:tool_call",
}

func stripGarbledToolXML(content string) string {
	hasIndicator := false
	lower := strings.ToLower(content)
	for _, ind := range garbledToolXMLIndicators {
		if strings.Contains(lower, strings.ToLower(ind)) {
			hasIndicator = true
			break
		}
	}
	if !hasIndicator {
		return content
	}

	cleaned := garbledToolXMLPattern.ReplaceAllString(content, "")
	cleaned = strings.TrimSpace(cleaned)

	if cleaned == "" {
		slog.Warn("stripped entire response as garbled tool XML", "original_len", len(content))
		return ""
	}

	slog.Warn("stripped garbled tool call XML from response",
		"original_len", len(content),
		"remaining_len", len(cleaned),
	)
	return cleaned
}

// --- 2. Downgraded tool call text ---

// stripDowngradedToolCallText removes [Tool Call: ...], [Tool Result ...],
// and [Historical context: ...] blocks that some models emit as text.
// Matching TS stripDowngradedToolCallText().
// Uses line-by-line scanning (Go regexp doesn't support lookahead).
func stripDowngradedToolCallText(content string) string {
	if !strings.Contains(content, "[Tool Call:") &&
		!strings.Contains(content, "[Tool Result") &&
		!strings.Contains(content, "[Historical context:") {
		return content
	}

	lines := strings.Split(content, "\n")
	var result []string
	skipping := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Start skipping on these markers
		if strings.HasPrefix(trimmed, "[Tool Call:") ||
			strings.HasPrefix(trimmed, "[Tool Result") ||
			strings.HasPrefix(trimmed, "[Historical context:") {
			skipping = true
			continue
		}

		// Stop skipping on non-indented, non-empty line that isn't part of the block
		if skipping {
			// Arguments JSON and tool output are typically indented or empty
			if trimmed == "" || strings.HasPrefix(trimmed, "Arguments:") ||
				strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "}") {
				continue
			}
			// Non-tool-block line → stop skipping
			skipping = false
		}

		result = append(result, line)
	}

	return strings.TrimSpace(strings.Join(result, "\n"))
}

// --- 3. Thinking/reasoning tags ---

// Matches TS stripThinkingTagsFromText() with strict mode.
// Strips: <think>...</think>, <thinking>...</thinking>, <thought>...</thought>,
//         <antThinking>...</antThinking>
// Go regexp doesn't support backreferences, so we use separate patterns.
var thinkingTagPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?is)<think>.*?</think>`),
	regexp.MustCompile(`(?is)<thinking>.*?</thinking>`),
	regexp.MustCompile(`(?is)<thought>.*?</thought>`),
	regexp.MustCompile(`(?is)<antThinking>.*?</antThinking>`),
	regexp.MustCompile(`(?is)<antthinking>.*?</antthinking>`),
}

func stripThinkingTags(content string) string {
	lower := strings.ToLower(content)
	if !strings.Contains(lower, "<think") && !strings.Contains(lower, "<thought") &&
		!strings.Contains(lower, "<antthinking") {
		return content
	}
	result := content
	for _, pat := range thinkingTagPatterns {
		result = pat.ReplaceAllString(result, "")
	}
	return strings.TrimSpace(result)
}

// --- 4. <final> tags ---

// Matches TS stripFinalTagsFromText(). Removes <final> and </final> tags
// but keeps the content inside.
var finalTagPattern = regexp.MustCompile(`(?i)<\s*/?\s*final\s*>`)

func stripFinalTags(content string) string {
	if !strings.Contains(strings.ToLower(content), "final") {
		return content
	}
	return finalTagPattern.ReplaceAllString(content, "")
}

// --- 5. Echoed [System Message] ---

// stripEchoedSystemMessages removes "[System Message] ..." blocks that LLMs
// hallucinate/echo in their response text.
// Uses line-based scanning (Go regexp doesn't support lookahead).
func stripEchoedSystemMessages(content string) string {
	if !strings.Contains(content, "[System Message]") {
		return content
	}

	lines := strings.Split(content, "\n")
	var result []string
	skipping := false

	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[System Message]") {
			skipping = true
			continue
		}
		if skipping {
			// Empty line ends the system message block
			if strings.TrimSpace(line) == "" {
				skipping = false
				continue
			}
			// Still part of the system message block (Stats:, reply instructions, etc.)
			continue
		}
		result = append(result, line)
	}

	cleaned := strings.TrimSpace(strings.Join(result, "\n"))

	if cleaned != strings.TrimSpace(content) {
		slog.Warn("stripped echoed [System Message] from assistant response",
			"original_len", len(content),
			"cleaned_len", len(cleaned),
		)
	}

	return cleaned
}

// --- 6. Collapse consecutive duplicate blocks ---

// collapseConsecutiveDuplicateBlocks removes repeated paragraph blocks.
// Matching TS collapseConsecutiveDuplicateBlocks().
func collapseConsecutiveDuplicateBlocks(content string) string {
	blocks := strings.Split(content, "\n\n")
	if len(blocks) <= 1 {
		return content
	}

	var result []string
	for i, block := range blocks {
		trimmed := strings.TrimSpace(block)
		if trimmed == "" {
			continue
		}
		if i > 0 && len(result) > 0 && trimmed == strings.TrimSpace(result[len(result)-1]) {
			continue // skip duplicate
		}
		result = append(result, block)
	}

	collapsed := strings.Join(result, "\n\n")
	if collapsed != content {
		slog.Debug("collapsed duplicate blocks",
			"original_blocks", len(blocks),
			"result_blocks", len(result),
		)
	}
	return collapsed
}

// --- 7. Strip MEDIA: paths ---

// stripMediaPaths removes lines containing MEDIA:/path references from LLM output.
// These are tool result artifacts that should not appear in user-facing text
// (media files are delivered separately via OutboundMessage.Media).
func stripMediaPaths(content string) string {
	if !strings.Contains(content, "MEDIA:") {
		return content
	}
	lines := strings.Split(content, "\n")
	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[[audio_as_voice]]") {
			continue
		}
		// Strip any line containing a MEDIA: path reference, regardless of wrapping format.
		// LLMs echo these in many forms: bare "MEDIA:/path", markdown "![alt](MEDIA:/path)",
		// JSON '{"image":"MEDIA:/path"}', etc. The /tmp/ or / after MEDIA: confirms it's a path.
		if strings.Contains(trimmed, "MEDIA:/") {
			continue
		}
		result = append(result, line)
	}
	return strings.TrimSpace(strings.Join(result, "\n"))
}

// --- 8. Strip leading blank lines ---

var leadingBlankLinesPattern = regexp.MustCompile(`^(?:[ \t]*\r?\n)+`)

func stripLeadingBlankLines(content string) string {
	return leadingBlankLinesPattern.ReplaceAllString(content, "")
}

// --- 9. Config leak detection (predefined agents) ---

// configLeakFileNames are internal file names that should not appear in user-facing output
// when a predefined agent describes its procedures or configuration.
var configLeakFileNames = []string{
	"SOUL.md", "IDENTITY.md", "AGENTS.md", "BOOTSTRAP.md",
	"internal_config", "system prompt",
}

// StripConfigLeak detects when a predefined agent dumps its internal configuration
// (e.g. referencing SOUL.md, AGENTS.md, IDENTITY.md) and replaces the entire
// response with a friendly decline.
//
// Only active for predefined agents. Single-gate detection:
// 3+ distinct internal file names mentioned → replace entire response.
// A predefined agent legitimately referencing 3+ internal config files in one
// response is essentially impossible, so this has no false-positive risk.
func StripConfigLeak(content, agentType string) string {
	if agentType != "predefined" || content == "" {
		return content
	}

	hits := 0
	for _, name := range configLeakFileNames {
		if strings.Contains(content, name) {
			hits++
		}
	}
	if hits < 3 {
		return content
	}

	slog.Warn("security.config_leak_stripped",
		"file_hits", hits,
		"original_len", len(content),
	)

	return "🔒 Security check not passed."
}

// --- NO_REPLY detection ---

// IsSilentReply checks if the text is a NO_REPLY token.
// Matching TS isSilentReplyText() from auto-reply/tokens.ts.
func IsSilentReply(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	const token = "NO_REPLY"
	// Exact match
	if trimmed == token {
		return true
	}
	// Starts with token followed by non-word char or end
	if strings.HasPrefix(trimmed, token) {
		rest := trimmed[len(token):]
		if rest == "" || !isWordChar(rune(rest[0])) {
			return true
		}
	}
	// Ends with token preceded by non-word char
	if strings.HasSuffix(trimmed, token) {
		before := trimmed[:len(trimmed)-len(token)]
		if before == "" || !isWordChar(rune(before[len(before)-1])) {
			return true
		}
	}
	return false
}

func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}
