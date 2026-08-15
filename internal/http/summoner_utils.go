package http

import (
	"strings"
)

var identityFieldPrefixes = []string{
	"- **Name:**",
	"- **Creature:**",
	"- **Purpose:**",
	"- **Vibe:**",
	"- **Emoji:**",
	"- **Avatar:**",
	"Name:",
	"Creature:",
	"Purpose:",
	"Vibe:",
	"Emoji:",
	"Avatar:",
}

// extractIdentityName extracts the Name field from IDENTITY.md content.
// Accepts only an inline Name value and ignores markdown field spillover.
func extractIdentityName(content string) string {
	if content == "" {
		return ""
	}

	for rawLine := range strings.SplitSeq(content, "\n") {
		line := strings.TrimSpace(rawLine)
		switch {
		case strings.HasPrefix(line, "- **Name:**"):
			return normalizeIdentityName(strings.TrimSpace(strings.TrimPrefix(line, "- **Name:**")))
		case strings.HasPrefix(line, "Name:"):
			return normalizeIdentityName(strings.TrimSpace(strings.TrimPrefix(line, "Name:")))
		}
	}

	return ""
}

func normalizeIdentityName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || looksLikeIdentityField(value) {
		return ""
	}

	for {
		next := trimMarkdownWrapper(value)
		if next == value {
			return value
		}
		value = strings.TrimSpace(next)
		if value == "" || looksLikeIdentityField(value) {
			return ""
		}
	}
}

func looksLikeIdentityField(value string) bool {
	for _, prefix := range identityFieldPrefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func trimMarkdownWrapper(value string) string {
	wrappers := [][2]string{
		{"**", "**"},
		{"__", "__"},
		{"`", "`"},
		{"*", "*"},
		{"_", "_"},
	}
	for _, wrapper := range wrappers {
		if strings.HasPrefix(value, wrapper[0]) && strings.HasSuffix(value, wrapper[1]) && len(value) > len(wrapper[0])+len(wrapper[1]) {
			return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, wrapper[0]), wrapper[1]))
		}
	}
	return value
}

// suffixString returns the last n runes of s.
func suffixString(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}

// truncateUTF8 truncates s to at most maxLen runes, appending "…" if truncated.
func truncateUTF8(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}

// parseFileResponse extracts file contents and frontmatter from XML-tagged LLM output.
// Frontmatter is stored under the special key "__frontmatter__".
func parseFileResponse(content string) map[string]string {
	files := make(map[string]string)
	matches := fileTagRe.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		name := strings.TrimSpace(m[1])
		body := strings.TrimSpace(m[2])
		if name != "" && body != "" {
			files[name] = body
		}
	}
	// Extract frontmatter tag if present
	if fm := frontmatterTagRe.FindStringSubmatch(content); len(fm) > 1 {
		if trimmed := strings.TrimSpace(fm[1]); trimmed != "" {
			files[frontmatterKey] = trimmed
		}
	}
	return files
}
