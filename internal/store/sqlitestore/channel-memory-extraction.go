//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type SQLiteChannelMemoryExtractionStore struct {
	db *sql.DB
}

func NewSQLiteChannelMemoryExtractionStore(db *sql.DB) *SQLiteChannelMemoryExtractionStore {
	return &SQLiteChannelMemoryExtractionStore{db: db}
}

const sqliteChannelMemoryRunCols = `id, tenant_id, channel_instance_id, channel_name, agent_id, user_id,
 history_key, trigger, status, source_start_id, source_end_id, source_start_at, source_end_at,
 message_count, redaction_count, redaction_types, item_count, error_message, started_at,
 completed_at, created_at, updated_at`

const sqliteChannelMemoryItemCols = `id, tenant_id, run_id, channel_instance_id, agent_id, user_id,
 item_hash, item_type, summary, topics, entities, confidence, source_id, status, approved_by,
 approved_at, rejected_by, rejected_at, deleted_at, written_at, episodic_id, created_at, updated_at`

func (s *SQLiteChannelMemoryExtractionStore) CreateRun(ctx context.Context, run *store.ChannelMemoryExtractionRun) error {
	if run.ID == uuid.Nil {
		run.ID = store.GenNewID()
	}
	run.TenantID = tenantIDForInsert(ctx)
	now := time.Now().UTC()
	run.CreatedAt = now
	run.UpdatedAt = now
	if run.Status == "" {
		run.Status = store.ChannelMemoryRunPending
	}
	if run.RedactionTypes == nil {
		run.RedactionTypes = json.RawMessage("[]")
	}
	return s.db.QueryRowContext(ctx, `INSERT INTO channel_memory_extraction_runs
		(`+sqliteChannelMemoryRunCols+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT (tenant_id, channel_instance_id, history_key, source_start_id, source_end_id)
		DO UPDATE SET updated_at = excluded.updated_at
		RETURNING id`,
		run.ID, run.TenantID, run.ChannelInstanceID, run.ChannelName, run.AgentID, run.UserID,
		run.HistoryKey, run.Trigger, run.Status, run.SourceStartID, run.SourceEndID,
		run.SourceStartAt, run.SourceEndAt, run.MessageCount, run.RedactionCount,
		string(run.RedactionTypes), run.ItemCount, run.ErrorMessage, run.StartedAt, run.CompletedAt,
		run.CreatedAt, run.UpdatedAt,
	).Scan(&run.ID)
}

func (s *SQLiteChannelMemoryExtractionStore) GetRun(ctx context.Context, id uuid.UUID) (*store.ChannelMemoryExtractionRun, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+sqliteChannelMemoryRunCols+` FROM channel_memory_extraction_runs WHERE id = ?`+tClause, append([]any{id}, tArgs...)...)
	return scanSQLiteChannelMemoryRun(row)
}

func (s *SQLiteChannelMemoryExtractionStore) ListRuns(ctx context.Context, opts store.ChannelMemoryRunListOptions) ([]store.ChannelMemoryExtractionRun, error) {
	tClause, args, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	conds := []string{"1=1" + tClause}
	if opts.ChannelInstanceID != uuid.Nil {
		conds = append(conds, "channel_instance_id = ?")
		args = append(args, opts.ChannelInstanceID)
	}
	if opts.HistoryKey != "" {
		conds = append(conds, "history_key = ?")
		args = append(args, opts.HistoryKey)
	}
	if opts.Status != "" {
		conds = append(conds, "status = ?")
		args = append(args, opts.Status)
	}
	limit := opts.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `SELECT `+sqliteChannelMemoryRunCols+`
		FROM channel_memory_extraction_runs WHERE `+strings.Join(conds, " AND ")+`
		ORDER BY created_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.ChannelMemoryExtractionRun
	for rows.Next() {
		run, err := scanSQLiteChannelMemoryRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *run)
	}
	return out, rows.Err()
}

func (s *SQLiteChannelMemoryExtractionStore) UpdateRun(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	return execMapUpdateWhereTenant(ctx, s.db, "channel_memory_extraction_runs", updates, id, tid)
}

func (s *SQLiteChannelMemoryExtractionStore) CreateItem(ctx context.Context, item *store.ChannelMemoryExtractionItem) error {
	if item.ID == uuid.Nil {
		item.ID = store.GenNewID()
	}
	item.TenantID = tenantIDForInsert(ctx)
	now := time.Now().UTC()
	item.CreatedAt = now
	item.UpdatedAt = now
	if item.Status == "" {
		item.Status = store.ChannelMemoryItemPendingReview
	}
	if item.Topics == nil {
		item.Topics = json.RawMessage("[]")
	}
	if item.Entities == nil {
		item.Entities = json.RawMessage("[]")
	}
	return s.db.QueryRowContext(ctx, `INSERT INTO channel_memory_extraction_items
		(`+sqliteChannelMemoryItemCols+`)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT (tenant_id, run_id, item_hash) DO UPDATE SET updated_at = excluded.updated_at
		RETURNING id`,
		item.ID, item.TenantID, item.RunID, item.ChannelInstanceID, item.AgentID, item.UserID,
		item.ItemHash, item.ItemType, item.Summary, string(item.Topics), string(item.Entities),
		item.Confidence, item.SourceID, item.Status, item.ApprovedBy, item.ApprovedAt,
		item.RejectedBy, item.RejectedAt, item.DeletedAt, item.WrittenAt, item.EpisodicID,
		item.CreatedAt, item.UpdatedAt,
	).Scan(&item.ID)
}

func (s *SQLiteChannelMemoryExtractionStore) GetItem(ctx context.Context, id uuid.UUID) (*store.ChannelMemoryExtractionItem, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+sqliteChannelMemoryItemCols+` FROM channel_memory_extraction_items WHERE id = ?`+tClause, append([]any{id}, tArgs...)...)
	return scanSQLiteChannelMemoryItem(row)
}

func (s *SQLiteChannelMemoryExtractionStore) ListItems(ctx context.Context, opts store.ChannelMemoryItemListOptions) ([]store.ChannelMemoryExtractionItem, error) {
	tClause, args, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	conds := []string{"1=1" + tClause}
	if opts.ChannelInstanceID != uuid.Nil {
		conds = append(conds, "channel_instance_id = ?")
		args = append(args, opts.ChannelInstanceID)
	}
	if opts.RunID != uuid.Nil {
		conds = append(conds, "run_id = ?")
		args = append(args, opts.RunID)
	}
	if opts.Status != "" {
		conds = append(conds, "status = ?")
		args = append(args, opts.Status)
	}
	limit := opts.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `SELECT `+sqliteChannelMemoryItemCols+`
		FROM channel_memory_extraction_items WHERE `+strings.Join(conds, " AND ")+`
		ORDER BY created_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.ChannelMemoryExtractionItem
	for rows.Next() {
		item, err := scanSQLiteChannelMemoryItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func (s *SQLiteChannelMemoryExtractionStore) UpdateItem(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	return execMapUpdateWhereTenant(ctx, s.db, "channel_memory_extraction_items", updates, id, tid)
}

type sqliteChannelMemoryScanner interface {
	Scan(dest ...any) error
}

func scanSQLiteChannelMemoryRun(row sqliteChannelMemoryScanner) (*store.ChannelMemoryExtractionRun, error) {
	var r store.ChannelMemoryExtractionRun
	var redactions string
	sourceStart, sourceEnd := &nullSqliteTime{}, &nullSqliteTime{}
	started, completed := &nullSqliteTime{}, &nullSqliteTime{}
	created, updated := scanTimePair()
	if err := row.Scan(&r.ID, &r.TenantID, &r.ChannelInstanceID, &r.ChannelName, &r.AgentID, &r.UserID, &r.HistoryKey, &r.Trigger, &r.Status, &r.SourceStartID, &r.SourceEndID, sourceStart, sourceEnd, &r.MessageCount, &r.RedactionCount, &redactions, &r.ItemCount, &r.ErrorMessage, started, completed, created, updated); err != nil {
		return nil, err
	}
	r.RedactionTypes = json.RawMessage(redactions)
	r.SourceStartAt = sqliteTimePtr(sourceStart)
	r.SourceEndAt = sqliteTimePtr(sourceEnd)
	r.StartedAt = sqliteTimePtr(started)
	r.CompletedAt = sqliteTimePtr(completed)
	r.CreatedAt = created.Time
	r.UpdatedAt = updated.Time
	return &r, nil
}

func scanSQLiteChannelMemoryItem(row sqliteChannelMemoryScanner) (*store.ChannelMemoryExtractionItem, error) {
	var i store.ChannelMemoryExtractionItem
	var topics, entities string
	approved, rejected := &nullSqliteTime{}, &nullSqliteTime{}
	deleted, written := &nullSqliteTime{}, &nullSqliteTime{}
	created, updated := scanTimePair()
	if err := row.Scan(&i.ID, &i.TenantID, &i.RunID, &i.ChannelInstanceID, &i.AgentID, &i.UserID, &i.ItemHash, &i.ItemType, &i.Summary, &topics, &entities, &i.Confidence, &i.SourceID, &i.Status, &i.ApprovedBy, approved, &i.RejectedBy, rejected, deleted, written, &i.EpisodicID, created, updated); err != nil {
		return nil, err
	}
	i.Topics = json.RawMessage(topics)
	i.Entities = json.RawMessage(entities)
	i.ApprovedAt = sqliteTimePtr(approved)
	i.RejectedAt = sqliteTimePtr(rejected)
	i.DeletedAt = sqliteTimePtr(deleted)
	i.WrittenAt = sqliteTimePtr(written)
	i.CreatedAt = created.Time
	i.UpdatedAt = updated.Time
	return &i, nil
}

func sqliteTimePtr(nt *nullSqliteTime) *time.Time {
	if nt == nil || !nt.Valid {
		return nil
	}
	return &nt.Time
}

var _ store.ChannelMemoryExtractionStore = (*SQLiteChannelMemoryExtractionStore)(nil)
