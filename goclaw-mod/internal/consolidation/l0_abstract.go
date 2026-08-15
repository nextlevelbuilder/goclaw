package consolidation

import (
	"strings"
	"unicode"
)

// generateL0Abstract extracts a brief abstract (~50 tokens) from a summary.
// Uses extractive approach (first meaningful sentence), no LLM call.
func generateL0Abstract(summary string) string {
	sentences := splitSentences(summary)
	for _, s := range sentences {
		s = strings.TrimSpace(s)
		runes := []rune(s)
		if len(runes) >= 20 { // skip very short fragments
			if len(runes) > 200 {
				return string(runes[:200]) + "..."
			}
			return s
		}
	}
	// Fallback: first 200 runes of summary (UTF-8 safe)
	if runes := []rune(summary); len(runes) > 200 {
		return string(runes[:200]) + "..."
	}
	return summary
}

// splitSentences splits text on sentence boundaries (. ! ? followed by space/newline/EOF).
func splitSentences(text string) []string {
	var sentences []string
	var current strings.Builder

	runes := []rune(text)
	for i, r := range runes {
		current.WriteRune(r)
		if (r == '.' || r == '!' || r == '?') && i+1 < len(runes) &&
			(unicode.IsSpace(runes[i+1]) || runes[i+1] == '\n') {
			sentences = append(sentences, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		sentences = append(sentences, current.String())
	}
	return sentences
}

// extractEntityNames extracts capitalized multi-word phrases (proper nouns) from text.
// Lightweight — for search tagging, not KG extraction.
func extractEntityNames(text string) []string {
	words := strings.Fields(text)
	seen := make(map[string]bool)
	var entities []string

	for i := 0; i < len(words); i++ {
		w := words[i]
		if len(w) < 2 || !unicode.IsUpper([]rune(w)[0]) {
			continue
		}
		// Collect consecutive capitalized words as phrase
		phrase := cleanWord(w)
		for j := i + 1; j < len(words); j++ {
			next := words[j]
			if len(next) < 2 || !unicode.IsUpper([]rune(next)[0]) {
				break
			}
			phrase += " " + cleanWord(next)
			i = j
		}
		if len(phrase) >= 3 && !seen[phrase] {
			seen[phrase] = true
			entities = append(entities, phrase)
			if len(entities) >= 20 {
				break
			}
		}
	}
	return entities
}

// cleanWord removes trailing punctuation from a word.
func cleanWord(w string) string {
	return strings.TrimRightFunc(w, func(r rune) bool {
		return unicode.IsPunct(r) && r != '-' && r != '\''
	})
}
