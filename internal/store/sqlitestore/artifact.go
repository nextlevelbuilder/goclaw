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

// SQLiteArtifactStore implements store.ArtifactStore backed by SQLite.
type SQLiteArtifactStore struct {
	db *sql.DB
}

// NewSQLiteArtifactStore creates a SQLite-backed artifact store.
func NewSQLiteArtifactStore(db *sql.DB) *SQLiteArtifactStore {
	return &SQLiteArtifactStore{db: db}
}

// CreateArtifact inserts a new artifact version. Root artifacts (ParentID nil)
// get version 1; children get parent.version + 1 computed inside the same
// transaction so the chain stays gap-free under concurrency.
func (s *SQLiteArtifactStore) CreateArtifact(ctx context.Context, artifact *store.Artifact) error {
	if artifact.ID == uuid.Nil {
		artifact.ID = store.GenNewID()
	}
	if artifact.CreatedAt.IsZero() {
		artifact.CreatedAt = time.Now()
	}
	if artifact.Type == "" {
		return fmt.Errorf("create artifact: type required")
	}
	if !store.ValidArtifactType(artifact.Type) {
		return fmt.Errorf("create artifact: unknown type %q", artifact.Type)
	}
	if artifact.Status == "" {
		artifact.Status = store.ArtifactStatusDraft
	}
	if !store.ValidArtifactStatus(artifact.Status) {
		return fmt.Errorf("create artifact: unknown status %q", artifact.Status)
	}
	if artifact.Checksum == "" {
		artifact.Checksum = store.ArtifactChecksum(artifact.Content)
	}
	if artifact.Version == 0 {
		artifact.Version = 1
	}
	artifact.TenantID = tenantIDForInsert(ctx)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("create artifact: begin tx: %w", err)
	}
	defer tx.Rollback()

	version := artifact.Version
	if artifact.ParentID != nil {
		var parentVersion int
		err := tx.QueryRowContext(ctx,
			`SELECT version FROM artifacts WHERE id = ? AND tenant_id = ?`,
			artifact.ParentID.String(), artifact.TenantID.String()).Scan(&parentVersion)
		if err != nil {
			return fmt.Errorf("create artifact: read parent version: %w", err)
		}
		version = parentVersion + 1
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO artifacts
		 (id, tenant_id, run_id, version, author_agent, type, status, checksum,
		  parent_id, title, content, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		artifact.ID.String(), artifact.TenantID.String(), nilStr(artifact.RunID), version,
		nilStr(artifact.AuthorAgent), artifact.Type, artifact.Status,
		nilStr(artifact.Checksum), nilUUIDStr(artifact.ParentID), nilStr(artifact.Title),
		artifact.Content, artifact.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("create artifact: insert: %w", err)
	}
	artifact.Version = version
	return tx.Commit()
}

// GetArtifact returns one artifact by id, scoped to the context tenant.
func (s *SQLiteArtifactStore) GetArtifact(ctx context.Context, id uuid.UUID) (*store.Artifact, error) {
	where, args := buildSQLiteArtifactWhere(ctx, id)
	q := `SELECT id, tenant_id, run_id, version, author_agent, type, status, checksum,
		 parent_id, title, content, created_at
		 FROM artifacts` + where
	var row artifactRow
	if err := scanArtifactRow(s.db.QueryRowContext(ctx, q, args...).Scan, &row); err != nil {
		return nil, err
	}
	got := row.toStore()
	return &got, nil
}

// ListArtifacts returns artifact rows filtered by opts, scoped to the context
// tenant. Newest first.
func (s *SQLiteArtifactStore) ListArtifacts(ctx context.Context, opts store.ArtifactListOpts) ([]store.Artifact, error) {
	where, args := buildSQLiteArtifactListWhere(ctx, opts)
	limit := opts.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, tenant_id, run_id, version, author_agent, type, status, checksum,
		 parent_id, title, content, created_at
		 FROM artifacts` + where +
		` ORDER BY created_at DESC, id DESC` +
		fmt.Sprintf(" LIMIT %d OFFSET %d", limit, opts.Offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []store.Artifact
	for rows.Next() {
		var row artifactRow
		if err := scanArtifactRow(rows.Scan, &row); err != nil {
			return nil, err
		}
		items = append(items, row.toStore())
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if items == nil {
		items = []store.Artifact{}
	}
	return items, nil
}

// GetVersionChain returns the direct children of parentID within tenantID,
// ordered by version ascending. A nil parentID returns root artifacts
// (parent_id IS NULL). The tenant is explicit so callers can walk a chain
// without fabricating tenant context.
func (s *SQLiteArtifactStore) GetVersionChain(ctx context.Context, tenantID uuid.UUID, parentID *uuid.UUID) ([]store.Artifact, error) {
	var q string
	var args []any
	if parentID == nil {
		q = `SELECT id, tenant_id, run_id, version, author_agent, type, status, checksum,
			 parent_id, title, content, created_at
			 FROM artifacts
			 WHERE tenant_id = ? AND parent_id IS NULL
			 ORDER BY version ASC, created_at ASC`
		args = []any{tenantID.String()}
	} else {
		q = `SELECT id, tenant_id, run_id, version, author_agent, type, status, checksum,
			 parent_id, title, content, created_at
			 FROM artifacts
			 WHERE tenant_id = ? AND parent_id = ?
			 ORDER BY version ASC, created_at ASC`
		args = []any{tenantID.String(), parentID.String()}
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []store.Artifact
	for rows.Next() {
		var row artifactRow
		if err := scanArtifactRow(rows.Scan, &row); err != nil {
			return nil, err
		}
		items = append(items, row.toStore())
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if items == nil {
		items = []store.Artifact{}
	}
	return items, nil
}

// MarkArtifactStatus transitions one artifact's status, scoped to the context
// tenant.
func (s *SQLiteArtifactStore) MarkArtifactStatus(ctx context.Context, id uuid.UUID, status string) error {
	if !store.ValidArtifactStatus(status) {
		return fmt.Errorf("mark artifact status: unknown status %q", status)
	}
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE artifacts SET status = ? WHERE id = ? AND tenant_id = ?`,
		status, id.String(), tid.String())
	return err
}

// buildSQLiteArtifactWhere scopes a single-artifact read. Fails closed
// (WHERE 1=0) when a tenant ID is required but absent from the context.
func buildSQLiteArtifactWhere(ctx context.Context, id uuid.UUID) (string, []any) {
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return " WHERE 1=0", nil
		}
		return " WHERE id = ? AND tenant_id = ?", []any{id.String(), tenantID.String()}
	}
	return " WHERE id = ?", []any{id.String()}
}

// buildSQLiteArtifactListWhere scopes an artifact list read. Fails closed
// (WHERE 1=0) when a tenant ID is required but absent from the context.
func buildSQLiteArtifactListWhere(ctx context.Context, opts store.ArtifactListOpts) (string, []any) {
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
	if opts.Type != "" {
		conditions = append(conditions, "type = ?")
		args = append(args, opts.Type)
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

type artifactRow struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	RunID       sql.NullString
	Version     int
	AuthorAgent sql.NullString
	Type        string
	Status      sql.NullString
	Checksum    sql.NullString
	ParentID    *uuid.UUID
	Title       sql.NullString
	Content     string
	CreatedAt   sqliteTime
}

// scanArtifactRow scans one artifacts row, converting SQLite's TEXT UUIDs and
// timestamps into Go values.
func scanArtifactRow(scan func(dest ...any) error, r *artifactRow) error {
	var parentID sql.NullString
	var createdAt sqliteTime
	if err := scan(
		&r.ID, &r.TenantID, &r.RunID, &r.Version, &r.AuthorAgent, &r.Type, &r.Status,
		&r.Checksum, &parentID, &r.Title, &r.Content, &createdAt,
	); err != nil {
		return err
	}
	if parentID.Valid && parentID.String != "" {
		id := uuid.MustParse(parentID.String)
		r.ParentID = &id
	}
	r.CreatedAt = createdAt
	return nil
}

func (r artifactRow) toStore() store.Artifact {
	return store.Artifact{
		ID:          r.ID,
		TenantID:    r.TenantID,
		RunID:       r.RunID.String,
		Version:     r.Version,
		AuthorAgent: r.AuthorAgent.String,
		Type:        r.Type,
		Status:      r.Status.String,
		Checksum:    r.Checksum.String,
		ParentID:    r.ParentID,
		Title:       r.Title.String,
		Content:     r.Content,
		CreatedAt:   r.CreatedAt.Time,
	}
}

// nilUUIDStr returns nil for a nil/empty UUID — used for the nullable parent_id
// and the SQLite TEXT conversion of uuid.UUID.
func nilUUIDStr(u *uuid.UUID) *string {
	if u == nil || *u == uuid.Nil {
		return nil
	}
	s := u.String()
	return &s
}
