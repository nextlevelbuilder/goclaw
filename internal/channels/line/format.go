package line

import "strings"

// splitMessage splits text into chunks of at most maxLen bytes,
// preferring to break at newline boundaries.
func splitMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var parts []string
	for len(text) > 0 {
		if len(text) <= maxLen {
			parts = append(parts, text)
			break
		}
		// Try to split at the last newline within maxLen.
		cut := maxLen
		if idx := strings.LastIndex(text[:maxLen], "\n"); idx > 0 {
			cut = idx + 1
		}
		parts = append(parts, text[:cut])
		text = text[cut:]
	}
	return parts
}

// formatForLINE removes markdown formatting that LINE does not render.
func formatForLINE(text string) string {
	// Remove bold markers: **text** → text
	text = strings.ReplaceAll(text, "**", "")
	// Remove italic markers: *text* → text (single asterisks remaining after bold removal)
	// Skip — single asterisks are ambiguous and may be intentional list bullets.
	// Remove inline code: `text` → text
	text = strings.ReplaceAll(text, "`", "")
	return text
}
