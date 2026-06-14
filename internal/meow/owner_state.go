package meow

import (
	"context"
	"strings"
)

// Owner-gate persistence keys (stored in the system config store).
const (
	CfgOwnerChatID   = "meow.owner_chat_id"
	CfgOwnerVerified = "meow.owner_verified"
)

// OwnerConfigStore is the subset of the system config store the owner gate
// needs. store.SystemConfigStore satisfies it.
type OwnerConfigStore interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
}

// LoadOwnerGate reads the owner-gate state from config. It is closed by default:
// missing config (or verified != "true") yields a gate that denies everyone.
func LoadOwnerGate(ctx context.Context, cs OwnerConfigStore) OwnerGate {
	if cs == nil {
		return OwnerGate{}
	}
	id, _ := cs.Get(ctx, CfgOwnerChatID)
	verified, _ := cs.Get(ctx, CfgOwnerVerified)
	return OwnerGate{
		OwnerChatID: strings.TrimSpace(id),
		Verified:    strings.TrimSpace(verified) == "true",
	}
}

// SetOwnerChatID records the configured owner chat id WITHOUT verifying it. The
// gate stays closed until VerifyRoundTrip succeeds.
func SetOwnerChatID(ctx context.Context, cs OwnerConfigStore, chatID string) error {
	return cs.Set(ctx, CfgOwnerChatID, strings.TrimSpace(chatID))
}

// VerifyRoundTrip records verification only if senderID matches the configured
// owner chat id (proving the sender controls that chat). Returns whether
// verification was recorded. A mismatch (or no configured owner) records
// nothing and the gate stays closed.
func VerifyRoundTrip(ctx context.Context, cs OwnerConfigStore, senderID string) (bool, error) {
	if cs == nil {
		return false, nil
	}
	id, _ := cs.Get(ctx, CfgOwnerChatID)
	owner := strings.TrimSpace(id)
	if owner == "" || strings.TrimSpace(senderID) != owner {
		return false, nil
	}
	if err := cs.Set(ctx, CfgOwnerVerified, "true"); err != nil {
		return false, err
	}
	return true, nil
}
