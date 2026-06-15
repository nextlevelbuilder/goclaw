package bitrix24

import (
	"regexp"
	"strings"
)

// Openline (Bitrix Open Channel) relays an external connector user's message
// into the operator chat with a sender tag at the very start of the text. The
// trailing number is the connector-side user id the connector needs in order to
// route a reply back to the correct external person. Two bracket layouts are
// seen in the wild:
//
//	[Thân Công Huy #1623524631958449211]: <msg>   — id inside the brackets
//	[Thân Công Huy] #1623524631958449211: <msg>   — id after the brackets
//
// Plain Bitrix24 group chats never carry this tag — they use
// "[USER=<id>]Name[/USER]" BBCode mentions, which handle.go converts to
// "@Name (ID:<id>)". Matching this exact shape therefore scopes the echo
// feature to openline without needing a separate per-channel config flag.
//
// We accept BOTH input layouts and always emit the canonical "[name] #id:"
// form, so the reply prefix is stable regardless of which layout the connector
// happens to deliver.

// openlineSenderPrefixPatterns matches the openline sender tag anchored at the
// start of the message. Group 1 = display name (non-greedy so it stops at the
// first " #<digits>" boundary), group 2 = connector user id (digits only).
var openlineSenderPrefixPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\[(.+?)\s+#(\d+)\]:\s*`), // [name #id]:
	regexp.MustCompile(`^\[(.+?)\]\s+#(\d+):\s*`), // [name] #id:
}

// extractOpenlineSenderPrefix detects the openline sender tag at the start of
// text. On a match it returns the canonical "[name] #id:" prefix plus the
// message body with the tag (and following whitespace) removed. On no match it
// returns ("", text) unchanged so callers can treat it as a cheap no-op.
func extractOpenlineSenderPrefix(text string) (prefix, rest string) {
	for _, re := range openlineSenderPrefixPatterns {
		m := re.FindStringSubmatch(text)
		if m == nil {
			continue
		}
		name := strings.TrimSpace(m[1])
		id := m[2]
		if name == "" || id == "" {
			continue
		}
		return "[" + name + "] #" + id + ":", text[len(m[0]):]
	}
	return "", text
}
