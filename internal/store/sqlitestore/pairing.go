//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	codeAlphabet         = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	codeLength           = 8
	codeTTL              = 60 * time.Minute
	pairedDeviceTTL      = 30 * 24 * time.Hour
	maxPendingPerAccount = 3
)

// SQLitePairingStore implements store.PairingStore backed by SQLite.
type SQLitePairingStore struct {
	db        *sql.DB
	onRequest func(code, senderID, channel, chatID string)
}

func NewSQLitePairingStore(db *sql.DB) *SQLitePairingStore {
	return &SQLitePairingStore{db: db}
}

func (s *SQLitePairingStore) SetOnRequest(cb func(code, senderID, channel, chatID string)) {
	s.onRequest = cb
}

func (s *SQLitePairingStore) RequestPairing(ctx context.Context, senderID, channel, chatID, accountID string, metadata map[string]string) (string, error) {
	tid := tenantIDForInsert(ctx)
	now := time.Now().Round(0) // Strip monotonic clock for correct SQLite string comparison

	s.db.ExecContext(ctx, "DELETE FROM pairing_requests WHERE expires_at < ?", now)

	var count int64
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pairing_requests WHERE account_id = ? AND tenant_id = ?", accountID, tid).Scan(&count)
	if count >= maxPendingPerAccount {
		return "", fmt.Errorf("max pending pairing requests (%d) exceeded", maxPendingPerAccount)
	}

	var existingCode string
	err := s.db.QueryRowContext(ctx, "SELECT code FROM pairing_requests WHERE sender_id = ? AND channel = ? AND tenant_id = ?", senderID, channel, tid).Scan(&existingCode)
	if err == nil {
		return existingCode, nil
	}

	metaJSON := []byte("{}")
	if len(metadata) > 0 {
		metaJSON, _ = json.Marshal(metadata)
	}

	code := generatePairingCode()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO pairing_requests (id, code, sender_id, channel, chat_id, account_id, expires_at, created_at, metadata, tenant_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uuid.Must(uuid.NewV7()), code, senderID, channel, chatID, accountID, now.Add(codeTTL).Round(0), now, metaJSON, tid,
	)
	if err != nil {
		return "", fmt.Errorf("create pairing request: %w", err)
	}
	if s.onRequest != nil {
		go s.onRequest(code, senderID, channel, chatID)
	}
	return code, nil
}

func (s *SQLitePairingStore) ApprovePairing(ctx context.Context, code, approvedBy string) (*store.PairedDeviceData, error) {
	now := time.Now().Round(0)
	s.db.ExecContext(ctx, "DELETE FROM pairing_requests WHERE expires_at < ?", now)

	var reqID uuid.UUID
	var senderID, channel, chatID string
	var metaJSON []byte
	var reqTenantID uuid.UUID

	err := s.db.QueryRowContext(ctx,
		"SELECT id, sender_id, channel, chat_id, COALESCE(metadata, '{}'), tenant_id FROM pairing_requests WHERE code = ?", code,
	).Scan(&reqID, &senderID, &channel, &chatID, &metaJSON, &reqTenantID)
	if err != nil {
		return nil, fmt.Errorf("pairing code %s not found or expired", code)
	}

	s.db.ExecContext(ctx, "DELETE FROM pairing_requests WHERE id = ?", reqID)

	expiresAt := now.Add(pairedDeviceTTL)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO paired_devices (id, sender_id, channel, chat_id, paired_by, paired_at, metadata, expires_at, tenant_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
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

func (s *SQLitePairingStore) DenyPairing(ctx context.Context, code string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM pairing_requests WHERE code = ?", code)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("pairing code %s not found or expired", code)
	}
	return nil
}

func (s *SQLitePairingStore) RevokePairing(ctx context.Context, senderID, channel string) error {
	tid := tenantIDForInsert(ctx)
	result, err := s.db.ExecContext(ctx, "DELETE FROM paired_devices WHERE sender_id = ? AND channel = ? AND tenant_id = ?", senderID, channel, tid)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("paired device not found: %s/%s", channel, senderID)
	}
	return nil
}

func (s *SQLitePairingStore) IsPaired(ctx context.Context, senderID, channel string) (bool, error) {
	tid := tenantIDForInsert(ctx)
	var count int64
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM paired_devices WHERE sender_id = ? AND channel = ? AND tenant_id = ? AND (expires_at IS NULL OR expires_at > ?)",
		senderID, channel, tid, time.Now().Round(0),
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("pairing check query: %w", err)
	}
	return count > 0, nil
}

func (s *SQLitePairingStore) ListPending(ctx context.Context) []store.PairingRequestData {
	tid := tenantIDForInsert(ctx)
	now := time.Now().Round(0) // Strip monotonic clock for correct SQLite string comparison

	s.db.ExecContext(ctx, "DELETE FROM pairing_requests WHERE expires_at < ?", now)

	rows, err := s.db.QueryContext(ctx,
		`SELECT code, sender_id, channel, chat_id, account_id, created_at, expires_at, COALESCE(metadata, '{}')
		 FROM pairing_requests WHERE tenant_id = ? ORDER BY created_at DESC`, tid)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []store.PairingRequestData
	for rows.Next() {
		var d store.PairingRequestData
		var createdAtStr, expiresAtStr string
		var metaJSON []byte
		if err := rows.Scan(&d.Code, &d.SenderID, &d.Channel, &d.ChatID, &d.AccountID, &createdAtStr, &expiresAtStr, &metaJSON); err != nil {
			slog.Warn("pairing: scan error", "error", err)
			continue
		}
		d.CreatedAt = parseTimeToMillis(createdAtStr)
		d.ExpiresAt = parseTimeToMillis(expiresAtStr)
		if len(metaJSON) > 0 {
			json.Unmarshal(metaJSON, &d.Metadata)
		}
		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("pairing: list pending iteration error", "error", err)
	}
	if result == nil {
		return []store.PairingRequestData{}
	}
	return result
}

func (s *SQLitePairingStore) ListPaired(ctx context.Context) []store.PairedDeviceData {
	tid := tenantIDForInsert(ctx)
	now := time.Now().Round(0)

	s.db.ExecContext(ctx, "DELETE FROM paired_devices WHERE expires_at IS NOT NULL AND expires_at < ?", now)

	rows, err := s.db.QueryContext(ctx,
		`SELECT sender_id, channel, chat_id, paired_by, paired_at, COALESCE(metadata, '{}')
		 FROM paired_devices WHERE tenant_id = ? ORDER BY paired_at DESC`, tid)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []store.PairedDeviceData
	for rows.Next() {
		var d store.PairedDeviceData
		var pairedAtStr string
		var metaJSON []byte
		if err := rows.Scan(&d.SenderID, &d.Channel, &d.ChatID, &d.PairedBy, &pairedAtStr, &metaJSON); err != nil {
			slog.Warn("pairing: scan paired error", "error", err)
			continue
		}
		d.PairedAt = parseTimeToMillis(pairedAtStr)
		if len(metaJSON) > 0 {
			json.Unmarshal(metaJSON, &d.Metadata)
		}
		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("pairing: list paired iteration error", "error", err)
	}
	if result == nil {
		return []store.PairedDeviceData{}
	}
	return result
}

// parseTimeToMillis parses a Go time.Time string (possibly with monotonic clock suffix)
// from SQLite and returns Unix milliseconds. Falls back to 0 on parse failure.
func parseTimeToMillis(s string) int64 {
	// Strip monotonic clock suffix "m=+xxx" if present
	if idx := strings.Index(s, " m="); idx > 0 {
		s = s[:idx]
	}
	// Try standard Go formats
	for _, layout := range []string{
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05.999999999 -0700 -07",
		"2006-01-02 15:04:05.999999 -0700 -07",
		"2006-01-02T15:04:05.999999999-07:00",
		time.RFC3339Nano,
		time.RFC3339,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UnixMilli()
		}
	}
	slog.Warn("pairing: failed to parse time", "value", s)
	return 0
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
