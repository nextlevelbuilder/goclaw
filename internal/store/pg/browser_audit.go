package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type pgBrowserAuditStore struct {
	db *sql.DB
}

func NewBrowserAuditStore(db *sql.DB) *pgBrowserAuditStore {
	return &pgBrowserAuditStore{db: db}
}

func (s *pgBrowserAuditStore) Log(ctx context.Context, e *store.BrowserAuditEntry) error {
	if e.TenantID == "" {
		e.TenantID = tenantIDForInsert(ctx).String()
	}
	args := e.Args
	if args == nil {
		args = json.RawMessage("{}")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO browser_audit_log
		    (tenant_id, user_id, agent_id, session_id, action, target_id, args, result, error_text, duration_ms)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		e.TenantID,
		nilIfEmpty(e.UserID),
		nilIfEmpty(e.AgentID),
		nilIfEmpty(e.SessionID),
		e.Action,
		e.TargetID,
		args,
		e.Result,
		e.ErrorText,
		e.DurationMs,
	)
	return err
}

func (s *pgBrowserAuditStore) List(ctx context.Context, tenantID string, opts store.AuditListOpts) ([]*store.BrowserAuditEntry, int, error) {
	if opts.Limit <= 0 {
		opts.Limit = 50
	}

	// Build WHERE clause
	where := "WHERE tenant_id = $1"
	args := []any{tenantID}
	n := 2

	if opts.SessionID != "" {
		where += fmt.Sprintf(" AND session_id = $%d", n)
		args = append(args, opts.SessionID)
		n++
	}
	if opts.Action != "" {
		where += fmt.Sprintf(" AND action = $%d", n)
		args = append(args, opts.Action)
		n++
	}

	// Count total
	var total int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM browser_audit_log `+where, args...,
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Fetch rows
	query := fmt.Sprintf(
		`SELECT id, tenant_id, user_id, agent_id, session_id, action, target_id,
		        args, result, error_text, duration_ms, created_at
		 FROM browser_audit_log %s
		 ORDER BY created_at DESC
		 LIMIT $%d OFFSET $%d`, where, n, n+1)
	args = append(args, opts.Limit, opts.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var entries []*store.BrowserAuditEntry
	for rows.Next() {
		var e store.BrowserAuditEntry
		var userID, agentID, sessionID sql.NullString
		var argsJSON []byte
		if err := rows.Scan(&e.ID, &e.TenantID, &userID, &agentID, &sessionID,
			&e.Action, &e.TargetID, &argsJSON, &e.Result, &e.ErrorText,
			&e.DurationMs, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		if userID.Valid {
			e.UserID = userID.String
		}
		if agentID.Valid {
			e.AgentID = agentID.String
		}
		if sessionID.Valid {
			e.SessionID = sessionID.String
		}
		e.Args = argsJSON
		entries = append(entries, &e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}
