package pg

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	codeAlphabet         = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	codeLength           = 8
	codeTTL              = 60 * time.Minute
	pairedDeviceTTL      = 30 * 24 * time.Hour // 30 days
	maxPendingPerAccount = 3
)

// PGPairingStore implements store.PairingStore backed by Postgres.
type PGPairingStore struct {
	db        *sql.DB
	onRequest func(code, senderID, channel, chatID string)
}

func NewPGPairingStore(db *sql.DB) *PGPairingStore {
	return &PGPairingStore{db: db}
}

// SetOnRequest sets a callback fired after a new pairing request is created.
func (s *PGPairingStore) SetOnRequest(cb func(code, senderID, channel, chatID string)) {
	s.onRequest = cb
}

func (s *PGPairingStore) RequestPairing(ctx context.Context, senderID, channel, chatID, accountID string, metadata map[string]string) (string, error) {
	tid := tenantIDForInsert(ctx)

	// Prune expired
	s.db.ExecContext(ctx, "DELETE FROM pairing_requests WHERE expires_at < $1", time.Now())

	// Check max pending (per tenant)
	var count int64
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pairing_requests WHERE account_id = $1 AND tenant_id = $2", accountID, tid).Scan(&count)
	if count >= maxPendingPerAccount {
		return "", fmt.Errorf("max pending pairing requests (%d) exceeded", maxPendingPerAccount)
	}

	// Check existing (per tenant)
	var existingCode string
	err := s.db.QueryRowContext(ctx, "SELECT code FROM pairing_requests WHERE sender_id = $1 AND channel = $2 AND tenant_id = $3", senderID, channel, tid).Scan(&existingCode)
	if err == nil {
		return existingCode, nil
	}

	metaJSON := []byte("{}")
	if len(metadata) > 0 {
		metaJSON, _ = json.Marshal(metadata)
	}

	code := generatePairingCode()
	now := time.Now()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO pairing_requests (id, code, sender_id, channel, chat_id, account_id, expires_at, created_at, metadata, tenant_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		uuid.Must(uuid.NewV7()), code, senderID, channel, chatID, accountID, now.Add(codeTTL), now, metaJSON, tid,
	)
	if err != nil {
		return "", fmt.Errorf("create pairing request: %w", err)
	}
	if s.onRequest != nil {
		go s.onRequest(code, senderID, channel, chatID)
	}
	return code, nil
}

// ApprovePairing looks up by code (globally unique random token) and creates paired device.
// The approver's tenant context determines paired_devices.tenant_id.
func (s *PGPairingStore) ApprovePairing(ctx context.Context, code, approvedBy string) (*store.PairedDeviceData, error) {
	// Prune expired
	s.db.ExecContext(ctx, "DELETE FROM pairing_requests WHERE expires_at < $1", time.Now())

	var reqID uuid.UUID
	var senderID, channel, chatID string
	var metaJSON []byte
	var reqTenantID uuid.UUID
	// Code lookup is cross-tenant (random token, approver is WS/HTTP user).
	// Also check expires_at to close race between prune DELETE and this SELECT.
	err := s.db.QueryRowContext(ctx,
		"SELECT id, sender_id, channel, chat_id, COALESCE(metadata, '{}'), tenant_id FROM pairing_requests WHERE code = $1 AND expires_at > NOW()", code,
	).Scan(&reqID, &senderID, &channel, &chatID, &metaJSON, &reqTenantID)
	if err != nil {
		return nil, fmt.Errorf("pairing code %s not found or expired", code)
	}

	// Remove from pending
	s.db.ExecContext(ctx, "DELETE FROM pairing_requests WHERE id = $1", reqID)

	// Add to paired — use the request's tenant (the channel that initiated pairing)
	now := time.Now()
	expiresAt := now.Add(pairedDeviceTTL)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO paired_devices (id, sender_id, channel, chat_id, paired_by, paired_at, metadata, expires_at, tenant_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		uuid.Must(uuid.NewV7()), senderID, channel, chatID, approvedBy, now, metaJSON, expiresAt, reqTenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("create paired device: %w", err)
	}

	var meta map[string]string
	if len(metaJSON) > 0 {
		json.Unmarshal(metaJSON, &meta)
	}

	return &store.PairedDeviceData{
		SenderID: senderID,
		Channel:  channel,
		ChatID:   chatID,
		PairedAt: now.UnixMilli(),
		PairedBy: approvedBy,
		Metadata: meta,
	}, nil
}

func (s *PGPairingStore) DenyPairing(ctx context.Context, code string) error {
	// Code lookup is cross-tenant (random token)
	result, err := s.db.ExecContext(ctx, "DELETE FROM pairing_requests WHERE code = $1", code)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("pairing code %s not found or expired", code)
	}
	return nil
}

func (s *PGPairingStore) RevokePairing(ctx context.Context, senderID, channel string) error {
	tid := tenantIDForInsert(ctx)
	result, err := s.db.ExecContext(ctx, "DELETE FROM paired_devices WHERE sender_id = $1 AND channel = $2 AND tenant_id = $3", senderID, channel, tid)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("paired device not found: %s/%s", channel, senderID)
	}
	return nil
}

func (s *PGPairingStore) IsPaired(ctx context.Context, senderID, channel string) (bool, error) {
	tid := tenantIDForInsert(ctx)
	var count int64
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM paired_devices WHERE sender_id = $1 AND channel = $2 AND tenant_id = $3 AND (expires_at IS NULL OR expires_at > NOW())",
		senderID, channel, tid,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("pairing check query: %w", err)
	}
	return count > 0, nil
}

func (s *PGPairingStore) ListPending(ctx context.Context) []store.PairingRequestData {
	tid := tenantIDForInsert(ctx)

	// Prune expired
	s.db.ExecContext(ctx, "DELETE FROM pairing_requests WHERE expires_at < $1", time.Now())

	rows, err := s.db.QueryContext(ctx,
		`SELECT code, sender_id, channel, chat_id, account_id, created_at, expires_at, COALESCE(metadata, '{}')
		 FROM pairing_requests WHERE tenant_id = $1 ORDER BY created_at DESC`, tid)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []store.PairingRequestData
	for rows.Next() {
		var d store.PairingRequestData
		var createdAt, expiresAt time.Time
		var metaJSON []byte
		if err := rows.Scan(&d.Code, &d.SenderID, &d.Channel, &d.ChatID, &d.AccountID, &createdAt, &expiresAt, &metaJSON); err != nil {
			continue
		}
		d.CreatedAt = createdAt.UnixMilli()
		d.ExpiresAt = expiresAt.UnixMilli()
		if len(metaJSON) > 0 {
			json.Unmarshal(metaJSON, &d.Metadata)
		}
		result = append(result, d)
	}
	if result == nil {
		return []store.PairingRequestData{}
	}
	return result
}

func (s *PGPairingStore) ListPaired(ctx context.Context) []store.PairedDeviceData {
	tid := tenantIDForInsert(ctx)

	// Prune expired paired devices
	s.db.ExecContext(ctx, "DELETE FROM paired_devices WHERE expires_at IS NOT NULL AND expires_at < NOW()")

	rows, err := s.db.QueryContext(ctx,
		`SELECT sender_id, channel, chat_id, paired_by, paired_at, COALESCE(metadata, '{}')
		 FROM paired_devices WHERE tenant_id = $1 ORDER BY paired_at DESC`, tid)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []store.PairedDeviceData
	for rows.Next() {
		var d store.PairedDeviceData
		var pairedAt time.Time
		var metaJSON []byte
		if err := rows.Scan(&d.SenderID, &d.Channel, &d.ChatID, &d.PairedBy, &pairedAt, &metaJSON); err != nil {
			continue
		}
		d.PairedAt = pairedAt.UnixMilli()
		if len(metaJSON) > 0 {
			json.Unmarshal(metaJSON, &d.Metadata)
		}
		result = append(result, d)
	}
	if result == nil {
		return []store.PairedDeviceData{}
	}
	return result
}

func generatePairingCode() string {
	b := make([]byte, codeLength)
	rand.Read(b)
	code := make([]byte, codeLength)
	for i := range code {
		code[i] = codeAlphabet[int(b[i])%len(codeAlphabet)]
	}
	return string(code)
}
