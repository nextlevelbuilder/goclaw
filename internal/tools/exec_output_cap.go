package tools

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// cutAtLastNewline trims text to the nearest newline in the last 20% of the string.
func cutAtLastNewline(text string) string {
	threshold := len(text) * 80 / 100
	if idx := strings.LastIndex(text[threshold:], "\n"); idx >= 0 {
		return text[:threshold+idx]
	}
	return text
}

// cutAtFirstNewline trims text from the start at the nearest newline in the first 20%.
func cutAtFirstNewline(text string) string {
	searchEnd := len(text) * 20 / 100
	if searchEnd == 0 {
		return text
	}
	if idx := strings.Index(text[:searchEnd], "\n"); idx >= 0 {
		return text[idx+1:]
	}
	return text
}

// execMaxOutputChars is the maximum characters kept from exec/shell output.
// Larger outputs are truncated with head+tail strategy.
const execMaxOutputChars = 30000

// execImportantTailRe matches keywords indicating the tail contains important info.
var execImportantTailRe = regexp.MustCompile(`(?i)(error|exception|failed|fatal|traceback|panic|stack trace|exit code|total|summary|result|complete|finished|done|}\s*$)`)

// capExecOutput truncates exec output to maxChars using smart head+tail strategy.
// If the tail contains important content (errors, summaries), keeps 70% head + 30% tail.
// Otherwise keeps head only. Returns the original text if it fits within maxChars.
func capExecOutput(output string, maxChars int) string {
	if utf8.RuneCountInString(output) <= maxChars {
		return output
	}

	runes := []rune(output)
	totalRunes := len(runes)
	suffix := fmt.Sprintf("\n\n[Output truncated: %d chars total. Redirect to file for full output: command > output.txt]", totalRunes)
	budget := max(maxChars-utf8.RuneCountInString(suffix), 2000)

	// Check if tail has important content.
	tailCheckLen := min(2000, totalRunes)
	tailSample := string(runes[totalRunes-tailCheckLen:])

	if execImportantTailRe.MatchString(tailSample) && budget > 4000 {
		// Smart split: 70% head + 30% tail.
		headBudget := budget * 7 / 10
		tailBudget := budget - headBudget
		if tailBudget > 4000 {
			tailBudget = 4000
			headBudget = budget - tailBudget
		}

		head := string(runes[:headBudget])
		tail := string(runes[totalRunes-tailBudget:])

		// Cut at newline boundaries for cleaner output.
		head = cutAtLastNewline(head)
		tail = cutAtFirstNewline(tail)

		return head + "\n\n⚠️ [... middle content omitted ...]\n\n" + tail + suffix
	}

	// Head-only truncation.
	head := string(runes[:budget])
	head = cutAtLastNewline(head)
	return head + suffix
}
