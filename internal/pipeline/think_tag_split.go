package pipeline

import (
	"regexp"
	"strings"
)

var taggedThinkingOpenRe = regexp.MustCompile(`(?is)<\s*(?:redacted_thinking|think(?:ing)?|thought|antthinking)\b[^>]*>`)
var taggedThinkingCloseRe = regexp.MustCompile(`(?is)</\s*(?:redacted_thinking|think(?:ing)?|thought|antthinking)\s*>`)

func splitTaggedThinkingContent(content, existingThinking string) (string, string) {
	lower := strings.ToLower(content)
	if !strings.Contains(lower, "<think") &&
		!strings.Contains(lower, "<thought") &&
		!strings.Contains(lower, "<antthinking") &&
		!strings.Contains(lower, "<redacted_thinking") {
		return content, existingThinking
	}

	var answer strings.Builder
	var taggedThinking strings.Builder
	remaining := content

	for remaining != "" {
		openLoc := taggedThinkingOpenRe.FindStringIndex(remaining)
		if openLoc == nil {
			answer.WriteString(remaining)
			break
		}

		answer.WriteString(remaining[:openLoc[0]])
		afterOpen := remaining[openLoc[1]:]
		closeLoc := taggedThinkingCloseRe.FindStringIndex(afterOpen)
		if closeLoc == nil {
			taggedThinking.WriteString(afterOpen)
			break
		}

		taggedThinking.WriteString(afterOpen[:closeLoc[0]])
		remaining = afterOpen[closeLoc[1]:]
	}

	return answer.String(), appendTaggedThinking(existingThinking, taggedThinking.String())
}

func appendTaggedThinking(existing, addition string) string {
	existing = strings.TrimSpace(existing)
	addition = strings.TrimSpace(addition)
	if existing == "" {
		return addition
	}
	if addition == "" {
		return existing
	}
	return existing + "\n" + addition
}
