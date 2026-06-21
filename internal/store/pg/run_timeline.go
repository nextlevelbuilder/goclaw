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

// PGRunTimelineStore implements store.RunTimelineStore backed by PostgreSQL.
type PGRunTimelineStore struct {
	db *sql.DB
}

func NewPGRunTimelineStore(db *sql.DB) *PGRunTimelineStore {
	return &PGRunTimelineStore{db: db}
}

func (s *PGRunTimelineStore) AppendRunTimelineItem(ctx context.Context, item *store.RunTimelineItem) error {
	if item.ID == uuid.Nil {
		item.ID = store.GenNewID()
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now()
	}
	tenantID := tenantIDForInsert(ctx)
	item.TenantID = tenantID
	metadata := item.Metadata
	if len(metadata) == 0 {
		metadata = []byte(`{}`)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO run_timeline_items
		 (id, tenant_id, run_id, session_key, agent_id, user_id, channel, chat_id, seq,
		  item_type, status, title, preview, content, tool_name, tool_call_id, trace_id, span_id,
		  metadata, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9,
		  $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
		 ON CONFLICT (tenant_id, run_id, seq) DO UPDATE SET
		  session_key = EXCLUDED.session_key,
		  agent_id = EXCLUDED.agent_id,
		  user_id = EXCLUDED.user_id,
		  channel = EXCLUDED.channel,
		  chat_id = EXCLUDED.chat_id,
		  item_type = EXCLUDED.item_type,
		  status = EXCLUDED.status,
		  title = EXCLUDED.title,
		  preview = EXCLUDED.preview,
		  content = '',
		  tool_name = EXCLUDED.tool_name,
		  tool_call_id = EXCLUDED.tool_call_id,
		  trace_id = EXCLUDED.trace_id,
		  span_id = EXCLUDED.span_id,
		  metadata = EXCLUDED.metadata,
		  created_at = EXCLUDED.created_at`,
		item.ID, tenantID, item.RunID, item.SessionKey, nilUUID(item.AgentID), nilStr(item.UserID),
		nilStr(item.Channel), nilStr(item.ChatID), item.Seq, item.ItemType, nilStr(item.Status),
		nilStr(item.Title), nilStr(item.Preview), "", nilStr(item.ToolName), nilStr(item.ToolCallID),
		nilUUID(item.TraceID), nilUUID(item.SpanID), jsonOrEmpty(metadata), item.CreatedAt,
	)
	if err == nil {
		item.Content = ""
	}
	return err
}

func (s *PGRunTimelineStore) ListRunTimelineItems(ctx context.Context, opts store.RunTimelineListOpts) ([]store.RunTimelineItem, error) {
	where, args := buildRunTimelineWhere(ctx, opts)
	limit := opts.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	q := `SELECT id, tenant_id, run_id, session_key, agent_id, user_id, channel, chat_id, seq,
		 item_type, status, title, preview, COALESCE(content, '') AS content, tool_name, tool_call_id,
		 trace_id, span_id, COALESCE(metadata, '{}'::jsonb) AS metadata, created_at
		 FROM run_timeline_items` + where +
		runTimelineOrderBy(opts) +
		fmt.Sprintf(" OFFSET %d LIMIT %d", opts.Offset, limit)

	var rows []runTimelineRow
	if err := pkgSqlxDB.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil, err
	}
	items := make([]store.RunTimelineItem, len(rows))
	for i, row := range rows {
		items[i] = row.toStore()
	}
	return items, nil
}

func runTimelineOrderBy(opts store.RunTimelineListOpts) string {
	if opts.RunID != "" {
		return " ORDER BY seq ASC, created_at ASC"
	}
	return " ORDER BY created_at ASC, seq ASC"
}

func buildRunTimelineWhere(ctx context.Context, opts store.RunTimelineListOpts) (string, []any) {
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
	if opts.SessionKey != "" {
		conditions = append(conditions, fmt.Sprintf("session_key = $%d", argIdx))
		args = append(args, opts.SessionKey)
	}
	if len(conditions) == 0 {
		return " WHERE 1=0", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

type runTimelineRow struct {
	ID         uuid.UUID       `db:"id"`
	TenantID   uuid.UUID       `db:"tenant_id"`
	RunID      string          `db:"run_id"`
	SessionKey string          `db:"session_key"`
	AgentID    *uuid.UUID      `db:"agent_id"`
	UserID     *string         `db:"user_id"`
	Channel    *string         `db:"channel"`
	ChatID     *string         `db:"chat_id"`
	Seq        int             `db:"seq"`
	ItemType   string          `db:"item_type"`
	Status     *string         `db:"status"`
	Title      *string         `db:"title"`
	Preview    *string         `db:"preview"`
	Content    string          `db:"content"`
	ToolName   *string         `db:"tool_name"`
	ToolCallID *string         `db:"tool_call_id"`
	TraceID    *uuid.UUID      `db:"trace_id"`
	SpanID     *uuid.UUID      `db:"span_id"`
	Metadata   json.RawMessage `db:"metadata"`
	CreatedAt  time.Time       `db:"created_at"`
}

func (r runTimelineRow) toStore() store.RunTimelineItem {
	return store.RunTimelineItem{
		ID: r.ID, TenantID: r.TenantID, RunID: r.RunID, SessionKey: r.SessionKey,
		AgentID: r.AgentID, UserID: derefStr(r.UserID), Channel: derefStr(r.Channel),
		ChatID: derefStr(r.ChatID), Seq: r.Seq, ItemType: r.ItemType,
		Status: derefStr(r.Status), Title: derefStr(r.Title), Preview: derefStr(r.Preview),
		Content: r.Content, ToolName: derefStr(r.ToolName), ToolCallID: derefStr(r.ToolCallID),
		TraceID: r.TraceID, SpanID: r.SpanID, Metadata: r.Metadata, CreatedAt: r.CreatedAt,
	}
}
