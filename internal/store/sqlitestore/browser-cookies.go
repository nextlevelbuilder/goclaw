//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type SQLiteBrowserCookieStore struct {
	db     *sql.DB
	encKey string
}

func NewSQLiteBrowserCookieStore(db *sql.DB, encKey string) *SQLiteBrowserCookieStore {
	return &SQLiteBrowserCookieStore{db: db, encKey: encKey}
}

func (s *SQLiteBrowserCookieStore) Upsert(ctx context.Context, scope store.BrowserCookieScope, cookies []store.BrowserCookie) (int, error) {
	if s.encKey == "" {
		return 0, store.ErrBrowserCookieEncryptionRequired
	}
	if err := scope.Validate(); err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339Nano)
	count := 0
	for _, raw := range cookies {
		c := store.NormalizeBrowserCookie(raw)
		if err := store.ValidateBrowserCookie(c); err != nil {
			return count, err
		}
		encrypted, err := crypto.Encrypt(c.Value, s.encKey)
		if err != nil {
			return count, fmt.Errorf("encrypt browser cookie: %w", err)
		}
		id := c.ID
		if id == uuid.Nil {
			id = store.GenNewID()
		}
		var expiresAt any
		if c.ExpiresAt != nil {
			expiresAt = c.ExpiresAt.UTC().Format(time.RFC3339Nano)
		}
		res, err := s.db.ExecContext(ctx, `
			INSERT INTO browser_cookies (
				id, tenant_id, user_id, agent_id, domain, name, path, encrypted_value,
				secure, http_only, same_site, expires_at, source, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (tenant_id, user_id, agent_id, domain, path, name)
			DO UPDATE SET encrypted_value = excluded.encrypted_value,
				secure = excluded.secure,
				http_only = excluded.http_only,
				same_site = excluded.same_site,
				expires_at = excluded.expires_at,
				source = excluded.source,
				updated_at = excluded.updated_at`,
			id.String(), scope.TenantID.String(), scope.UserID, scope.AgentID, c.Domain, c.Name, c.Path, encrypted,
			c.Secure, c.HTTPOnly, c.SameSite, expiresAt, c.Source, nowStr, nowStr,
		)
		if err != nil {
			return count, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			count++
		}
	}
	return count, nil
}

func (s *SQLiteBrowserCookieStore) List(ctx context.Context, scope store.BrowserCookieScope, filter store.BrowserCookieFilter) ([]store.BrowserCookie, error) {
	if s.encKey == "" {
		return nil, store.ErrBrowserCookieEncryptionRequired
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	query := `SELECT id, tenant_id, user_id, agent_id, domain, name, path, encrypted_value,
		secure, http_only, same_site, expires_at, source, created_at, updated_at
		FROM browser_cookies
		WHERE tenant_id = ? AND user_id = ? AND agent_id = ?
		  AND (expires_at IS NULL OR expires_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`
	args := []any{scope.TenantID.String(), scope.UserID, scope.AgentID}
	if filter.Domain != "" {
		query += " AND domain = ?"
		args = append(args, strings.ToLower(strings.TrimSpace(filter.Domain)))
	}
	if filter.Name != "" {
		query += " AND name = ?"
		args = append(args, strings.TrimSpace(filter.Name))
	}
	if filter.Path != "" {
		query += " AND path = ?"
		args = append(args, strings.TrimSpace(filter.Path))
	}
	query += " ORDER BY domain, path, name"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanRows(rows)
}

func (s *SQLiteBrowserCookieStore) Delete(ctx context.Context, scope store.BrowserCookieScope, filter store.BrowserCookieFilter) (int, error) {
	if err := scope.Validate(); err != nil {
		return 0, err
	}
	query := `DELETE FROM browser_cookies WHERE tenant_id = ? AND user_id = ? AND agent_id = ?`
	args := []any{scope.TenantID.String(), scope.UserID, scope.AgentID}
	if filter.Domain != "" {
		query += " AND domain = ?"
		args = append(args, strings.ToLower(strings.TrimSpace(filter.Domain)))
	}
	if filter.Name != "" {
		query += " AND name = ?"
		args = append(args, strings.TrimSpace(filter.Name))
	}
	if filter.Path != "" {
		query += " AND path = ?"
		args = append(args, strings.TrimSpace(filter.Path))
	}
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLiteBrowserCookieStore) scanRows(rows *sql.Rows) ([]store.BrowserCookie, error) {
	var out []store.BrowserCookie
	for rows.Next() {
		var c store.BrowserCookie
		var id, tenantID, encrypted string
		var expiresAt nullSqliteTime
		createdAt, updatedAt := scanTimePair()
		if err := rows.Scan(&id, &tenantID, &c.UserID, &c.AgentID, &c.Domain, &c.Name, &c.Path,
			&encrypted, &c.Secure, &c.HTTPOnly, &c.SameSite, &expiresAt, &c.Source, createdAt, updatedAt); err != nil {
			return nil, err
		}
		c.ID = uuid.MustParse(id)
		c.TenantID = uuid.MustParse(tenantID)
		if expiresAt.Valid {
			c.ExpiresAt = &expiresAt.Time
		}
		c.CreatedAt = createdAt.Time
		c.UpdatedAt = updatedAt.Time
		value, err := crypto.Decrypt(encrypted, s.encKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt browser cookie: %w", err)
		}
		c.Value = value
		out = append(out, c)
	}
	return out, rows.Err()
}
