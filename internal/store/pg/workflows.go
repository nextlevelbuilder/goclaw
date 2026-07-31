package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGWorkflowStore implements store.WorkflowStore backed by Postgres.
type PGWorkflowStore struct {
	db *sql.DB
}

func NewPGWorkflowStore(db *sql.DB) *PGWorkflowStore { return &PGWorkflowStore{db: db} }

// workflowCols keeps every read in sync. graph/compiled are NOT NULL with a '{}'
// default so they never scan as nil, which lets callers json.Unmarshal without a
// length check.
const workflowCols = `id, tenant_id, name, description, enabled, graph, compiled,
	compile_error, created_by, created_at, updated_at`

func scanWorkflow(row interface{ Scan(...any) error }) (*store.Workflow, error) {
	var w store.Workflow
	if err := row.Scan(
		&w.ID, &w.TenantID, &w.Name, &w.Description, &w.Enabled,
		&w.Graph, &w.Compiled, &w.CompileError, &w.CreatedBy, &w.CreatedAt, &w.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &w, nil
}

func (s *PGWorkflowStore) ListForTenant(ctx context.Context, tenantID uuid.UUID) ([]*store.Workflow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+workflowCols+` FROM workflows WHERE tenant_id = $1 ORDER BY updated_at DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list workflows: %w", err)
	}
	defer rows.Close()

	var out []*store.Workflow
	for rows.Next() {
		w, err := scanWorkflow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan workflow: %w", err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// Get returns nil, nil when the row does not exist OR belongs to another tenant.
// Both are the same answer on purpose: distinguishing them would turn a guessed
// UUID into an existence oracle for another tenant's automation.
func (s *PGWorkflowStore) Get(ctx context.Context, tenantID, id uuid.UUID) (*store.Workflow, error) {
	w, err := scanWorkflow(s.db.QueryRowContext(ctx,
		`SELECT `+workflowCols+` FROM workflows WHERE id = $1 AND tenant_id = $2`, id, tenantID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get workflow: %w", err)
	}
	return w, nil
}

func (s *PGWorkflowStore) Create(ctx context.Context, w *store.Workflow) error {
	if len(w.Graph) == 0 {
		w.Graph = json.RawMessage(`{}`)
	}
	if len(w.Compiled) == 0 {
		w.Compiled = json.RawMessage(`{}`)
	}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO workflows (tenant_id, name, description, enabled, graph, compiled, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, created_at, updated_at`,
		w.TenantID, w.Name, w.Description, w.Enabled, []byte(w.Graph), []byte(w.Compiled), w.CreatedBy,
	).Scan(&w.ID, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrWorkflowNameTaken
		}
		return fmt.Errorf("create workflow: %w", err)
	}
	return nil
}

// Update writes the AUTHORED fields only. compiled/compile_error are deliberately
// absent: they are owned by SetCompileResult, and letting a user's save carry
// them would let a stale client overwrite what the reconciler just recorded.
func (s *PGWorkflowStore) Update(ctx context.Context, w *store.Workflow) error {
	if len(w.Graph) == 0 {
		w.Graph = json.RawMessage(`{}`)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE workflows
		    SET name = $1, description = $2, enabled = $3, graph = $4, updated_at = NOW()
		  WHERE id = $5 AND tenant_id = $6`,
		w.Name, w.Description, w.Enabled, []byte(w.Graph), w.ID, w.TenantID)
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrWorkflowNameTaken
		}
		return fmt.Errorf("update workflow: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *PGWorkflowStore) SetCompileResult(ctx context.Context, tenantID, id uuid.UUID, compiled json.RawMessage, compileErr *string) error {
	if len(compiled) == 0 {
		compiled = json.RawMessage(`{}`)
	}
	// updated_at is intentionally NOT touched: a compile is a consequence of an
	// edit, not an edit, and bumping it would reshuffle the "recently changed"
	// ordering every time the reconciler ran.
	_, err := s.db.ExecContext(ctx,
		`UPDATE workflows SET compiled = $1, compile_error = $2 WHERE id = $3 AND tenant_id = $4`,
		[]byte(compiled), compileErr, id, tenantID)
	if err != nil {
		return fmt.Errorf("set compile result: %w", err)
	}
	return nil
}

func (s *PGWorkflowStore) Delete(ctx context.Context, tenantID, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM workflows WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	if err != nil {
		return fmt.Errorf("delete workflow: %w", err)
	}
	return nil
}

func (s *PGWorkflowStore) ListEnabled(ctx context.Context) ([]*store.Workflow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+workflowCols+` FROM workflows WHERE enabled ORDER BY tenant_id, updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list enabled workflows: %w", err)
	}
	defer rows.Close()

	var out []*store.Workflow
	for rows.Next() {
		w, err := scanWorkflow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan workflow: %w", err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// isUniqueViolation reports a Postgres 23505. Matched on the SQLSTATE text rather
// than a pgconn type assertion so it works through database/sql wrapping, which
// is how the rest of this package already receives driver errors.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") || strings.Contains(msg, "duplicate key value")
}
