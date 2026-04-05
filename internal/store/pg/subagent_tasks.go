package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGSubagentTaskStore implements store.SubagentTaskStore using PostgreSQL.
type PGSubagentTaskStore struct {
	db *sql.DB
}

// NewPGSubagentTaskStore creates a new PostgreSQL-backed subagent task store.
func NewPGSubagentTaskStore(db *sql.DB) *PGSubagentTaskStore {
	return &PGSubagentTaskStore{db: db}
}

const subagentTaskInsertCols = `tenant_id, parent_agent_key, session_key, subject, description,
	status, result, depth, model, provider, iterations, input_tokens, output_tokens,
	origin_channel, origin_chat_id, origin_peer_kind, origin_user_id, spawned_by, metadata`

// Create persists a new subagent task at spawn time.
func (s *PGSubagentTaskStore) Create(ctx context.Context, task *store.SubagentTaskData) error {
	tid := tenantIDForInsert(ctx)

	metaJSON := []byte("{}")
	if len(task.Metadata) > 0 {
		if b, err := json.Marshal(task.Metadata); err == nil {
			metaJSON = b
		}
	}

	q := fmt.Sprintf(`INSERT INTO subagent_tasks (id, %s)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
		ON CONFLICT (id) DO NOTHING`, subagentTaskInsertCols)

	_, err := s.db.ExecContext(ctx, q,
		task.ID, tid, task.ParentAgentKey, task.SessionKey, task.Subject, task.Description,
		task.Status, task.Result, task.Depth, task.Model, task.Provider,
		task.Iterations, task.InputTokens, task.OutputTokens,
		task.OriginChannel, task.OriginChatID, task.OriginPeerKind, task.OriginUserID,
		task.SpawnedBy, metaJSON,
	)
	return err
}

const subagentTaskSelectCols = `id, tenant_id, parent_agent_key, session_key, subject, description,
	status, result, depth, model, provider, iterations, input_tokens, output_tokens,
	origin_channel, origin_chat_id, origin_peer_kind, origin_user_id, spawned_by,
	completed_at, archived_at, COALESCE(metadata, '{}'), created_at, updated_at`

// scanTask scans a single row into SubagentTaskData.
func scanTask(row interface{ Scan(...any) error }) (*store.SubagentTaskData, error) {
	var t store.SubagentTaskData
	var metaJSON []byte
	err := row.Scan(
		&t.ID, &t.TenantID, &t.ParentAgentKey, &t.SessionKey, &t.Subject, &t.Description,
		&t.Status, &t.Result, &t.Depth, &t.Model, &t.Provider,
		&t.Iterations, &t.InputTokens, &t.OutputTokens,
		&t.OriginChannel, &t.OriginChatID, &t.OriginPeerKind, &t.OriginUserID, &t.SpawnedBy,
		&t.CompletedAt, &t.ArchivedAt, &metaJSON, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if len(metaJSON) > 2 { // skip "{}"
		_ = json.Unmarshal(metaJSON, &t.Metadata)
	}
	return &t, nil
}

// Get retrieves a single task by ID (tenant-scoped).
func (s *PGSubagentTaskStore) Get(ctx context.Context, id uuid.UUID) (*store.SubagentTaskData, error) {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`SELECT %s FROM subagent_tasks WHERE id = $1 AND tenant_id = $2`, subagentTaskSelectCols)
	row := s.db.QueryRowContext(ctx, q, id, tid)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

// UpdateStatus updates status, result, iterations, and token counts.
func (s *PGSubagentTaskStore) UpdateStatus(
	ctx context.Context, id uuid.UUID,
	status string, result *string, iterations int,
	inputTokens, outputTokens int64,
) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}

	var completedAt *time.Time
	if status != "running" {
		now := time.Now().UTC()
		completedAt = &now
	}

	q := `UPDATE subagent_tasks SET
		status = $1, result = $2, iterations = $3,
		input_tokens = $4, output_tokens = $5,
		completed_at = $6, updated_at = NOW()
		WHERE id = $7 AND tenant_id = $8`
	_, err = s.db.ExecContext(ctx, q,
		status, result, iterations, inputTokens, outputTokens,
		completedAt, id, tid,
	)
	return err
}

// ListByParent returns tasks for a parent agent key, optionally filtered by status.
func (s *PGSubagentTaskStore) ListByParent(
	ctx context.Context, parentAgentKey string, statusFilter string,
) ([]store.SubagentTaskData, error) {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return nil, err
	}

	var rows *sql.Rows
	if statusFilter != "" {
		q := fmt.Sprintf(`SELECT %s FROM subagent_tasks
			WHERE tenant_id = $1 AND parent_agent_key = $2 AND status = $3
			ORDER BY created_at DESC LIMIT 50`, subagentTaskSelectCols)
		rows, err = s.db.QueryContext(ctx, q, tid, parentAgentKey, statusFilter)
	} else {
		q := fmt.Sprintf(`SELECT %s FROM subagent_tasks
			WHERE tenant_id = $1 AND parent_agent_key = $2
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
func (s *PGSubagentTaskStore) ListBySession(
	ctx context.Context, sessionKey string,
) ([]store.SubagentTaskData, error) {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return nil, err
	}

	q := fmt.Sprintf(`SELECT %s FROM subagent_tasks
		WHERE tenant_id = $1 AND session_key = $2
		ORDER BY created_at DESC LIMIT 50`, subagentTaskSelectCols)
	rows, err := s.db.QueryContext(ctx, q, tid, sessionKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return collectTasks(rows)
}

// Archive marks old completed/failed/cancelled tasks as archived.
func (s *PGSubagentTaskStore) Archive(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan)
	q := `UPDATE subagent_tasks SET archived_at = NOW(), updated_at = NOW()
		WHERE status IN ('completed', 'failed', 'cancelled')
		AND archived_at IS NULL AND completed_at < $1`
	res, err := s.db.ExecContext(ctx, q, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UpdateMetadata merges metadata on an existing task.
func (s *PGSubagentTaskStore) UpdateMetadata(ctx context.Context, id uuid.UUID, metadata map[string]any) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}

	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		return err
	}

	q := `UPDATE subagent_tasks SET metadata = metadata || $1, updated_at = NOW()
		WHERE id = $2 AND tenant_id = $3`
	_, err = s.db.ExecContext(ctx, q, metaJSON, id, tid)
	return err
}

// collectTasks scans rows into a slice.
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
