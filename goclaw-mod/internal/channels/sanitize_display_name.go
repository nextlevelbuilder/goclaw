package channels

import "strings"

// SanitizeDisplayName strips newlines and markdown heading markers from a
// channel-provided display name to prevent injection into markdown templates
// (e.g. USER.md). Caps length at 100 runes.
func SanitizeDisplayName(name string) string {
	name = strings.ReplaceAll(name, "\n", " ")
	name = strings.ReplaceAll(name, "\r", " ")
	name = strings.ReplaceAll(name, "#", "")
	name = strings.TrimSpace(name)
	if len([]rune(name)) > 100 {
		name = string([]rune(name)[:100])
	}
	return name
}
