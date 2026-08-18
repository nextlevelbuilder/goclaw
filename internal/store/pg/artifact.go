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

// PGArtifactStore implements store.ArtifactStore backed by PostgreSQL.
type PGArtifactStore struct {
	db *sql.DB
}

// NewPGArtifactStore creates a PG-backed artifact store.
func NewPGArtifactStore(db *sql.DB) *PGArtifactStore {
	return &PGArtifactStore{db: db}
}

// CreateArtifact inserts a new artifact version. Root artifacts (ParentID nil)
// get version 1; children get parent.version + 1 computed inside the same
// transaction so the chain stays gap-free under concurrency.
func (s *PGArtifactStore) CreateArtifact(ctx context.Context, artifact *store.Artifact) error {
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
			`SELECT version FROM artifacts WHERE id = $1 AND tenant_id = $2`,
			*artifact.ParentID, artifact.TenantID).Scan(&parentVersion)
		if err != nil {
			return fmt.Errorf("create artifact: read parent version: %w", err)
		}
		version = parentVersion + 1
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO artifacts
		 (id, tenant_id, run_id, version, author_agent, type, status, checksum,
		  parent_id, title, content, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		artifact.ID, artifact.TenantID, nilStr(artifact.RunID), version,
		nilStr(artifact.AuthorAgent), artifact.Type, artifact.Status,
		nilStr(artifact.Checksum), nilUUID(artifact.ParentID), nilStr(artifact.Title),
		artifact.Content, artifact.CreatedAt)
	if err != nil {
		return fmt.Errorf("create artifact: insert: %w", err)
	}
	artifact.Version = version
	return tx.Commit()
}

// GetArtifact returns one artifact by id, scoped to the context tenant.
func (s *PGArtifactStore) GetArtifact(ctx context.Context, id uuid.UUID) (*store.Artifact, error) {
	where, args := buildArtifactWhere(ctx, id)
	q := `SELECT id, tenant_id, run_id, version, author_agent, type, status, checksum,
		 parent_id, title, content, created_at
		 FROM artifacts` + where
	var row artifactRow
	if err := pkgSqlxDB.GetContext(ctx, &row, q, args...); err != nil {
		return nil, err
	}
	got := row.toStore()
	return &got, nil
}

// ListArtifacts returns artifact rows filtered by opts, scoped to the context
// tenant. Newest first.
func (s *PGArtifactStore) ListArtifacts(ctx context.Context, opts store.ArtifactListOpts) ([]store.Artifact, error) {
	where, args := buildArtifactListWhere(ctx, opts)
	limit := opts.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, tenant_id, run_id, version, author_agent, type, status, checksum,
		 parent_id, title, content, created_at
		 FROM artifacts` + where +
		` ORDER BY created_at DESC, id DESC` +
		fmt.Sprintf(" OFFSET %d LIMIT %d", opts.Offset, limit)

	var rows []artifactRow
	if err := pkgSqlxDB.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, err
	}
	items := make([]store.Artifact, len(rows))
	for i, row := range rows {
		items[i] = row.toStore()
	}
	return items, nil
}

// GetVersionChain returns the direct children of parentID within tenantID,
// ordered by version ascending. A nil parentID returns root artifacts
// (parent_id IS NULL). The tenant is explicit so callers can walk a chain
// without fabricating tenant context.
func (s *PGArtifactStore) GetVersionChain(ctx context.Context, tenantID uuid.UUID, parentID *uuid.UUID) ([]store.Artifact, error) {
	var q string
	var args []any
	if parentID == nil {
		q = `SELECT id, tenant_id, run_id, version, author_agent, type, status, checksum,
			 parent_id, title, content, created_at
			 FROM artifacts
			 WHERE tenant_id = $1 AND parent_id IS NULL
			 ORDER BY version ASC, created_at ASC`
		args = []any{tenantID}
	} else {
		q = `SELECT id, tenant_id, run_id, version, author_agent, type, status, checksum,
			 parent_id, title, content, created_at
			 FROM artifacts
			 WHERE tenant_id = $1 AND parent_id = $2
			 ORDER BY version ASC, created_at ASC`
		args = []any{tenantID, *parentID}
	}

	var rows []artifactRow
	if err := pkgSqlxDB.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, err
	}
	items := make([]store.Artifact, len(rows))
	for i, row := range rows {
		items[i] = row.toStore()
	}
	return items, nil
}

// MarkArtifactStatus transitions one artifact's status, scoped to the context
// tenant.
func (s *PGArtifactStore) MarkArtifactStatus(ctx context.Context, id uuid.UUID, status string) error {
	if !store.ValidArtifactStatus(status) {
		return fmt.Errorf("mark artifact status: unknown status %q", status)
	}
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE artifacts SET status = $3 WHERE id = $1 AND tenant_id = $2`,
		id, tid, status)
	return err
}

// buildArtifactWhere scopes a single-artifact read. Fails closed (WHERE 1=0)
// when a tenant ID is required but absent from the context.
func buildArtifactWhere(ctx context.Context, id uuid.UUID) (string, []any) {
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return " WHERE 1=0", nil
		}
		return " WHERE id = $1 AND tenant_id = $2", []any{id, tenantID}
	}
	return " WHERE id = $1", []any{id}
}

// buildArtifactListWhere scopes an artifact list read. Fails closed (WHERE 1=0)
// when a tenant ID is required but absent from the context.
func buildArtifactListWhere(ctx context.Context, opts store.ArtifactListOpts) (string, []any) {
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
	if opts.Type != "" {
		conditions = append(conditions, fmt.Sprintf("type = $%d", argIdx))
		args = append(args, opts.Type)
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

type artifactRow struct {
	ID          uuid.UUID  `db:"id"`
	TenantID    uuid.UUID  `db:"tenant_id"`
	RunID       *string    `db:"run_id"`
	Version     int        `db:"version"`
	AuthorAgent *string    `db:"author_agent"`
	Type        string     `db:"type"`
	Status      *string    `db:"status"`
	Checksum    *string    `db:"checksum"`
	ParentID    *uuid.UUID `db:"parent_id"`
	Title       *string    `db:"title"`
	Content     string     `db:"content"`
	CreatedAt   time.Time  `db:"created_at"`
}

func (r artifactRow) toStore() store.Artifact {
	return store.Artifact{
		ID:          r.ID,
		TenantID:    r.TenantID,
		RunID:       derefStr(r.RunID),
		Version:     r.Version,
		AuthorAgent: derefStr(r.AuthorAgent),
		Type:        r.Type,
		Status:      derefStr(r.Status),
		Checksum:    derefStr(r.Checksum),
		ParentID:    r.ParentID,
		Title:       derefStr(r.Title),
		Content:     r.Content,
		CreatedAt:   r.CreatedAt,
	}
}

