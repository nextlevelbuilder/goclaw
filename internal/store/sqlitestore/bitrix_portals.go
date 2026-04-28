//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteBitrixPortalStore implements store.BitrixPortalStore backed by SQLite.
// Mirrors PGBitrixPortalStore's encrypt-on-write / decrypt-on-read contract.
type SQLiteBitrixPortalStore struct {
	db     *sql.DB
	encKey string
}

func NewSQLiteBitrixPortalStore(db *sql.DB, encryptionKey string) *SQLiteBitrixPortalStore {
	return &SQLiteBitrixPortalStore{db: db, encKey: encryptionKey}
}

const bitrixPortalCols = `id, tenant_id, name, domain, credentials, state, created_at, updated_at`

func (s *SQLiteBitrixPortalStore) encryptBlob(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if s.encKey == "" {
		return raw, nil
	}
	enc, err := crypto.Encrypt(string(raw), s.encKey)
	if err != nil {
		return nil, err
	}
	return []byte(enc), nil
}

func (s *SQLiteBitrixPortalStore) decryptBlob(raw []byte, field, name string) []byte {
	if len(raw) == 0 {
		return nil
	}
	if s.encKey == "" {
		return raw
	}
	dec, err := crypto.Decrypt(string(raw), s.encKey)
	if err != nil {
		slog.Warn("bitrix_portals: decrypt failed", "field", field, "name", name, "error", err)
		return nil
	}
	return []byte(dec)
}

func (s *SQLiteBitrixPortalStore) Create(ctx context.Context, p *store.BitrixPortalData) error {
	if p == nil {
		return errors.New("bitrix_portals: nil portal")
	}
	if p.TenantID == uuid.Nil {
		return errors.New("bitrix_portals: tenant_id required")
	}
	if p.Name == "" || p.Domain == "" {
		return errors.New("bitrix_portals: name and domain required")
	}
	if p.ID == uuid.Nil {
		p.ID = store.GenNewID()
	}

	credsBytes, err := s.encryptBlob(p.Credentials)
	if err != nil {
		return fmt.Errorf("encrypt credentials: %w", err)
	}
	stateBytes, err := s.encryptBlob(p.State)
	if err != nil {
		return fmt.Errorf("encrypt state: %w", err)
	}

	now := time.Now().UTC()
	p.CreatedAt = now
	p.UpdatedAt = now
	nowStr := now.Format(time.RFC3339Nano)

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO bitrix_portals (id, tenant_id, name, domain, credentials, state, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		p.ID.String(), p.TenantID.String(), p.Name, p.Domain, credsBytes, stateBytes, nowStr, nowStr,
	)
	return err
}

func (s *SQLiteBitrixPortalStore) GetByName(ctx context.Context, tenantID uuid.UUID, name string) (*store.BitrixPortalData, error) {
	if tenantID == uuid.Nil {
		return nil, errors.New("bitrix_portals: tenant_id required")
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+bitrixPortalCols+` FROM bitrix_portals WHERE tenant_id = ? AND name = ?`,
		tenantID.String(), name,
	)
	return s.scanRow(row, name)
}

// parseBitrixTime parses RFC3339 / RFC3339Nano timestamps. Returns zero time on failure.
func parseBitrixTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// scanRow handles column types: id + tenant_id + timestamps are TEXT in SQLite,
// so we read as strings and parse into uuid.UUID / time.Time.
func (s *SQLiteBitrixPortalStore) scanRow(row *sql.Row, name string) (*store.BitrixPortalData, error) {
	var (
		idStr, tidStr          string
		createdAtStr, updatedAtStr string
		p                      store.BitrixPortalData
		creds, state           []byte
	)
	err := row.Scan(&idStr, &tidStr, &p.Name, &p.Domain, &creds, &state, &createdAtStr, &updatedAtStr)
	if err != nil {
		return nil, err
	}
	if id, err := uuid.Parse(idStr); err == nil {
		p.ID = id
	}
	if tid, err := uuid.Parse(tidStr); err == nil {
		p.TenantID = tid
	}
	p.CreatedAt = parseBitrixTime(createdAtStr)
	p.UpdatedAt = parseBitrixTime(updatedAtStr)
	p.Credentials = s.decryptBlob(creds, "credentials", name)
	p.State = s.decryptBlob(state, "state", name)
	return &p, nil
}

func (s *SQLiteBitrixPortalStore) scanRows(rows *sql.Rows) ([]store.BitrixPortalData, error) {
	defer rows.Close()
	var result []store.BitrixPortalData
	for rows.Next() {
		var (
			idStr, tidStr              string
			createdAtStr, updatedAtStr string
			p                          store.BitrixPortalData
			creds, state               []byte
		)
		if err := rows.Scan(&idStr, &tidStr, &p.Name, &p.Domain, &creds, &state, &createdAtStr, &updatedAtStr); err != nil {
			return nil, err
		}
		if id, err := uuid.Parse(idStr); err == nil {
			p.ID = id
		}
		if tid, err := uuid.Parse(tidStr); err == nil {
			p.TenantID = tid
		}
		p.CreatedAt = parseBitrixTime(createdAtStr)
		p.UpdatedAt = parseBitrixTime(updatedAtStr)
		p.Credentials = s.decryptBlob(creds, "credentials", p.Name)
		p.State = s.decryptBlob(state, "state", p.Name)
		result = append(result, p)
	}
	return result, rows.Err()
}

func (s *SQLiteBitrixPortalStore) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]store.BitrixPortalData, error) {
	if tenantID == uuid.Nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+bitrixPortalCols+` FROM bitrix_portals WHERE tenant_id = ? ORDER BY name`,
		tenantID.String(),
	)
	if err != nil {
		return nil, err
	}
	return s.scanRows(rows)
}

func (s *SQLiteBitrixPortalStore) ListAllForLoader(ctx context.Context) ([]store.BitrixPortalData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+bitrixPortalCols+` FROM bitrix_portals ORDER BY tenant_id, name`,
	)
	if err != nil {
		return nil, err
	}
	return s.scanRows(rows)
}

func (s *SQLiteBitrixPortalStore) UpdateCredentials(ctx context.Context, tenantID uuid.UUID, name string, creds []byte) error {
	if tenantID == uuid.Nil {
		return errors.New("bitrix_portals: tenant_id required")
	}
	enc, err := s.encryptBlob(creds)
	if err != nil {
		return fmt.Errorf("encrypt credentials: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE bitrix_portals SET credentials = ?, updated_at = ? WHERE tenant_id = ? AND name = ?`,
		enc, time.Now().UTC().Format(time.RFC3339Nano), tenantID.String(), name,
	)
	return err
}

func (s *SQLiteBitrixPortalStore) UpdateState(ctx context.Context, tenantID uuid.UUID, name string, state []byte) error {
	if tenantID == uuid.Nil {
		return errors.New("bitrix_portals: tenant_id required")
	}
	enc, err := s.encryptBlob(state)
	if err != nil {
		return fmt.Errorf("encrypt state: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE bitrix_portals SET state = ?, updated_at = ? WHERE tenant_id = ? AND name = ?`,
		enc, time.Now().UTC().Format(time.RFC3339Nano), tenantID.String(), name,
	)
	return err
}

func (s *SQLiteBitrixPortalStore) Delete(ctx context.Context, tenantID uuid.UUID, name string) error {
	if tenantID == uuid.Nil {
		return errors.New("bitrix_portals: tenant_id required")
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM bitrix_portals WHERE tenant_id = ? AND name = ?`, tenantID.String(), name,
	)
	return err
}
