package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteWorkflowStore implements store.WorkflowStore for the desktop/Lite build.
//
// Two differences from the PG store, both forced by SQLite rather than chosen:
// timestamps are RFC3339 text (no native timestamptz), and the id is generated
// here (no gen_random_uuid()).
type SQLiteWorkflowStore struct {
	db *sql.DB
}

func NewSQLiteWorkflowStore(db *sql.DB) *SQLiteWorkflowStore { return &SQLiteWorkflowStore{db: db} }

const sqliteWorkflowCols = `id, tenant_id, name, description, enabled, graph, compiled,
	compile_error, created_by, created_at, updated_at`

func scanSQLiteWorkflow(row interface{ Scan(...any) error }) (*store.Workflow, error) {
	var (
		w         store.Workflow
		id        string
		tenantID  sql.NullString
		enabled   int
		graph     string
		compiled  string
		createdAt string
		updatedAt string
	)
	if err := row.Scan(&id, &tenantID, &w.Name, &w.Description, &enabled,
		&graph, &compiled, &w.CompileError, &w.CreatedBy, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	parsed, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("workflow id %q is not a uuid: %w", id, err)
	}
	w.ID = parsed
	// Lite is single-tenant, so tenant_id is usually NULL. Parsing failures are
	// swallowed to uuid.Nil rather than erroring: a desktop row with no tenant is
	// the normal case, not corruption.
	if tenantID.Valid {
		if t, err := uuid.Parse(tenantID.String); err == nil {
			w.TenantID = t
		}
	}
	w.Enabled = enabled != 0
	w.Graph = json.RawMessage(graph)
	w.Compiled = json.RawMessage(compiled)
	w.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	w.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &w, nil
}

// tenantFilter renders the tenant predicate.
//
// uuid.Nil means "the single local tenant", which is stored as NULL — comparing
// a column to the zero UUID string would match nothing and make every desktop
// workflow invisible.
func tenantFilter(tenantID uuid.UUID) (string, any) {
	if tenantID == uuid.Nil {
		return "tenant_id IS NULL", nil
	}
	return "tenant_id = ?", tenantID.String()
}

func (s *SQLiteWorkflowStore) ListForTenant(ctx context.Context, tenantID uuid.UUID) ([]*store.Workflow, error) {
	pred, arg := tenantFilter(tenantID)
	q := `SELECT ` + sqliteWorkflowCols + ` FROM workflows WHERE ` + pred + ` ORDER BY updated_at DESC`
	var (
		rows *sql.Rows
		err  error
	)
	if arg == nil {
		rows, err = s.db.QueryContext(ctx, q)
	} else {
		rows, err = s.db.QueryContext(ctx, q, arg)
	}
	if err != nil {
		return nil, fmt.Errorf("list workflows: %w", err)
	}
	defer rows.Close()

	var out []*store.Workflow
	for rows.Next() {
		w, err := scanSQLiteWorkflow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan workflow: %w", err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *SQLiteWorkflowStore) Get(ctx context.Context, tenantID, id uuid.UUID) (*store.Workflow, error) {
	pred, arg := tenantFilter(tenantID)
	q := `SELECT ` + sqliteWorkflowCols + ` FROM workflows WHERE id = ? AND ` + pred
	var row *sql.Row
	if arg == nil {
		row = s.db.QueryRowContext(ctx, q, id.String())
	} else {
		row = s.db.QueryRowContext(ctx, q, id.String(), arg)
	}
	w, err := scanSQLiteWorkflow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get workflow: %w", err)
	}
	return w, nil
}

func (s *SQLiteWorkflowStore) Create(ctx context.Context, w *store.Workflow) error {
	if w.ID == uuid.Nil {
		w.ID = uuid.New()
	}
	if len(w.Graph) == 0 {
		w.Graph = json.RawMessage(`{}`)
	}
	if len(w.Compiled) == 0 {
		w.Compiled = json.RawMessage(`{}`)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var tenant any
	if w.TenantID != uuid.Nil {
		tenant = w.TenantID.String()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO workflows (id, tenant_id, name, description, enabled, graph, compiled, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.ID.String(), tenant, w.Name, w.Description, boolToInt(w.Enabled),
		string(w.Graph), string(w.Compiled), w.CreatedBy, now, now)
	if err != nil {
		if isSQLiteUnique(err) {
			return store.ErrWorkflowNameTaken
		}
		return fmt.Errorf("create workflow: %w", err)
	}
	w.CreatedAt, _ = time.Parse(time.RFC3339, now)
	w.UpdatedAt = w.CreatedAt
	return nil
}

func (s *SQLiteWorkflowStore) Update(ctx context.Context, w *store.Workflow) error {
	if len(w.Graph) == 0 {
		w.Graph = json.RawMessage(`{}`)
	}
	pred, arg := tenantFilter(w.TenantID)
	now := time.Now().UTC().Format(time.RFC3339)
	args := []any{w.Name, w.Description, boolToInt(w.Enabled), string(w.Graph), now, w.ID.String()}
	if arg != nil {
		args = append(args, arg)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE workflows SET name = ?, description = ?, enabled = ?, graph = ?, updated_at = ?
		  WHERE id = ? AND `+pred, args...)
	if err != nil {
		if isSQLiteUnique(err) {
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

func (s *SQLiteWorkflowStore) SetCompileResult(ctx context.Context, tenantID, id uuid.UUID, compiled json.RawMessage, compileErr *string) error {
	if len(compiled) == 0 {
		compiled = json.RawMessage(`{}`)
	}
	pred, arg := tenantFilter(tenantID)
	args := []any{string(compiled), compileErr, id.String()}
	if arg != nil {
		args = append(args, arg)
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE workflows SET compiled = ?, compile_error = ? WHERE id = ? AND `+pred, args...)
	if err != nil {
		return fmt.Errorf("set compile result: %w", err)
	}
	return nil
}

func (s *SQLiteWorkflowStore) Delete(ctx context.Context, tenantID, id uuid.UUID) error {
	pred, arg := tenantFilter(tenantID)
	args := []any{id.String()}
	if arg != nil {
		args = append(args, arg)
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM workflows WHERE id = ? AND `+pred, args...)
	if err != nil {
		return fmt.Errorf("delete workflow: %w", err)
	}
	return nil
}

func (s *SQLiteWorkflowStore) ListEnabled(ctx context.Context) ([]*store.Workflow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+sqliteWorkflowCols+` FROM workflows WHERE enabled = 1 ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list enabled workflows: %w", err)
	}
	defer rows.Close()

	var out []*store.Workflow
	for rows.Next() {
		w, err := scanSQLiteWorkflow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan workflow: %w", err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// isSQLiteUnique reports a UNIQUE constraint failure. modernc.org/sqlite reports
// it in the message rather than a typed code reachable through database/sql.
func isSQLiteUnique(err error) bool {
	return err != nil && strings.Contains(strings.ToUpper(err.Error()), "UNIQUE CONSTRAINT FAILED")
}
