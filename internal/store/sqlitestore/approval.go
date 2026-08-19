//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteApprovalStore implements store.ApprovalStore backed by SQLite.
// payload is stored as TEXT (canonical JSON); UUIDs and timestamps as TEXT.
type SQLiteApprovalStore struct {
	db *sql.DB
}

// NewSQLiteApprovalStore creates a SQLite-backed approval store.
func NewSQLiteApprovalStore(db *sql.DB) *SQLiteApprovalStore {
	return &SQLiteApprovalStore{db: db}
}

// CreateRequest inserts a new approval request. Status defaults to pending;
// the tenant comes from the context tenant (master fallback).
func (s *SQLiteApprovalStore) CreateRequest(ctx context.Context, req *store.ApprovalRequest) error {
	if req.ID == uuid.Nil {
		req.ID = store.GenNewID()
	}
	now := time.Now()
	if req.CreatedAt.IsZero() {
		req.CreatedAt = now
	}
	if req.Status == "" {
		req.Status = store.ApprovalStatusPending
	}
	if req.TimeoutSeconds == 0 {
		req.TimeoutSeconds = 120
	}
	req.TenantID = tenantIDForInsert(ctx)

	// payload is a NOT NULL column; normalize nil to "{}".
	payload := jsonOrEmpty(req.Payload)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO approval_requests (id, tenant_id, agent_id, requester_id, requester_type, action_type, payload, command, status, decision, decided_by, allow_once, allow_always, created_at, decided_at, expired_at, timeout_seconds)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.ID.String(), req.TenantID.String(), nilUUIDStr(req.AgentID), nilUUIDStr(req.RequesterID),
		req.RequesterType, req.ActionType, string(payload), nilStr(req.Command), req.Status,
		nilStr(req.Decision), nilUUIDStr(req.DecidedBy), sqlBoolNil(req.AllowOnce), sqlBoolNil(req.AllowAlways),
		req.CreatedAt.Format(time.RFC3339Nano), nilTimeStr(req.DecidedAt), nilTimeStr(req.ExpiredAt),
		req.TimeoutSeconds,
	)
	if err != nil {
		return fmt.Errorf("create approval request: %w", err)
	}
	return nil
}

// ListPending returns pending (unresolved) requests for the given tenant,
// oldest first.
func (s *SQLiteApprovalStore) ListPending(ctx context.Context, tenantID uuid.UUID) ([]store.ApprovalRequest, error) {
	if tenantID == uuid.Nil {
		tenantID = tenantIDForInsert(ctx)
	}
	q := `SELECT id, tenant_id, agent_id, requester_id, requester_type, action_type, payload, command, status, decision, decided_by, allow_once, allow_always, created_at, decided_at, expired_at, timeout_seconds
		 FROM approval_requests
		 WHERE tenant_id = ? AND status = ?
		 ORDER BY created_at ASC, id ASC`
	rows, err := s.db.QueryContext(ctx, q, tenantID.String(), store.ApprovalStatusPending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []store.ApprovalRequest
	for rows.Next() {
		var r sqliteApprovalRow
		if err := scanApprovalRow(rows.Scan, &r); err != nil {
			return nil, err
		}
		items = append(items, r.toStore())
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if items == nil {
		items = []store.ApprovalRequest{}
	}
	return items, nil
}

// Resolve transitions a pending request to a terminal state, scoped to the
// context tenant. A second resolve of the same row returns
// ErrApprovalAlreadyResolved.
func (s *SQLiteApprovalStore) Resolve(ctx context.Context, id uuid.UUID, decision string, decidedBy *uuid.UUID, allowOnce, allowAlways bool) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	status := store.ApprovalStatusApproved
	if decision == store.ApprovalDecisionDeny {
		status = store.ApprovalStatusDenied
	}
	now := time.Now().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx,
		`UPDATE approval_requests
		 SET status = ?, decision = ?, decided_by = ?, allow_once = ?, allow_always = ?, decided_at = ?
		 WHERE id = ? AND tenant_id = ? AND status = ?`,
		status, decision, nilUUIDStr(decidedBy), sqlBoolNil(allowOnce), sqlBoolNil(allowAlways), now,
		id.String(), tid.String(), store.ApprovalStatusPending)
	if err != nil {
		return fmt.Errorf("resolve approval request: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("resolve approval request: rows affected: %w", err)
	}
	if affected == 0 {
		return store.ErrApprovalAlreadyResolved
	}
	return nil
}

// GetByID returns one request by id, scoped to the context tenant. Returns
// (nil, nil) when the row is missing or belongs to another tenant.
func (s *SQLiteApprovalStore) GetByID(ctx context.Context, id uuid.UUID) (*store.ApprovalRequest, error) {
	where, args := buildApprovalGetWhere(ctx, id)
	q := `SELECT id, tenant_id, agent_id, requester_id, requester_type, action_type, payload, command, status, decision, decided_by, allow_once, allow_always, created_at, decided_at, expired_at, timeout_seconds
		 FROM approval_requests` + where
	var r sqliteApprovalRow
	if err := scanApprovalRow(s.db.QueryRowContext(ctx, q, args...).Scan, &r); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	got := r.toStore()
	return &got, nil
}

// MarkExpired transitions a pending request to expired, scoped to the context
// tenant. Already-terminal rows are left untouched.
func (s *SQLiteApprovalStore) MarkExpired(ctx context.Context, id uuid.UUID) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	now := time.Now().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx,
		`UPDATE approval_requests SET status = ?, decided_at = ? WHERE id = ? AND tenant_id = ? AND status = ?`,
		store.ApprovalStatusExpired, now, id.String(), tid.String(), store.ApprovalStatusPending)
	if err != nil {
		return fmt.Errorf("mark approval expired: %w", err)
	}
	return nil
}

// ListHistory returns resolved requests for the given tenant, newest first,
// optionally filtered by status.
func (s *SQLiteApprovalStore) ListHistory(ctx context.Context, tenantID uuid.UUID, opts store.ApprovalListOpts) ([]store.ApprovalRequest, error) {
	if tenantID == uuid.Nil {
		tenantID = tenantIDForInsert(ctx)
	}
	limit := opts.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	var conditions []string
	var args []any
	conditions = append(conditions, "tenant_id = ?")
	args = append(args, tenantID.String())
	if opts.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, opts.Status)
	}
	args = append(args, limit, opts.Offset)
	q := `SELECT id, tenant_id, agent_id, requester_id, requester_type, action_type, payload, command, status, decision, decided_by, allow_once, allow_always, created_at, decided_at, expired_at, timeout_seconds
		 FROM approval_requests
		 WHERE ` + strings.Join(conditions, " AND ") +
		` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []store.ApprovalRequest
	for rows.Next() {
		var r sqliteApprovalRow
		if err := scanApprovalRow(rows.Scan, &r); err != nil {
			return nil, err
		}
		items = append(items, r.toStore())
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if items == nil {
		items = []store.ApprovalRequest{}
	}
	return items, nil
}

// buildApprovalGetWhere scopes a single-row read. Fails closed (WHERE 1=0)
// when a tenant ID is required but absent from the context.
func buildApprovalGetWhere(ctx context.Context, id uuid.UUID) (string, []any) {
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return " WHERE 1=0", nil
		}
		return " WHERE id = ? AND tenant_id = ?", []any{id.String(), tenantID.String()}
	}
	return " WHERE id = ?", []any{id.String()}
}

// sqlBoolNil converts a Go bool to a nullable value for SQLite boolean
// columns (stored as INTEGER). False maps to NULL (zero value) to keep the
// column unset until a resolve actually happens.
func sqlBoolNil(v bool) *bool {
	if !v {
		return nil
	}
	return &v
}

// nilTimeStr converts a *time.Time to a timestamp string, or nil when zero/unset.
func nilTimeStr(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.Format(time.RFC3339Nano)
}

type sqliteApprovalRow struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	AgentID        sql.NullString
	RequesterID    sql.NullString
	RequesterType  sql.NullString
	ActionType     string
	Payload        sql.NullString
	Command        sql.NullString
	Status         string
	Decision       sql.NullString
	DecidedBy      sql.NullString
	AllowOnce      sql.NullBool
	AllowAlways    sql.NullBool
	CreatedAt      sqliteTime
	DecidedAt      nullSqliteTime
	ExpiredAt      nullSqliteTime
	TimeoutSeconds int
}

func scanApprovalRow(scan func(dest ...any) error, r *sqliteApprovalRow) error {
	var id, tenantID string
	var createdAt sqliteTime
	if err := scan(
		&id, &tenantID, &r.AgentID, &r.RequesterID, &r.RequesterType, &r.ActionType,
		&r.Payload, &r.Command, &r.Status, &r.Decision, &r.DecidedBy,
		&r.AllowOnce, &r.AllowAlways, &createdAt, &r.DecidedAt, &r.ExpiredAt, &r.TimeoutSeconds,
	); err != nil {
		return err
	}
	r.ID = uuid.MustParse(id)
	r.TenantID = uuid.MustParse(tenantID)
	r.CreatedAt = createdAt
	return nil
}

func (r sqliteApprovalRow) toStore() store.ApprovalRequest {
	req := store.ApprovalRequest{
		ID:             r.ID,
		TenantID:       r.TenantID,
		AgentID:        parseNullableUUID(r.AgentID),
		RequesterID:    parseNullableUUID(r.RequesterID),
		RequesterType:  r.RequesterType.String,
		ActionType:     r.ActionType,
		Payload:        nil,
		Command:        r.Command.String,
		Status:         r.Status,
		Decision:       r.Decision.String,
		DecidedBy:      parseNullableUUID(r.DecidedBy),
		AllowOnce:      r.AllowOnce.Bool,
		AllowAlways:    r.AllowAlways.Bool,
		CreatedAt:      r.CreatedAt.Time,
		TimeoutSeconds: r.TimeoutSeconds,
	}
	if r.Payload.Valid {
		req.Payload = []byte(r.Payload.String)
	}
	if r.DecidedAt.Valid {
		t := r.DecidedAt.Time
		req.DecidedAt = &t
	}
	if r.ExpiredAt.Valid {
		t := r.ExpiredAt.Time
		req.ExpiredAt = &t
	}
	return req
}

// parseNullableUUID parses a nullable SQLite TEXT UUID into a *uuid.UUID.
func parseNullableUUID(s sql.NullString) *uuid.UUID {
	if !s.Valid || s.String == "" {
		return nil
	}
	if parsed, err := uuid.Parse(s.String); err == nil {
		return &parsed
	}
	return nil
}