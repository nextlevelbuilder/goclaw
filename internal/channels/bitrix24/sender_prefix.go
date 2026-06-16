package bitrix24

import (
	"regexp"
	"strings"
)

// Openline (Bitrix Open Channel) relays an external connector user's message
// into the operator chat with a sender tag at the very start of the text. The
// connector has shipped a few layouts over time; we accept them all and
// normalise the id-bearing ones to "[name] #id" and the bare one to "[name]":
//
//	[Thân Công Huy #1623524631958449211]: <msg>   — id inside the brackets, colon
//	[Thân Công Huy #1623524631958449211] <msg>    — id inside the brackets, no colon
//	[Thân Công Huy] #1623524631958449211: <msg>   — id after the brackets, colon
//	[Thân Công Huy] #1623524631958449211 <msg>    — id after the brackets, no colon
//	[Thân Công Huy] <msg>                          — name only (no id)
//
// The trailing number is the connector-side user id the connector uses to route
// a reply back to the right external person, so it must survive into the echo.
// The trailing ":" is optional because the connector dropped it in its latest
// format. The bare name-only layout is far more generic, so it is only
// recognised for Open Channel sessions (see allowNameOnly).
//
// Plain Bitrix24 group chats never carry these tags — they use
// "[USER=<id>]Name[/USER]" BBCode mentions, which handle.go converts to
// "@Name (ID:<id>)".

// openlineSenderPrefixPatterns match the id-bearing sender tag anchored at the
// start of the message. Group 1 = display name, group 2 = connector user id
// (digits only). The colon after the tag is optional (`:?`) — newer connector
// builds omit it.
var openlineSenderPrefixPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\[(.+?)\s+#(\d+)\]:?\s*`), // [name #id]:  or  [name #id]
	regexp.MustCompile(`^\[(.+?)\]\s+#(\d+):?\s*`), // [name] #id:  or  [name] #id
}

// nameOnlySenderPrefixPattern matches the shortest "[name] " tag — a display
// name in brackets followed by whitespace, with no connector id. Group 1 =
// display name. Generic on purpose, so callers gate it behind allowNameOnly to
// keep it to Open Channel sessions only.
var nameOnlySenderPrefixPattern = regexp.MustCompile(`^\[([^\]]+?)\]\s+`)

// extractOpenlineSenderPrefix detects the openline sender tag at the start of
// text. On a match it returns the canonical prefix ("[name] #id" for id-bearing
// tags, "[name]" for the name-only tag) plus the message body with the tag (and
// following whitespace) removed. On no match it returns ("", text) unchanged so
// callers can treat it as a cheap no-op.
//
// allowNameOnly enables the generic name-only "[name] " layout; pass it only
// for Open Channel sessions. The id-bearing layouts are always tried first (and
// take precedence) because their numeric id makes them unambiguous — this also
// stops the name-only pattern from clipping just "[name] " off a "[name] #id"
// tag and dropping the id.
func extractOpenlineSenderPrefix(text string, allowNameOnly bool) (prefix, rest string) {
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
		return "[" + name + "] #" + id, text[len(m[0]):]
	}

	if allowNameOnly {
		if m := nameOnlySenderPrefixPattern.FindStringSubmatch(text); m != nil {
			if name := strings.TrimSpace(m[1]); name != "" {
				return "[" + name + "]", text[len(m[0]):]
			}
		}
	}

	return "", text
}
