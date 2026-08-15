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
	Found    bool   // true if an opening think tag was found
	Pending  string // trailing text withheld because it may be a split opening tag
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
		answer, pending := splitTrailingThinkTagPrefix(text)
		return ThinkTagSplit{Answer: answer, Pending: pending}
	}

	var thinking, answer strings.Builder
	remaining := text
	found := false

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
		found = true

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
				Found:    true,
				Pending:  remaining[openLoc[0]:],
			}
		}

		// Content between open and close tags is thinking
		thinking.WriteString(afterOpen[:closeLoc[0]])
		remaining = afterOpen[closeLoc[1]:]
	}

	answerText := answer.String()
	pending := ""
	if !found {
		answerText, pending = splitTrailingThinkTagPrefix(answerText)
	}

	return ThinkTagSplit{
		Thinking: thinking.String(),
		Answer:   answerText,
		Found:    found,
		Pending:  pending,
	}
}

func splitTrailingThinkTagPrefix(text string) (answer, pending string) {
	idx := strings.LastIndex(text, "<")
	if idx < 0 {
		return text, ""
	}
	suffix := text[idx:]
	if strings.Contains(suffix, ">") || !isPossibleThinkOpenPrefix(suffix) {
		return text, ""
	}
	return text[:idx], suffix
}

func isPossibleThinkOpenPrefix(suffix string) bool {
	lower := strings.ToLower(suffix)
	if !strings.HasPrefix(lower, "<") {
		return false
	}
	rest := strings.TrimLeft(lower[1:], " \t\r\n")
	normalized := "<" + rest
	if normalized == "<" {
		return true
	}
	for _, tag := range []string{"<think", "<thinking", "<thought", "<antthinking"} {
		if strings.HasPrefix(tag, normalized) || strings.HasPrefix(normalized, tag) {
			return true
		}
	}
	return false
}
