package channels

import (
	"regexp"
	"strings"
)

// thinkOpenRe matches opening think tags emitted by various models.
// Covers: <think>, <thinking>, <thought>, <antThinking>, plus optional attributes.
var thinkOpenRe = regexp.MustCompile(`(?i)<\s*(?:think(?:ing)?|thought|antthinking)\b[^>]*>`)

// thinkCloseRe matches closing think tags.
var thinkCloseRe = regexp.MustCompile(`(?i)</\s*(?:think(?:ing)?|thought|antthinking)\s*>`)

// ThinkTagSplit holds the result of splitting think-tagged content.
type ThinkTagSplit struct {
	Thinking string // content inside <think> tags (empty if no tags found)
	Answer   string // content outside <think> tags
	Partial  bool   // true if an unclosed <think> tag is present (buffer for more)
}

// SplitThinkTags extracts thinking content from <think>...</think> tags.
// Returns empty Thinking if no tags are found. Handles multiple tag pairs
// and accumulates all thinking/answer segments.
func SplitThinkTags(text string) ThinkTagSplit {
	lower := strings.ToLower(text)
	// Fast path: no think-like tags at all
	if !strings.Contains(lower, "<think") &&
		!strings.Contains(lower, "<thought") &&
		!strings.Contains(lower, "<antthinking") {
		return ThinkTagSplit{Answer: text}
	}

	var thinking, answer strings.Builder
	remaining := text

	for remaining != "" {
		// Find opening tag
		openLoc := thinkOpenRe.FindStringIndex(remaining)
		if openLoc == nil {
			// No more opening tags — rest is answer
			answer.WriteString(remaining)
			break
		}

		// Content before opening tag is answer
		if openLoc[0] > 0 {
			answer.WriteString(remaining[:openLoc[0]])
		}

		// Find closing tag after the opening tag
		afterOpen := remaining[openLoc[1]:]
		closeLoc := thinkCloseRe.FindStringIndex(afterOpen)
		if closeLoc == nil {
			// Unclosed tag — content is still arriving (streaming)
			thinking.WriteString(afterOpen)
			return ThinkTagSplit{
				Thinking: thinking.String(),
				Answer:   answer.String(),
				Partial:  true,
			}
		}

		// Content between open and close tags is thinking
		thinking.WriteString(afterOpen[:closeLoc[0]])
		remaining = afterOpen[closeLoc[1]:]
	}

	return ThinkTagSplit{
		Thinking: thinking.String(),
		Answer:   answer.String(),
	}
}
