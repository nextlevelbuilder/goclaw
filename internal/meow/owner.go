package meow

import "strings"

// OwnerGate authorizes owner-only commands (publish, post-now, approve,
// live-edit). It is CLOSED BY DEFAULT: until an owner chat id is configured AND
// that id has been round-trip verified (the bot DM'd the owner and the owner
// acked), no sender is allowed. Publish authority over live channels must never
// rest on an unverified or merely-derived identity.
type OwnerGate struct {
	OwnerChatID string // configured/derived owner Telegram chat id
	Verified    bool   // set true only after a successful round-trip DM
}

// Allowed reports whether senderID may run owner-gated commands. The zero-value
// gate (no owner, not verified) denies everyone.
func (g OwnerGate) Allowed(senderID string) bool {
	owner := strings.TrimSpace(g.OwnerChatID)
	if owner == "" || !g.Verified {
		return false
	}
	return strings.TrimSpace(senderID) == owner
}
