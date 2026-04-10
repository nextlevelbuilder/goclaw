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

// NewSQLiteSubagentTaskStore creates a new SQLiteSubagentTaskStore.
func NewSQLiteSubagentTaskStore(db *sql.DB) *SQLiteSubagentTaskStore {
	return &SQLiteSubagentTaskStore{db: db}
}

const subagentTaskInsertCols = `tenant_id, parent_agent_key, session_key, subject, description,
	status, result, depth, model, provider, iterations, input_tokens, output_tokens,
	origin_channel, origin_chat_id, origin_peer_kind, origin_user_id, spawned_by, metadata`

const subagentTaskSelectCols = `id, tenant_id, parent_agent_key, session_key, subject, description,
	status, result, depth, model, provider, iterations, input_tokens, output_tokens,
	origin_channel, origin_chat_id, origin_peer_kind, origin_user_id, spawned_by,
	completed_at, archived_at, COALESCE(metadata, '{}'), created_at, updated_at`

// Create persists a new subagent task at spawn time.
func (s *SQLiteSubagentTaskStore) Create(ctx context.Context, task *store.SubagentTaskData) error {
	tid := tenantIDForInsert(ctx)

	metaJSON := []byte("{}")
	if len(task.Metadata) > 0 {
		if b, err := json.Marshal(task.Metadata); err == nil {
			metaJSON = b
		}
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	q := fmt.Sprintf(`INSERT OR IGNORE INTO subagent_tasks (id, %s, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, subagentTaskInsertCols)

	_, err := s.db.ExecContext(ctx, q,
		task.ID, tid, task.ParentAgentKey, task.SessionKey, task.Subject, task.Description,
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
		&t.ID, &t.TenantID, &t.ParentAgentKey, &t.SessionKey, &t.Subject, &t.Description,
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

// Get retrieves a single task by ID (tenant-scoped).
func (s *SQLiteSubagentTaskStore) Get(ctx context.Context, id uuid.UUID) (*store.SubagentTaskData, error) {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`SELECT %s FROM subagent_tasks WHERE id = ? AND tenant_id = ?`, subagentTaskSelectCols)
	row := s.db.QueryRowContext(ctx, q, id, tid)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

// UpdateStatus updates status, result, iterations, and token counts on completion/failure.
func (s *SQLiteSubagentTaskStore) UpdateStatus(
	ctx context.Context, id uuid.UUID,
	status string, result *string, iterations int,
	inputTokens, outputTokens int64,
) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	var completedAt *string
	if status != "running" {
		v := now
		completedAt = &v
	}

	q := `UPDATE subagent_tasks SET
		status = ?, result = ?, iterations = ?,
		input_tokens = ?, output_tokens = ?,
		completed_at = ?, updated_at = ?
		WHERE id = ? AND tenant_id = ?`
	_, err = s.db.ExecContext(ctx, q,
		status, result, iterations, inputTokens, outputTokens,
		completedAt, now, id, tid,
	)
	return err
}

// ListByParent returns tasks for a parent agent key, optionally filtered by status.
func (s *SQLiteSubagentTaskStore) ListByParent(
	ctx context.Context, parentAgentKey string, statusFilter string,
) ([]store.SubagentTaskData, error) {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return nil, err
	}

	var rows *sql.Rows
	if statusFilter != "" {
		q := fmt.Sprintf(`SELECT %s FROM subagent_tasks
			WHERE tenant_id = ? AND parent_agent_key = ? AND status = ?
			ORDER BY created_at DESC LIMIT 50`, subagentTaskSelectCols)
		rows, err = s.db.QueryContext(ctx, q, tid, parentAgentKey, statusFilter)
	} else {
		q := fmt.Sprintf(`SELECT %s FROM subagent_tasks
			WHERE tenant_id = ? AND parent_agent_key = ?
			ORDER BY created_at DESC LIMIT 50`, subagentTaskSelectCols)
		rows, err = s.db.QueryContext(ctx, q, tid, parentAgentKey)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectTasks(rows)
}

// ListBySession returns tasks for a specific session key (tenant-scoped).
func (s *SQLiteSubagentTaskStore) ListBySession(
	ctx context.Context, sessionKey string,
) ([]store.SubagentTaskData, error) {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return nil, err
	}

	q := fmt.Sprintf(`SELECT %s FROM subagent_tasks
		WHERE tenant_id = ? AND session_key = ?
		ORDER BY created_at DESC LIMIT 50`, subagentTaskSelectCols)
	rows, err := s.db.QueryContext(ctx, q, tid, sessionKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectTasks(rows)
}

// Archive marks old completed/failed/cancelled tasks as archived.
func (s *SQLiteSubagentTaskStore) Archive(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan).Format(time.RFC3339Nano)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	q := `UPDATE subagent_tasks SET archived_at = ?, updated_at = ?
		WHERE status IN ('completed', 'failed', 'cancelled')
		AND archived_at IS NULL AND completed_at < ?`
	res, err := s.db.ExecContext(ctx, q, now, now, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UpdateMetadata merges metadata keys atomically using json_set().
// Builds a single UPDATE statement to avoid read-merge-write race window.
func (s *SQLiteSubagentTaskStore) UpdateMetadata(ctx context.Context, id uuid.UUID, metadata map[string]any) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
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
		parts = append(parts, fmt.Sprintf("'$.%s', ?", k))
		b, _ := json.Marshal(v)
		args = append(args, string(b))
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	setExpr := "json_set(metadata, " + strings.Join(parts, ", ") + ")"
	args = append(args, now, id, tid)

	q := fmt.Sprintf(`UPDATE subagent_tasks SET metadata = %s, updated_at = ? WHERE id = ? AND tenant_id = ?`, setExpr)
	_, err = s.db.ExecContext(ctx, q, args...)
	return err
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
