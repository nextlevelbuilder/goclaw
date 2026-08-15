package bootstrap

import "strings"

// ParseTriggerWords extracts the agent's trigger-word aliases from IDENTITY.md.
//
// Trigger words let an agent respond in group chats when named by an alias
// (e.g. "Alice") without an explicit @mention. They are declared in IDENTITY.md
// using the existing Key: Value convention, comma-separated:
//
//	Trigger words: Alice, Boss, Chief
//
// The key match is case-insensitive and tolerates the markdown bullet form
// (`- **Trigger words:** …`) and the singular "Trigger word". Returns nil when
// no trigger-word line is present.
func ParseTriggerWords(identityContent string) []string {
	for line := range strings.SplitSeq(identityContent, "\n") {
		line = strings.TrimSpace(line)
		// Strip markdown bullet + bold markers: "- **Trigger words:** x" → "Trigger words:** x".
		line = strings.TrimPrefix(line, "-")
		line = strings.TrimSpace(line)
		line = strings.ReplaceAll(line, "*", "")

		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:idx]))
		if key != "trigger words" && key != "trigger word" {
			continue
		}

		var words []string
		for w := range strings.SplitSeq(line[idx+1:], ",") {
			if w = strings.TrimSpace(w); w != "" {
				words = append(words, w)
			}
		}
		return words
	}
	return nil
}
