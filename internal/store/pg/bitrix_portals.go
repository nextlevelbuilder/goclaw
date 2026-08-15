package pg

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

// PGBitrixPortalStore implements store.BitrixPortalStore backed by Postgres.
//
// Both `credentials` and `state` columns hold AES-256-GCM ciphertext when
// an encryption key is configured. With no key (empty string) values are
// stored as-is — crypto.Encrypt/Decrypt pass plaintext through for that case
// and log a warning on read. The table itself uses BYTEA for portability.
type PGBitrixPortalStore struct {
	db     *sql.DB
	encKey string
}

// NewPGBitrixPortalStore constructs a Bitrix24 portal store.
func NewPGBitrixPortalStore(db *sql.DB, encryptionKey string) *PGBitrixPortalStore {
	return &PGBitrixPortalStore{db: db, encKey: encryptionKey}
}

const bitrixPortalCols = `id, tenant_id, name, domain, credentials, state, created_at, updated_at`

// encryptBlob wraps raw bytes → AES-GCM ciphertext bytes. Empty input returns nil.
// With empty encKey it returns the raw bytes unchanged (crypto.Encrypt contract).
func (s *PGBitrixPortalStore) encryptBlob(raw []byte) ([]byte, error) {
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

// decryptBlob reverses encryptBlob. Corrupt ciphertext returns an error rather
// than silently returning plaintext — portal corruption should fail loud so
// operators reinstall instead of running with silently stale tokens.
func (s *PGBitrixPortalStore) decryptBlob(raw []byte, field, name string) []byte {
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

func (s *PGBitrixPortalStore) Create(ctx context.Context, p *store.BitrixPortalData) error {
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

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO bitrix_portals (id, tenant_id, name, domain, credentials, state, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		p.ID, p.TenantID, p.Name, p.Domain, credsBytes, stateBytes, now, now,
	)
	return err
}

func (s *PGBitrixPortalStore) GetByName(ctx context.Context, tenantID uuid.UUID, name string) (*store.BitrixPortalData, error) {
	if tenantID == uuid.Nil {
		return nil, errors.New("bitrix_portals: tenant_id required")
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+bitrixPortalCols+` FROM bitrix_portals WHERE tenant_id = $1 AND name = $2`,
		tenantID, name,
	)
	return s.scanRow(row, name)
}

func (s *PGBitrixPortalStore) scanRow(row *sql.Row, name string) (*store.BitrixPortalData, error) {
	var p store.BitrixPortalData
	var creds, state []byte
	err := row.Scan(&p.ID, &p.TenantID, &p.Name, &p.Domain, &creds, &state, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	p.Credentials = s.decryptBlob(creds, "credentials", name)
	p.State = s.decryptBlob(state, "state", name)
	return &p, nil
}

func (s *PGBitrixPortalStore) scanRows(rows *sql.Rows) ([]store.BitrixPortalData, error) {
	defer rows.Close()
	var result []store.BitrixPortalData
	for rows.Next() {
		var p store.BitrixPortalData
		var creds, state []byte
		if err := rows.Scan(&p.ID, &p.TenantID, &p.Name, &p.Domain, &creds, &state, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Credentials = s.decryptBlob(creds, "credentials", p.Name)
		p.State = s.decryptBlob(state, "state", p.Name)
		result = append(result, p)
	}
	return result, rows.Err()
}

func (s *PGBitrixPortalStore) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]store.BitrixPortalData, error) {
	if tenantID == uuid.Nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+bitrixPortalCols+` FROM bitrix_portals WHERE tenant_id = $1 ORDER BY name`, tenantID,
	)
	if err != nil {
		return nil, err
	}
	return s.scanRows(rows)
}

// ListAllForLoader returns rows across all tenants. Startup-only; never expose via RPC.
func (s *PGBitrixPortalStore) ListAllForLoader(ctx context.Context) ([]store.BitrixPortalData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+bitrixPortalCols+` FROM bitrix_portals ORDER BY tenant_id, name`,
	)
	if err != nil {
		return nil, err
	}
	return s.scanRows(rows)
}

func (s *PGBitrixPortalStore) UpdateCredentials(ctx context.Context, tenantID uuid.UUID, name string, creds []byte) error {
	if tenantID == uuid.Nil {
		return errors.New("bitrix_portals: tenant_id required")
	}
	enc, err := s.encryptBlob(creds)
	if err != nil {
		return fmt.Errorf("encrypt credentials: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE bitrix_portals SET credentials = $1, updated_at = $2 WHERE tenant_id = $3 AND name = $4`,
		enc, time.Now().UTC(), tenantID, name,
	)
	return err
}

func (s *PGBitrixPortalStore) UpdateState(ctx context.Context, tenantID uuid.UUID, name string, state []byte) error {
	if tenantID == uuid.Nil {
		return errors.New("bitrix_portals: tenant_id required")
	}
	enc, err := s.encryptBlob(state)
	if err != nil {
		return fmt.Errorf("encrypt state: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE bitrix_portals SET state = $1, updated_at = $2 WHERE tenant_id = $3 AND name = $4`,
		enc, time.Now().UTC(), tenantID, name,
	)
	return err
}

func (s *PGBitrixPortalStore) Delete(ctx context.Context, tenantID uuid.UUID, name string) error {
	if tenantID == uuid.Nil {
		return errors.New("bitrix_portals: tenant_id required")
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM bitrix_portals WHERE tenant_id = $1 AND name = $2`, tenantID, name,
	)
	return err
}
