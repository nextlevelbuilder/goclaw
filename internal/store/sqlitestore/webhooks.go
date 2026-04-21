//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// compile-time interface assertion
var _ store.WebhookStore = (*SQLiteWebhookStore)(nil)

// SQLiteWebhookStore implements store.WebhookStore backed by SQLite.
type SQLiteWebhookStore struct {
	db *sql.DB
}

// NewSQLiteWebhookStore creates a new SQLite-backed webhook store.
func NewSQLiteWebhookStore(db *sql.DB) *SQLiteWebhookStore {
	return &SQLiteWebhookStore{db: db}
}

// scanSQLiteWebhookRow scans a single webhooks row from SQLite into WebhookData.
// scopes/ip_allowlist are stored as JSON TEXT; bool columns as INTEGER (0/1).
func scanSQLiteWebhookRow(row interface {
	Scan(dest ...any) error
}) (*store.WebhookData, error) {
	var w store.WebhookData
	var agentID, channelID *uuid.UUID
	// secret_prefix, created_by are nullable TEXT columns.
	var secretPrefix, createdBy *string
	var scopesRaw, ipAllowlistRaw []byte
	var lastUsedAt nullSqliteTime
	createdAt, updatedAt := scanTimePair()

	err := row.Scan(
		&w.ID, &w.TenantID, &agentID,
		&w.Name, &w.Kind, &secretPrefix, &w.SecretHash, &w.EncryptedSecret,
		&scopesRaw, &channelID, &w.RateLimitPerMin, &ipAllowlistRaw,
		&w.RequireHMAC, &w.LocalhostOnly, &w.Revoked, &createdBy,
		createdAt, updatedAt, &lastUsedAt,
	)
	if err != nil {
		return nil, err
	}
	w.CreatedAt = createdAt.Time
	w.UpdatedAt = updatedAt.Time
	if lastUsedAt.Valid {
		w.LastUsedAt = &lastUsedAt.Time
	}
	w.AgentID = agentID
	w.ChannelID = channelID
	if secretPrefix != nil {
		w.SecretPrefix = *secretPrefix
	}
	if createdBy != nil {
		w.CreatedBy = *createdBy
	}
	scanJSONStringArray(scopesRaw, &w.Scopes)
	scanJSONStringArray(ipAllowlistRaw, &w.IPAllowlist)
	return &w, nil
}

// sqliteWebhookSelectCols is the canonical SELECT column list for webhooks in SQLite.
const sqliteWebhookSelectCols = `id, tenant_id, agent_id, name, kind, secret_prefix, secret_hash, encrypted_secret,
	scopes, channel_id, rate_limit_per_min, ip_allowlist,
	require_hmac, localhost_only, revoked, created_by,
	created_at, updated_at, last_used_at`

func (s *SQLiteWebhookStore) Create(ctx context.Context, w *store.WebhookData) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO webhooks
		 (id, tenant_id, agent_id, name, kind, secret_prefix, secret_hash, encrypted_secret,
		  scopes, channel_id, rate_limit_per_min, ip_allowlist,
		  require_hmac, localhost_only, revoked, created_by, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		w.ID, w.TenantID, nilUUID(w.AgentID),
		w.Name, w.Kind, nilStr(w.SecretPrefix), w.SecretHash, w.EncryptedSecret,
		jsonStringArray(w.Scopes), nilUUID(w.ChannelID), w.RateLimitPerMin, jsonStringArray(w.IPAllowlist),
		w.RequireHMAC, w.LocalhostOnly, w.Revoked,
		nilStr(w.CreatedBy), w.CreatedAt, w.UpdatedAt,
	)
	return err
}

func (s *SQLiteWebhookStore) GetByID(ctx context.Context, id uuid.UUID) (*store.WebhookData, error) {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+sqliteWebhookSelectCols+`
		 FROM webhooks
		 WHERE id = ? AND tenant_id = ?`,
		id, tid,
	)
	return scanSQLiteWebhookRow(row)
}

func (s *SQLiteWebhookStore) GetByHash(ctx context.Context, secretHash string) (*store.WebhookData, error) {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+sqliteWebhookSelectCols+`
		 FROM webhooks
		 WHERE secret_hash = ? AND tenant_id = ? AND revoked = 0`,
		secretHash, tid,
	)
	return scanSQLiteWebhookRow(row)
}

// GetByHashUnscoped looks up a webhook by secret_hash without a tenant filter.
// Intended only for WebhookAuthMiddleware pre-auth resolution before tenant context
// has been established. Downstream queries must remain tenant-scoped.
func (s *SQLiteWebhookStore) GetByHashUnscoped(ctx context.Context, secretHash string) (*store.WebhookData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+sqliteWebhookSelectCols+`
		 FROM webhooks
		 WHERE secret_hash = ? AND revoked = 0`,
		secretHash,
	)
	return scanSQLiteWebhookRow(row)
}

// GetByIDUnscoped looks up a webhook by UUID without a tenant filter.
// Intended only for WebhookAuthMiddleware HMAC pre-auth resolution.
func (s *SQLiteWebhookStore) GetByIDUnscoped(ctx context.Context, id uuid.UUID) (*store.WebhookData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+sqliteWebhookSelectCols+`
		 FROM webhooks
		 WHERE id = ? AND revoked = 0`,
		id,
	)
	return scanSQLiteWebhookRow(row)
}

func (s *SQLiteWebhookStore) List(ctx context.Context, f store.WebhookListFilter) ([]store.WebhookData, error) {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return nil, err
	}

	q := `SELECT ` + sqliteWebhookSelectCols + ` FROM webhooks WHERE tenant_id = ?`
	args := []any{tid}

	if f.AgentID != nil {
		q += ` AND agent_id = ?`
		args = append(args, *f.AgentID)
	}
	q += ` ORDER BY created_at DESC`

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	q += ` LIMIT ? OFFSET ?`
	args = append(args, limit, f.Offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []store.WebhookData
	for rows.Next() {
		w, scanErr := scanSQLiteWebhookRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *w)
	}
	return out, rows.Err()
}

func (s *SQLiteWebhookStore) Update(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	return execMapUpdateWhereTenant(ctx, s.db, "webhooks", updates, id, tid)
}

func (s *SQLiteWebhookStore) RotateSecret(ctx context.Context, id uuid.UUID, newSecretHash, newPrefix, newEncryptedSecret string) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE webhooks SET secret_hash = ?, secret_prefix = ?, encrypted_secret = ?, updated_at = ?
		 WHERE id = ? AND tenant_id = ?`,
		newSecretHash, newPrefix, newEncryptedSecret, time.Now(), id, tid,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLiteWebhookStore) Revoke(ctx context.Context, id uuid.UUID) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE webhooks SET revoked = 1, updated_at = ?
		 WHERE id = ? AND tenant_id = ?`,
		time.Now(), id, tid,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLiteWebhookStore) TouchLastUsed(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE webhooks SET last_used_at = ? WHERE id = ?`,
		time.Now(), id,
	)
	return err
}
