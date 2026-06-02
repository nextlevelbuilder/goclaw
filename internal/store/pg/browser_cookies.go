package pg

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

type PGBrowserCookieStore struct {
	db     *sql.DB
	encKey string
}

func NewPGBrowserCookieStore(db *sql.DB, encKey string) *PGBrowserCookieStore {
	return &PGBrowserCookieStore{db: db, encKey: encKey}
}

func (s *PGBrowserCookieStore) Upsert(ctx context.Context, scope store.BrowserCookieScope, cookies []store.BrowserCookie) (int, error) {
	if s.encKey == "" {
		return 0, store.ErrBrowserCookieEncryptionRequired
	}
	if err := scope.Validate(); err != nil {
		return 0, err
	}
	now := time.Now().UTC()
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
		res, err := s.db.ExecContext(ctx, `
			INSERT INTO browser_cookies (
				id, tenant_id, user_id, agent_id, domain, name, path, encrypted_value,
				secure, http_only, same_site, expires_at, source, created_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
			ON CONFLICT (tenant_id, user_id, agent_id, domain, path, name)
			DO UPDATE SET encrypted_value = EXCLUDED.encrypted_value,
				secure = EXCLUDED.secure,
				http_only = EXCLUDED.http_only,
				same_site = EXCLUDED.same_site,
				expires_at = EXCLUDED.expires_at,
				source = EXCLUDED.source,
				updated_at = EXCLUDED.updated_at`,
			id, scope.TenantID, scope.UserID, scope.AgentID, c.Domain, c.Name, c.Path, encrypted,
			c.Secure, c.HTTPOnly, c.SameSite, c.ExpiresAt, c.Source, now, now,
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

func (s *PGBrowserCookieStore) List(ctx context.Context, scope store.BrowserCookieScope, filter store.BrowserCookieFilter) ([]store.BrowserCookie, error) {
	if s.encKey == "" {
		return nil, store.ErrBrowserCookieEncryptionRequired
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	query := `SELECT id, tenant_id, user_id, agent_id, domain, name, path, encrypted_value,
		secure, http_only, same_site, expires_at, source, created_at, updated_at
		FROM browser_cookies
		WHERE tenant_id = $1 AND user_id = $2 AND agent_id = $3
		  AND (expires_at IS NULL OR expires_at > NOW())`
	args := []any{scope.TenantID, scope.UserID, scope.AgentID}
	next := 4
	if filter.Domain != "" {
		query += fmt.Sprintf(" AND domain = $%d", next)
		args = append(args, strings.ToLower(strings.TrimSpace(filter.Domain)))
		next++
	}
	if filter.Name != "" {
		query += fmt.Sprintf(" AND name = $%d", next)
		args = append(args, strings.TrimSpace(filter.Name))
		next++
	}
	if filter.Path != "" {
		query += fmt.Sprintf(" AND path = $%d", next)
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

func (s *PGBrowserCookieStore) Delete(ctx context.Context, scope store.BrowserCookieScope, filter store.BrowserCookieFilter) (int, error) {
	if err := scope.Validate(); err != nil {
		return 0, err
	}
	query := `DELETE FROM browser_cookies WHERE tenant_id = $1 AND user_id = $2 AND agent_id = $3`
	args := []any{scope.TenantID, scope.UserID, scope.AgentID}
	next := 4
	if filter.Domain != "" {
		query += fmt.Sprintf(" AND domain = $%d", next)
		args = append(args, strings.ToLower(strings.TrimSpace(filter.Domain)))
		next++
	}
	if filter.Name != "" {
		query += fmt.Sprintf(" AND name = $%d", next)
		args = append(args, strings.TrimSpace(filter.Name))
		next++
	}
	if filter.Path != "" {
		query += fmt.Sprintf(" AND path = $%d", next)
		args = append(args, strings.TrimSpace(filter.Path))
	}
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *PGBrowserCookieStore) scanRows(rows *sql.Rows) ([]store.BrowserCookie, error) {
	var out []store.BrowserCookie
	for rows.Next() {
		var c store.BrowserCookie
		var encrypted string
		if err := rows.Scan(&c.ID, &c.TenantID, &c.UserID, &c.AgentID, &c.Domain, &c.Name, &c.Path,
			&encrypted, &c.Secure, &c.HTTPOnly, &c.SameSite, &c.ExpiresAt, &c.Source, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		value, err := crypto.Decrypt(encrypted, s.encKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt browser cookie: %w", err)
		}
		c.Value = value
		out = append(out, c)
	}
	return out, rows.Err()
}
