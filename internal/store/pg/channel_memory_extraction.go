package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type PGChannelMemoryExtractionStore struct {
	db *sql.DB
}

func NewPGChannelMemoryExtractionStore(db *sql.DB) *PGChannelMemoryExtractionStore {
	return &PGChannelMemoryExtractionStore{db: db}
}

const channelMemoryRunCols = `id, tenant_id, channel_instance_id, channel_name, agent_id, user_id,
 history_key, trigger, status, source_start_id, source_end_id, source_start_at, source_end_at,
 message_count, redaction_count, redaction_types, item_count, error_message, started_at,
 completed_at, created_at, updated_at`

const channelMemoryItemCols = `id, tenant_id, run_id, channel_instance_id, agent_id, user_id,
 item_hash, item_type, summary, topics, entities, confidence, source_id, status, approved_by,
 approved_at, rejected_by, rejected_at, deleted_at, written_at, episodic_id, created_at, updated_at`

func (s *PGChannelMemoryExtractionStore) CreateRun(ctx context.Context, run *store.ChannelMemoryExtractionRun) error {
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
	err := s.db.QueryRowContext(ctx, `INSERT INTO channel_memory_extraction_runs
		(`+channelMemoryRunCols+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)
		ON CONFLICT (tenant_id, channel_instance_id, history_key, source_start_id, source_end_id)
		DO UPDATE SET updated_at = EXCLUDED.updated_at
		RETURNING id`,
		run.ID, run.TenantID, run.ChannelInstanceID, run.ChannelName, run.AgentID, run.UserID,
		run.HistoryKey, run.Trigger, run.Status, run.SourceStartID, run.SourceEndID,
		run.SourceStartAt, run.SourceEndAt, run.MessageCount, run.RedactionCount,
		run.RedactionTypes, run.ItemCount, run.ErrorMessage, run.StartedAt, run.CompletedAt,
		run.CreatedAt, run.UpdatedAt,
	).Scan(&run.ID)
	return err
}

func (s *PGChannelMemoryExtractionStore) GetRun(ctx context.Context, id uuid.UUID) (*store.ChannelMemoryExtractionRun, error) {
	tClause, tArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+channelMemoryRunCols+` FROM channel_memory_extraction_runs WHERE id = $1`+tClause, append([]any{id}, tArgs...)...)
	return scanChannelMemoryRun(row)
}

func (s *PGChannelMemoryExtractionStore) ListRuns(ctx context.Context, opts store.ChannelMemoryRunListOptions) ([]store.ChannelMemoryExtractionRun, error) {
	where, args, next, err := scopeClause(ctx, 1)
	if err != nil {
		return nil, err
	}
	conds := []string{"1=1" + where}
	if opts.ChannelInstanceID != uuid.Nil {
		conds = append(conds, fmt.Sprintf("channel_instance_id = $%d", next))
		args = append(args, opts.ChannelInstanceID)
		next++
	}
	if opts.HistoryKey != "" {
		conds = append(conds, fmt.Sprintf("history_key = $%d", next))
		args = append(args, opts.HistoryKey)
		next++
	}
	if opts.Status != "" {
		conds = append(conds, fmt.Sprintf("status = $%d", next))
		args = append(args, opts.Status)
		next++
	}
	limit := opts.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `SELECT `+channelMemoryRunCols+`
		FROM channel_memory_extraction_runs WHERE `+strings.Join(conds, " AND ")+`
		ORDER BY created_at DESC LIMIT $`+fmt.Sprint(next), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.ChannelMemoryExtractionRun
	for rows.Next() {
		run, err := scanChannelMemoryRunRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *run)
	}
	return out, rows.Err()
}

func (s *PGChannelMemoryExtractionStore) UpdateRun(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	return execMapUpdateWhereTenant(ctx, s.db, "channel_memory_extraction_runs", updates, id, tid)
}

func (s *PGChannelMemoryExtractionStore) CreateItem(ctx context.Context, item *store.ChannelMemoryExtractionItem) error {
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
	err := s.db.QueryRowContext(ctx, `INSERT INTO channel_memory_extraction_items
		(`+channelMemoryItemCols+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
		ON CONFLICT (tenant_id, run_id, item_hash) DO UPDATE SET updated_at = EXCLUDED.updated_at
		RETURNING id`,
		item.ID, item.TenantID, item.RunID, item.ChannelInstanceID, item.AgentID, item.UserID,
		item.ItemHash, item.ItemType, item.Summary, item.Topics, item.Entities, item.Confidence,
		item.SourceID, item.Status, item.ApprovedBy, item.ApprovedAt, item.RejectedBy,
		item.RejectedAt, item.DeletedAt, item.WrittenAt, item.EpisodicID, item.CreatedAt, item.UpdatedAt,
	).Scan(&item.ID)
	return err
}

func (s *PGChannelMemoryExtractionStore) GetItem(ctx context.Context, id uuid.UUID) (*store.ChannelMemoryExtractionItem, error) {
	tClause, tArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+channelMemoryItemCols+` FROM channel_memory_extraction_items WHERE id = $1`+tClause, append([]any{id}, tArgs...)...)
	return scanChannelMemoryItem(row)
}

func (s *PGChannelMemoryExtractionStore) ListItems(ctx context.Context, opts store.ChannelMemoryItemListOptions) ([]store.ChannelMemoryExtractionItem, error) {
	where, args, next, err := scopeClause(ctx, 1)
	if err != nil {
		return nil, err
	}
	conds := []string{"1=1" + where}
	if opts.ChannelInstanceID != uuid.Nil {
		conds = append(conds, fmt.Sprintf("channel_instance_id = $%d", next))
		args = append(args, opts.ChannelInstanceID)
		next++
	}
	if opts.RunID != uuid.Nil {
		conds = append(conds, fmt.Sprintf("run_id = $%d", next))
		args = append(args, opts.RunID)
		next++
	}
	if opts.Status != "" {
		conds = append(conds, fmt.Sprintf("status = $%d", next))
		args = append(args, opts.Status)
		next++
	}
	limit := opts.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `SELECT `+channelMemoryItemCols+`
		FROM channel_memory_extraction_items WHERE `+strings.Join(conds, " AND ")+`
		ORDER BY created_at DESC LIMIT $`+fmt.Sprint(next), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.ChannelMemoryExtractionItem
	for rows.Next() {
		item, err := scanChannelMemoryItemRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func (s *PGChannelMemoryExtractionStore) UpdateItem(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	tid, err := requireTenantID(ctx)
	if err != nil {
		return err
	}
	return execMapUpdateWhereTenant(ctx, s.db, "channel_memory_extraction_items", updates, id, tid)
}

type channelMemoryScanner interface {
	Scan(dest ...any) error
}

func scanChannelMemoryRun(row channelMemoryScanner) (*store.ChannelMemoryExtractionRun, error) {
	var r store.ChannelMemoryExtractionRun
	if err := row.Scan(&r.ID, &r.TenantID, &r.ChannelInstanceID, &r.ChannelName, &r.AgentID, &r.UserID, &r.HistoryKey, &r.Trigger, &r.Status, &r.SourceStartID, &r.SourceEndID, &r.SourceStartAt, &r.SourceEndAt, &r.MessageCount, &r.RedactionCount, &r.RedactionTypes, &r.ItemCount, &r.ErrorMessage, &r.StartedAt, &r.CompletedAt, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

func scanChannelMemoryRunRows(rows *sql.Rows) (*store.ChannelMemoryExtractionRun, error) {
	return scanChannelMemoryRun(rows)
}

func scanChannelMemoryItem(row channelMemoryScanner) (*store.ChannelMemoryExtractionItem, error) {
	var i store.ChannelMemoryExtractionItem
	if err := row.Scan(&i.ID, &i.TenantID, &i.RunID, &i.ChannelInstanceID, &i.AgentID, &i.UserID, &i.ItemHash, &i.ItemType, &i.Summary, &i.Topics, &i.Entities, &i.Confidence, &i.SourceID, &i.Status, &i.ApprovedBy, &i.ApprovedAt, &i.RejectedBy, &i.RejectedAt, &i.DeletedAt, &i.WrittenAt, &i.EpisodicID, &i.CreatedAt, &i.UpdatedAt); err != nil {
		return nil, err
	}
	return &i, nil
}

func scanChannelMemoryItemRows(rows *sql.Rows) (*store.ChannelMemoryExtractionItem, error) {
	return scanChannelMemoryItem(rows)
}
