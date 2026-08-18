//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteContractStore implements store.ContractStore backed by SQLite.
type SQLiteContractStore struct {
	db *sql.DB
}

// NewSQLiteContractStore creates a SQLite-backed contract record store.
func NewSQLiteContractStore(db *sql.DB) *SQLiteContractStore {
	return &SQLiteContractStore{db: db}
}

// CreateContractRecord inserts a new multi-agent record. Kind is required and
// validated; Status defaults to draft; Body must be valid JSON (stored as
// TEXT). The tenant comes from the context tenant (master fallback).
func (s *SQLiteContractStore) CreateContractRecord(ctx context.Context, rec *store.ContractRecord) error {
	if rec.ID == uuid.Nil {
		rec.ID = store.GenNewID()
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now()
	}
	if rec.Kind == "" {
		return fmt.Errorf("create contract record: kind required")
	}
	if !validContractRecordKind(rec.Kind) {
		return fmt.Errorf("create contract record: unknown kind %q", rec.Kind)
	}
	if rec.Status == "" {
		rec.Status = store.ContractRecordDraft
	}
	if !store.ValidContractRecordStatus(rec.Status) {
		return fmt.Errorf("create contract record: unknown status %q", rec.Status)
	}
	rec.TenantID = tenantIDForInsert(ctx)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO multi_agent_records (id, tenant_id, run_id, kind, body, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		rec.ID.String(), rec.TenantID.String(), nilStr(rec.RunID), rec.Kind,
		bodyString(rec.Body), rec.Status, rec.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("create contract record: insert: %w", err)
	}
	return nil
}

// GetContractRecord returns one record by id, scoped to the context tenant.
func (s *SQLiteContractStore) GetContractRecord(ctx context.Context, id uuid.UUID) (*store.ContractRecord, error) {
	where, args := buildSQLiteContractWhere(ctx, id)
	q := `SELECT id, tenant_id, run_id, kind, body, status, created_at
		 FROM multi_agent_records` + where
	var row contractRecordRow
	if err := scanContractRecordRow(s.db.QueryRowContext(ctx, q, args...).Scan, &row); err != nil {
		return nil, err
	}
	got := row.toStore()
	return &got, nil
}

// ListContractRecords returns record rows filtered by opts, scoped to the
// context tenant. Newest first.
func (s *SQLiteContractStore) ListContractRecords(ctx context.Context, opts store.ContractRecordListOpts) ([]store.ContractRecord, error) {
	where, args := buildSQLiteContractListWhere(ctx, opts)
	limit := opts.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, tenant_id, run_id, kind, body, status, created_at
		 FROM multi_agent_records` + where +
		` ORDER BY created_at DESC, id DESC` +
		fmt.Sprintf(" LIMIT %d OFFSET %d", limit, opts.Offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []store.ContractRecord
	for rows.Next() {
		var row contractRecordRow
		if err := scanContractRecordRow(rows.Scan, &row); err != nil {
			return nil, err
		}
		items = append(items, row.toStore())
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if items == nil {
		items = []store.ContractRecord{}
	}
	return items, nil
}

// UpdateContractRecordStatus transitions one record's status, scoped to the
// context tenant. The target status must be a known status.
func (s *SQLiteContractStore) UpdateContractRecordStatus(ctx context.Context, id uuid.UUID, status string) error {
	if !store.ValidContractRecordStatus(status) {
		return fmt.Errorf("update contract record: unknown status %q", status)
	}
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE multi_agent_records SET status = ? WHERE id = ? AND tenant_id = ?`,
		status, id.String(), tid.String())
	if err != nil {
		return fmt.Errorf("update contract record: %w", err)
	}
	return nil
}

// validContractRecordKind reports whether k is one of the known record kinds.
func validContractRecordKind(k string) bool {
	switch k {
	case store.ContractRecordHandoff, store.ContractRecordJury,
		store.ContractRecordCompetition, store.ContractRecordNegotiation:
		return true
	}
	return false
}

// bodyString returns the JSON body to persist, normalizing an empty value to a
// valid empty JSON object.
func bodyString(body string) string {
	if body == "" {
		return "{}"
	}
	return body
}

// buildSQLiteContractWhere scopes a single-record read. Fails closed
// (WHERE 1=0) when a tenant ID is required but absent from the context.
func buildSQLiteContractWhere(ctx context.Context, id uuid.UUID) (string, []any) {
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return " WHERE 1=0", nil
		}
		return " WHERE id = ? AND tenant_id = ?", []any{id.String(), tenantID.String()}
	}
	return " WHERE id = ?", []any{id.String()}
}

// buildSQLiteContractListWhere scopes a record list read. Fails closed
// (WHERE 1=0) when a tenant ID is required but absent from the context.
func buildSQLiteContractListWhere(ctx context.Context, opts store.ContractRecordListOpts) (string, []any) {
	var conditions []string
	var args []any
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return " WHERE 1=0", nil
		}
		conditions = append(conditions, "tenant_id = ?")
		args = append(args, tenantID.String())
	}
	if opts.RunID != "" {
		conditions = append(conditions, "run_id = ?")
		args = append(args, opts.RunID)
	}
	if opts.Kind != "" {
		conditions = append(conditions, "kind = ?")
		args = append(args, opts.Kind)
	}
	if opts.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, opts.Status)
	}
	if len(conditions) == 0 {
		return " WHERE 1=0", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

type contractRecordRow struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	RunID     sql.NullString
	Kind      string
	Body      string
	Status    sql.NullString
	CreatedAt sqliteTime
}

// scanContractRecordRow scans one multi_agent_records row, converting SQLite's
// TEXT UUIDs and timestamps into Go values.
func scanContractRecordRow(scan func(dest ...any) error, r *contractRecordRow) error {
	var id, tenantID string
	var createdAt sqliteTime
	if err := scan(
		&id, &tenantID, &r.RunID, &r.Kind, &r.Body, &r.Status, &createdAt,
	); err != nil {
		return err
	}
	r.ID = uuid.MustParse(id)
	r.TenantID = uuid.MustParse(tenantID)
	r.CreatedAt = createdAt
	return nil
}

func (r contractRecordRow) toStore() store.ContractRecord {
	return store.ContractRecord{
		ID:        r.ID,
		TenantID:  r.TenantID,
		RunID:     r.RunID.String,
		Kind:      r.Kind,
		Body:      r.Body,
		Status:    r.Status.String,
		CreatedAt: r.CreatedAt.Time,
	}
}
