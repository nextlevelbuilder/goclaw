//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteSubagentTaskStore implements store.SubagentTaskStore backed by SQLite.
type SQLiteSubagentTaskStore struct {
	db *sql.DB
}

var _ store.SubagentTaskRecoveryStore = (*SQLiteSubagentTaskStore)(nil)

// NewSQLiteSubagentTaskStore creates a new SQLiteSubagentTaskStore.
func NewSQLiteSubagentTaskStore(db *sql.DB) *SQLiteSubagentTaskStore {
	return &SQLiteSubagentTaskStore{db: db}
}

const subagentTaskInsertCols = `tenant_id, root_agent_id, parent_agent_key, session_key, subject, description,
	status, result, depth, model, provider, iterations, input_tokens, output_tokens,
	origin_channel, origin_chat_id, origin_peer_kind, origin_user_id, spawned_by, metadata`

const subagentTaskSelectCols = `id, tenant_id, root_agent_id, parent_agent_key, session_key, subject, description,
	status, result, depth, model, provider, iterations, input_tokens, output_tokens,
	origin_channel, origin_chat_id, origin_peer_kind, origin_user_id, spawned_by,
	completed_at, archived_at, COALESCE(metadata, '{}'), created_at, updated_at`

// Create persists a new subagent task at spawn time.
func (s *SQLiteSubagentTaskStore) Create(ctx context.Context, task *store.SubagentTaskData) error {
	tid := tenantIDForInsert(ctx)
	if task.RootAgentID == uuid.Nil {
		return store.ErrSubagentRootAgentIDRequired
	}

	metaJSON := []byte("{}")
	if len(task.Metadata) > 0 {
		if b, err := json.Marshal(task.Metadata); err == nil {
			metaJSON = b
		}
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	q := fmt.Sprintf(`INSERT OR IGNORE INTO subagent_tasks (id, %s, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, subagentTaskInsertCols)

	_, err := s.db.ExecContext(ctx, q,
		task.ID, tid, task.RootAgentID, task.ParentAgentKey, task.SessionKey, task.Subject, task.Description,
		task.Status, task.Result, task.Depth, task.Model, task.Provider,
		task.Iterations, task.InputTokens, task.OutputTokens,
		task.OriginChannel, task.OriginChatID, task.OriginPeerKind, task.OriginUserID,
		task.SpawnedBy, metaJSON,
		now, now,
	)
	return err
}

// scanTask scans a single row into SubagentTaskData.
func scanTask(row interface{ Scan(...any) error }) (*store.SubagentTaskData, error) {
	var t store.SubagentTaskData
	var metaJSON []byte
	var completedAt, archivedAt nullSqliteTime
	var createdAt, updatedAt sqliteTime

	err := row.Scan(
		&t.ID, &t.TenantID, &t.RootAgentID, &t.ParentAgentKey, &t.SessionKey, &t.Subject, &t.Description,
		&t.Status, &t.Result, &t.Depth, &t.Model, &t.Provider,
		&t.Iterations, &t.InputTokens, &t.OutputTokens,
		&t.OriginChannel, &t.OriginChatID, &t.OriginPeerKind, &t.OriginUserID, &t.SpawnedBy,
		&completedAt, &archivedAt, &metaJSON, &createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}
	if completedAt.Valid {
		v := completedAt.Time
		t.CompletedAt = &v
	}
	if archivedAt.Valid {
		v := archivedAt.Time
		t.ArchivedAt = &v
	}
	t.CreatedAt = createdAt.Time
	t.UpdatedAt = updatedAt.Time
	if len(metaJSON) > 2 {
		_ = json.Unmarshal(metaJSON, &t.Metadata)
	}
	return &t, nil
}

// Get retrieves a task owned by the tenant and immutable root-agent UUID.
func (s *SQLiteSubagentTaskStore) Get(
	ctx context.Context, rootAgentID, id uuid.UUID,
) (*store.SubagentTaskData, error) {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return nil, err
	}
	if rootAgentID == uuid.Nil {
		return nil, store.ErrSubagentRootAgentIDRequired
	}
	q := fmt.Sprintf(`SELECT %s FROM subagent_tasks
		WHERE id = ? AND tenant_id = ? AND root_agent_id = ?`, subagentTaskSelectCols)
	row := s.db.QueryRowContext(ctx, q, id, tid, rootAgentID)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

// UpdateStatus updates status, result, iterations, and token counts on completion/failure.
func (s *SQLiteSubagentTaskStore) UpdateStatus(
	ctx context.Context, rootAgentID, id uuid.UUID,
	status string, result *string, iterations int,
	inputTokens, outputTokens int64,
) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	if rootAgentID == uuid.Nil {
		return store.ErrSubagentRootAgentIDRequired
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	var completedAt *string
	if store.IsTerminalSubagentTaskStatus(status) {
		v := now
		completedAt = &v
	}

	q := `UPDATE subagent_tasks SET
		status = ?, result = ?, iterations = ?,
		input_tokens = ?, output_tokens = ?,
		completed_at = ?, updated_at = ?
		WHERE id = ? AND tenant_id = ? AND root_agent_id = ?`
	res, err := s.db.ExecContext(ctx, q,
		status, result, iterations, inputTokens, outputTokens,
		completedAt, now, id, tid, rootAgentID,
	)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return store.ErrSubagentTaskNotFound
	}
	return nil
}

// ListByParent returns tasks for a root-agent UUID, optionally filtered by status.
func (s *SQLiteSubagentTaskStore) ListByParent(
	ctx context.Context, rootAgentID uuid.UUID, statusFilter string,
) ([]store.SubagentTaskData, error) {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return nil, err
	}
	if rootAgentID == uuid.Nil {
		return nil, store.ErrSubagentRootAgentIDRequired
	}

	var rows *sql.Rows
	if statusFilter != "" {
		q := fmt.Sprintf(`SELECT %s FROM subagent_tasks
				WHERE tenant_id = ? AND root_agent_id = ? AND status = ?
				AND COALESCE(json_extract(metadata, '$.completion_kind'), 'subagent') <> 'delegate'
				ORDER BY created_at DESC LIMIT 50`, subagentTaskSelectCols)
		rows, err = s.db.QueryContext(ctx, q, tid, rootAgentID, statusFilter)
	} else {
		q := fmt.Sprintf(`SELECT %s FROM subagent_tasks
				WHERE tenant_id = ? AND root_agent_id = ?
				AND COALESCE(json_extract(metadata, '$.completion_kind'), 'subagent') <> 'delegate'
				ORDER BY created_at DESC LIMIT 50`, subagentTaskSelectCols)
		rows, err = s.db.QueryContext(ctx, q, tid, rootAgentID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectTasks(rows)
}

// ListBySession returns tasks for a specific session key (tenant-scoped).
func (s *SQLiteSubagentTaskStore) ListBySession(
	ctx context.Context, rootAgentID uuid.UUID, sessionKey string,
) ([]store.SubagentTaskData, error) {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return nil, err
	}
	if rootAgentID == uuid.Nil {
		return nil, store.ErrSubagentRootAgentIDRequired
	}

	q := fmt.Sprintf(`SELECT %s FROM subagent_tasks
		WHERE tenant_id = ? AND root_agent_id = ? AND session_key = ?
		AND COALESCE(json_extract(metadata, '$.completion_kind'), 'subagent') <> 'delegate'
		ORDER BY created_at DESC LIMIT 50`, subagentTaskSelectCols)
	rows, err := s.db.QueryContext(ctx, q, tid, rootAgentID, sessionKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectTasks(rows)
}

// Archive marks a bounded batch of old terminal tasks as archived.
func (s *SQLiteSubagentTaskStore) Archive(
	ctx context.Context, rootAgentID uuid.UUID, olderThan time.Duration, limit int,
) (int64, error) {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return 0, err
	}
	if rootAgentID == uuid.Nil {
		return 0, store.ErrSubagentRootAgentIDRequired
	}
	if limit <= 0 {
		return 0, nil
	}

	cutoff := time.Now().UTC().Add(-olderThan).Format(time.RFC3339Nano)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	q := `UPDATE subagent_tasks SET archived_at = ?, updated_at = ?
		WHERE tenant_id = ? AND root_agent_id = ? AND id IN (
			SELECT id
			FROM subagent_tasks
			WHERE tenant_id = ? AND root_agent_id = ?
				AND status IN ('completed', 'failed', 'cancelled')
				AND archived_at IS NULL AND completed_at < ?
			ORDER BY completed_at, id
			LIMIT ?
		)`
	res, err := s.db.ExecContext(
		ctx, q,
		now, now, tid, rootAgentID,
		tid, rootAgentID, cutoff, limit,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UpdateMetadata merges metadata keys atomically using json_set().
// Builds a single UPDATE statement to avoid read-merge-write race window.
func (s *SQLiteSubagentTaskStore) UpdateMetadata(
	ctx context.Context, rootAgentID, id uuid.UUID, metadata map[string]any,
) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	if rootAgentID == uuid.Nil {
		return store.ErrSubagentRootAgentIDRequired
	}
	if len(metadata) == 0 {
		return nil
	}

	// Build json_set(metadata, '$.key1', ?, '$.key2', ?, ...) expression.
	// Validate keys to prevent SQL injection via interpolated JSON path.
	var parts []string
	var args []any
	for k, v := range metadata {
		if !validMetadataKey(k) {
			return fmt.Errorf("invalid metadata key: %q", k)
		}
		parts = append(parts, fmt.Sprintf("'$.%s', json(?)", k))
		b, _ := json.Marshal(v)
		args = append(args, string(b))
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	setExpr := "json_set(metadata, " + strings.Join(parts, ", ") + ")"
	args = append(args, now, id, tid, rootAgentID)

	q := fmt.Sprintf(`UPDATE subagent_tasks SET metadata = %s, updated_at = ?
		WHERE id = ? AND tenant_id = ? AND root_agent_id = ?`, setExpr)
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return store.ErrSubagentTaskNotFound
	}
	return nil
}

// RecoverInterrupted marks child runs that cannot survive a process restart as
// failed. This cross-tenant update runs before the gateway accepts new work.
func (s *SQLiteSubagentTaskStore) RecoverInterrupted(ctx context.Context) (int64, error) {
	if !store.IsMasterScope(ctx) {
		return 0, fmt.Errorf("recover interrupted subagent tasks requires master scope")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	q := `UPDATE subagent_tasks SET
		status = 'failed', result = ?, completed_at = ?, updated_at = ?
		WHERE status NOT IN ('completed', 'failed', 'cancelled')`
	res, err := s.db.ExecContext(ctx, q, store.InterruptedSubagentTaskResult, now, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func collectTasks(rows *sql.Rows) ([]store.SubagentTaskData, error) {
	var tasks []store.SubagentTaskData
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *t)
	}
	return tasks, rows.Err()
}

// validMetadataKey returns true if the key is safe for use in json_set() SQL path.
var metadataKeyRe = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

func validMetadataKey(k string) bool { return metadataKeyRe.MatchString(k) }
