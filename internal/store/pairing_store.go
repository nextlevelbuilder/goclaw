package store

import "context"

// PairingRequest represents a pending pairing code.
type PairingRequestData struct {
	Code      string            `json:"code" db:"code"`
	SenderID  string            `json:"sender_id" db:"sender_id"`
	Channel   string            `json:"channel" db:"channel"`
	ChatID    string            `json:"chat_id" db:"chat_id"`
	AccountID string            `json:"account_id" db:"account_id"`
	CreatedAt int64             `json:"created_at" db:"created_at"`
	ExpiresAt int64             `json:"expires_at" db:"expires_at"`
	Metadata  map[string]string `json:"metadata,omitempty" db:"metadata"`
}

// PairedDeviceData represents an approved pairing.
type PairedDeviceData struct {
	SenderID string            `json:"sender_id" db:"sender_id"`
	Channel  string            `json:"channel" db:"channel"`
	ChatID   string            `json:"chat_id" db:"chat_id"`
	PairedAt int64             `json:"paired_at" db:"paired_at"`
	PairedBy string            `json:"paired_by" db:"paired_by"`
	Metadata map[string]string `json:"metadata,omitempty" db:"metadata"`
}

// PairingStore manages device pairing.
type PairingStore interface {
	RequestPairing(ctx context.Context, senderID, channel, chatID, accountID string, metadata map[string]string) (string, error)
	ApprovePairing(ctx context.Context, code, approvedBy string) (*PairedDeviceData, error)
	DenyPairing(ctx context.Context, code string) error
	RevokePairing(ctx context.Context, senderID, channel string) error
	IsPaired(ctx context.Context, senderID, channel string) (bool, error)
	ListPending(ctx context.Context) []PairingRequestData
	ListPaired(ctx context.Context) []PairedDeviceData
	// MigrateGroupChatID updates all references from oldChatID to newChatID
	// across paired_devices, sessions, and channel_contacts within a transaction.
	// Scoped by tenant_id and channel. Idempotent (safe to call multiple times).
	MigrateGroupChatID(ctx context.Context, channel, oldChatID, newChatID string) error
}
