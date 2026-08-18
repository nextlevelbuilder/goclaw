package pg

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGContractStore implements store.ContractStore backed by PostgreSQL.
type PGContractStore struct {
	db *sql.DB
}

// NewPGContractStore creates a PG-backed contract record store.
func NewPGContractStore(db *sql.DB) *PGContractStore {
	return &PGContractStore{db: db}
}

// CreateContractRecord inserts a new multi-agent record. Kind is required and
// validated; Status defaults to draft; Body must be valid JSON (stored as
// JSONB). The tenant comes from the context tenant (master fallback).
func (s *PGContractStore) CreateContractRecord(ctx context.Context, rec *store.ContractRecord) error {
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
		 VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7)`,
		rec.ID, rec.TenantID, nilStr(rec.RunID), rec.Kind, bodyJSON(rec.Body), rec.Status, rec.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create contract record: insert: %w", err)
	}
	return nil
}

// GetContractRecord returns one record by id, scoped to the context tenant.
func (s *PGContractStore) GetContractRecord(ctx context.Context, id uuid.UUID) (*store.ContractRecord, error) {
	where, args := buildContractRecordWhere(ctx, id)
	q := `SELECT id, tenant_id, run_id, kind, body::text AS body, status, created_at
		 FROM multi_agent_records` + where
	var row contractRecordRow
	if err := pkgSqlxDB.GetContext(ctx, &row, q, args...); err != nil {
		return nil, err
	}
	got := row.toStore()
	return &got, nil
}

// ListContractRecords returns record rows filtered by opts, scoped to the
// context tenant. Newest first.
func (s *PGContractStore) ListContractRecords(ctx context.Context, opts store.ContractRecordListOpts) ([]store.ContractRecord, error) {
	where, args := buildContractRecordListWhere(ctx, opts)
	limit := opts.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, tenant_id, run_id, kind, body::text AS body, status, created_at
		 FROM multi_agent_records` + where +
		` ORDER BY created_at DESC, id DESC` +
		fmt.Sprintf(" OFFSET %d LIMIT %d", opts.Offset, limit)

	var rows []contractRecordRow
	if err := pkgSqlxDB.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, err
	}
	items := make([]store.ContractRecord, len(rows))
	for i, row := range rows {
		items[i] = row.toStore()
	}
	return items, nil
}

// UpdateContractRecordStatus transitions one record's status, scoped to the
// context tenant. The target status must be a known status.
func (s *PGContractStore) UpdateContractRecordStatus(ctx context.Context, id uuid.UUID, status string) error {
	if !store.ValidContractRecordStatus(status) {
		return fmt.Errorf("update contract record: unknown status %q", status)
	}
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE multi_agent_records SET status = $3 WHERE id = $1 AND tenant_id = $2`,
		id, tid, status)
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

// bodyJSON converts an opaque JSON body string into a JSONB-compatible value.
// An empty body becomes a valid empty JSON object.
func bodyJSON(body string) string {
	if body == "" {
		return "{}"
	}
	return body
}

// buildContractRecordWhere scopes a single-record read. Fails closed
// (WHERE 1=0) when a tenant ID is required but absent from the context.
func buildContractRecordWhere(ctx context.Context, id uuid.UUID) (string, []any) {
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return " WHERE 1=0", nil
		}
		return " WHERE id = $1 AND tenant_id = $2", []any{id, tenantID}
	}
	return " WHERE id = $1", []any{id}
}

// buildContractRecordListWhere scopes a record list read. Fails closed
// (WHERE 1=0) when a tenant ID is required but absent from the context.
func buildContractRecordListWhere(ctx context.Context, opts store.ContractRecordListOpts) (string, []any) {
	var conditions []string
	var args []any
	argIdx := 1
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return " WHERE 1=0", nil
		}
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIdx))
		args = append(args, tenantID)
		argIdx++
	}
	if opts.RunID != "" {
		conditions = append(conditions, fmt.Sprintf("run_id = $%d", argIdx))
		args = append(args, opts.RunID)
		argIdx++
	}
	if opts.Kind != "" {
		conditions = append(conditions, fmt.Sprintf("kind = $%d", argIdx))
		args = append(args, opts.Kind)
		argIdx++
	}
	if opts.Status != "" {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, opts.Status)
		argIdx++
	}
	if len(conditions) == 0 {
		return " WHERE 1=0", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

type contractRecordRow struct {
	ID        uuid.UUID `db:"id"`
	TenantID  uuid.UUID `db:"tenant_id"`
	RunID     *string   `db:"run_id"`
	Kind      string    `db:"kind"`
	Body      string    `db:"body"`
	Status    *string   `db:"status"`
	CreatedAt time.Time `db:"created_at"`
}

func (r contractRecordRow) toStore() store.ContractRecord {
	return store.ContractRecord{
		ID:        r.ID,
		TenantID:  r.TenantID,
		RunID:     derefStr(r.RunID),
		Kind:      r.Kind,
		Body:      r.Body,
		Status:    derefStr(r.Status),
		CreatedAt: r.CreatedAt,
	}
}
