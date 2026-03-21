package agent

import (
	"regexp"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// Regex patterns for extractive memory fallback.
var (
	// Decisions: "decided to", "let's go with", "approved", "agreed on", "chose", "we'll use"
	reDecision = regexp.MustCompile(`(?i)(?:decided\s+to|let'?s\s+go\s+with|approved|agreed\s+on|chose|we'?ll\s+use)\s+.{5,120}`)

	// User preferences: "I prefer", "don't do", "always", "never", "I want", "please remember"
	rePreference = regexp.MustCompile(`(?i)(?:I\s+prefer|don'?t\s+do|always\s+|never\s+|I\s+want|please\s+remember)\s+.{5,120}`)

	// Technical facts: "the API is", "endpoint is", "version is", "uses X for Y"
	reTechFact = regexp.MustCompile(`(?i)(?:the\s+API\s+is|endpoint\s+is|version\s+is|uses?\s+\S+\s+for)\s+.{3,120}`)

	// URLs
	reURL = regexp.MustCompile(`https?://[^\s)<>]{8,200}`)

	// File paths (Unix-style and common project paths)
	reFilePath = regexp.MustCompile(`(?:^|\s)([/~][\w./-]{5,120}|[\w-]+/[\w./-]{5,80})`)

	// Dates in common formats
	reDate = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)
)

// ExtractiveMemoryFallback extracts key information from conversation history
// using regex patterns. Used as a safety net when LLM-based memory flush
// returns NO_REPLY or produces no output, preventing context loss during compaction.
func ExtractiveMemoryFallback(history []providers.Message) string {
	if len(history) == 0 {
		return ""
	}

	// Collect only user and assistant content (skip system, tool)
	var texts []string
	for _, msg := range history {
		if msg.Role == "user" || msg.Role == "assistant" {
			if content := strings.TrimSpace(msg.Content); content != "" {
				texts = append(texts, content)
			}
		}
	}
	if len(texts) == 0 {
		return ""
	}

	combined := strings.Join(texts, "\n")

	// Extract by category, dedup with a set
	decisions := extractUnique(reDecision, combined)
	preferences := extractUnique(rePreference, combined)

	// Technical facts = regex matches + URLs + dates
	techFacts := extractUnique(reTechFact, combined)
	for _, u := range extractUnique(reURL, combined) {
		techFacts = appendIfAbsent(techFacts, u)
	}
	for _, fp := range extractUniqueSubmatch(reFilePath, combined, 1) {
		techFacts = appendIfAbsent(techFacts, fp)
	}
	for _, d := range extractUnique(reDate, combined) {
		techFacts = appendIfAbsent(techFacts, d)
	}

	// Build output — only include non-empty sections
	if len(decisions) == 0 && len(techFacts) == 0 && len(preferences) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Extracted Context (auto-saved before compaction)\n")

	if len(decisions) > 0 {
		sb.WriteString("\n### Decisions\n")
		for _, d := range decisions {
			sb.WriteString("- ")
			sb.WriteString(strings.TrimSpace(d))
			sb.WriteByte('\n')
		}
	}

	if len(techFacts) > 0 {
		sb.WriteString("\n### Key Facts\n")
		for _, f := range techFacts {
			sb.WriteString("- ")
			sb.WriteString(strings.TrimSpace(f))
			sb.WriteByte('\n')
		}
	}

	if len(preferences) > 0 {
		sb.WriteString("\n### User Preferences\n")
		for _, p := range preferences {
			sb.WriteString("- ")
			sb.WriteString(strings.TrimSpace(p))
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}

// extractUnique returns deduplicated matches from a regex pattern.
func extractUnique(re *regexp.Regexp, text string) []string {
	matches := re.FindAllString(text, -1)
	return dedup(matches)
}

// extractUniqueSubmatch returns deduplicated submatch captures (by group index).
func extractUniqueSubmatch(re *regexp.Regexp, text string, group int) []string {
	matches := re.FindAllStringSubmatch(text, -1)
	var results []string
	for _, m := range matches {
		if group < len(m) {
			s := strings.TrimSpace(m[group])
			if s != "" {
				results = append(results, s)
			}
		}
	}
	return dedup(results)
}

// dedup removes duplicate strings while preserving order.
func dedup(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	var result []string
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

// appendIfAbsent appends s to slice only if not already present.
func appendIfAbsent(slice []string, s string) []string {
	for _, existing := range slice {
		if existing == s {
			return slice
		}
	}
	return append(slice, s)
}
