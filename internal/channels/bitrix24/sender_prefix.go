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
// The trailing number is the message id (msgId) the connector uses to quote /
// route a reply back to the right external message, so it must survive into the
// echo. (Empirically it is a per-message id, not a per-user id — the same
// external person produces a different number on each message.)
// The trailing ":" is optional because the connector dropped it in its latest
// format. The bare name-only layout is far more generic, so it is only
// recognised for Open Channel sessions (see allowNameOnly).
//
// A newer connector build prepends a SECOND number — the external person's
// stable uid — ahead of the msgId: "[Name] #uid #msgId <msg>". That richer
// 3-token layout is parsed by parseOpenlineSenderTag (below), which keeps the
// uid and msgId separate so callers can scope per-participant identity. The
// single-number layouts above remain the legacy/back-compat path.
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

// OpenlineSenderTagFormat classifies which connector sender-tag layout was
// found at the start of an Open Channel message.
type OpenlineSenderTagFormat int

const (
	// TagFormatNone means no recognised sender tag was present.
	TagFormatNone OpenlineSenderTagFormat = iota
	// TagFormatNameOnly is "[Name] <msg>" — a display name with no number.
	TagFormatNameOnly
	// TagFormatLegacy is "[Name] #msgId <msg>" — a single number, which is the
	// connector message id (NOT a user id). This is the format production
	// connectors ship today.
	TagFormatLegacy
	// TagFormatThreeToken is "[Name] #uid #msgId <msg>" — two numbers, where the
	// first is the external person's stable uid and the second is the message
	// id. Only the newer connector build emits this.
	TagFormatThreeToken
)

// OpenlineSenderTag is the structured result of parsing a connector sender tag.
// UID is populated only for TagFormatThreeToken; MsgID is populated for both the
// legacy and three-token layouts. Rest is the message body with the tag removed.
type OpenlineSenderTag struct {
	Name   string
	UID    string
	MsgID  string
	Format OpenlineSenderTagFormat
	Rest   string
}

// threeTokenPattern matches "[Name] #uid #msgId" — two consecutive #digits
// tokens after the bracketed name. Group 1 = name, group 2 = uid, group 3 =
// msgId. The trailing ":" is optional to match the legacy patterns' tolerance.
var threeTokenPattern = regexp.MustCompile(`^\[(.+?)\]\s+#(\d+)\s+#(\d+):?\s*`)

// parseOpenlineSenderTag classifies the connector sender tag at the start of an
// Open Channel message and returns its parts separated. Precedence is
// most-specific first: three-token (uid + msgId) → id-bearing legacy (msgId
// only) → name-only → none. On no match it returns {Format: TagFormatNone,
// Rest: text} so callers can treat it as a cheap no-op.
//
// Callers must gate identity derivation on the message source (IS_CONNECTOR=Y)
// themselves — this function only parses the shape; an operator who types a
// look-alike tag would parse identically. Only call it for Open Channel
// sessions; ordinary group chats never carry these tags.
func parseOpenlineSenderTag(text string) OpenlineSenderTag {
	// Three-token: "[Name] #uid #msgId" — try first so the second number isn't
	// mistaken for the body by the single-number legacy patterns.
	if m := threeTokenPattern.FindStringSubmatch(text); m != nil {
		if name := strings.TrimSpace(m[1]); name != "" && m[2] != "" && m[3] != "" {
			return OpenlineSenderTag{
				Name:   name,
				UID:    m[2],
				MsgID:  m[3],
				Format: TagFormatThreeToken,
				Rest:   strings.TrimSpace(text[len(m[0]):]),
			}
		}
	}

	// Legacy id-bearing: "[Name] #msgId" or "[Name #msgId]" — one number = msgId.
	for _, re := range openlineSenderPrefixPatterns {
		if m := re.FindStringSubmatch(text); m != nil {
			if name := strings.TrimSpace(m[1]); name != "" && m[2] != "" {
				return OpenlineSenderTag{
					Name:   name,
					MsgID:  m[2],
					Format: TagFormatLegacy,
					Rest:   strings.TrimSpace(text[len(m[0]):]),
				}
			}
		}
	}

	// Name-only: "[Name] <msg>".
	if m := nameOnlySenderPrefixPattern.FindStringSubmatch(text); m != nil {
		if name := strings.TrimSpace(m[1]); name != "" {
			return OpenlineSenderTag{
				Name:   name,
				Format: TagFormatNameOnly,
				Rest:   strings.TrimSpace(text[len(m[0]):]),
			}
		}
	}

	return OpenlineSenderTag{Format: TagFormatNone, Rest: text}
}
