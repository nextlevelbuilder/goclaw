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

// SQLiteSnapshotStore implements store.SnapshotStore backed by SQLite.
type SQLiteSnapshotStore struct {
	db *sql.DB
}

func NewSQLiteSnapshotStore(db *sql.DB) *SQLiteSnapshotStore {
	return &SQLiteSnapshotStore{db: db}
}

const snapshotFieldCount = 22

// sqliteSnapshotBatchSize limits each INSERT to stay under SQLite's 999-variable limit (999 / 22 ≈ 45 → use 40).
const sqliteSnapshotBatchSize = 40

func (s *SQLiteSnapshotStore) UpsertSnapshots(ctx context.Context, snapshots []store.UsageSnapshot) error {
	if len(snapshots) == 0 {
		return nil
	}
	for start := 0; start < len(snapshots); start += sqliteSnapshotBatchSize {
		end := start + sqliteSnapshotBatchSize
		if end > len(snapshots) {
			end = len(snapshots)
		}
		if err := s.upsertBatch(ctx, snapshots[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteSnapshotStore) upsertBatch(ctx context.Context, snapshots []store.UsageSnapshot) error {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}

	placeholderRow := "(" + strings.Repeat("?, ", snapshotFieldCount-1) + "?)"
	vals := make([]string, len(snapshots))
	var args []any

	for i, snap := range snapshots {
		vals[i] = placeholderRow
		args = append(args,
			snap.BucketHour, nilUUID(snap.AgentID), snap.Provider, snap.Model, snap.Channel,
			snap.InputTokens, snap.OutputTokens, snap.CacheReadTokens, snap.CacheCreateTokens, snap.ThinkingTokens,
			snap.TotalCost, snap.RequestCount, snap.LLMCallCount, snap.ToolCallCount,
			snap.ErrorCount, snap.UniqueUsers, snap.AvgDurationMS,
			snap.MemoryDocs, snap.MemoryChunks, snap.KGEntities, snap.KGRelations,
			tenantID,
		)
	}

	query := `INSERT INTO usage_snapshots (
		bucket_hour, agent_id, provider, model, channel,
		input_tokens, output_tokens, cache_read_tokens, cache_create_tokens, thinking_tokens,
		total_cost, request_count, llm_call_count, tool_call_count,
		error_count, unique_users, avg_duration_ms,
		memory_docs, memory_chunks, kg_entities, kg_relations,
		tenant_id
	) VALUES ` + strings.Join(vals, ", ") + `
	ON CONFLICT (bucket_hour, COALESCE(agent_id, '00000000-0000-0000-0000-000000000000'), COALESCE(provider, ''), COALESCE(model, ''), COALESCE(channel, ''), tenant_id)
	DO UPDATE SET
		input_tokens        = excluded.input_tokens,
		output_tokens       = excluded.output_tokens,
		cache_read_tokens   = excluded.cache_read_tokens,
		cache_create_tokens = excluded.cache_create_tokens,
		thinking_tokens     = excluded.thinking_tokens,
		total_cost          = excluded.total_cost,
		request_count       = excluded.request_count,
		llm_call_count      = excluded.llm_call_count,
		tool_call_count     = excluded.tool_call_count,
		error_count         = excluded.error_count,
		unique_users        = excluded.unique_users,
		avg_duration_ms     = excluded.avg_duration_ms,
		memory_docs         = excluded.memory_docs,
		memory_chunks       = excluded.memory_chunks,
		kg_entities         = excluded.kg_entities,
		kg_relations        = excluded.kg_relations`

	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *SQLiteSnapshotStore) GetTimeSeries(ctx context.Context, q store.SnapshotQuery) ([]store.SnapshotTimeSeries, error) {
	bucketExpr := "bucket_hour"
	if q.GroupBy == "day" {
		bucketExpr = "strftime('%Y-%m-%d 00:00:00', bucket_hour)"
	}

	where, args := buildSnapshotWhere(ctx, q)

	query := fmt.Sprintf(`SELECT
		bucket_time,
		SUM(input_tokens), SUM(output_tokens),
		SUM(cache_read_tokens), SUM(cache_create_tokens), SUM(thinking_tokens),
		SUM(total_cost),
		SUM(request_count), SUM(llm_call_count), SUM(tool_call_count),
		SUM(error_count), SUM(unique_users),
		CASE WHEN SUM(request_count) > 0
			THEN SUM(avg_duration_ms * request_count) / SUM(request_count)
			ELSE 0 END,
		SUM(memory_docs), SUM(memory_chunks),
		SUM(kg_entities), SUM(kg_relations)
	FROM (
		SELECT
			%s as bucket_time,
			CASE WHEN provider != '' THEN input_tokens ELSE 0 END as input_tokens,
			CASE WHEN provider != '' THEN output_tokens ELSE 0 END as output_tokens,
			CASE WHEN provider != '' THEN cache_read_tokens ELSE 0 END as cache_read_tokens,
			CASE WHEN provider != '' THEN cache_create_tokens ELSE 0 END as cache_create_tokens,
			CASE WHEN provider != '' THEN thinking_tokens ELSE 0 END as thinking_tokens,
			CASE WHEN provider != '' THEN total_cost ELSE 0 END as total_cost,
			CASE WHEN provider != '' THEN llm_call_count ELSE 0 END as llm_call_count,
			CASE WHEN provider = '' AND model = '' THEN request_count ELSE 0 END as request_count,
			CASE WHEN provider = '' AND model = '' THEN tool_call_count ELSE 0 END as tool_call_count,
			CASE WHEN provider = '' AND model = '' THEN error_count ELSE 0 END as error_count,
			CASE WHEN provider = '' AND model = '' THEN unique_users ELSE 0 END as unique_users,
			CASE WHEN provider = '' AND model = '' THEN avg_duration_ms ELSE 0 END as avg_duration_ms,
			CASE WHEN provider = '' AND model = '' THEN memory_docs ELSE 0 END as memory_docs,
			CASE WHEN provider = '' AND model = '' THEN memory_chunks ELSE 0 END as memory_chunks,
			CASE WHEN provider = '' AND model = '' THEN kg_entities ELSE 0 END as kg_entities,
			CASE WHEN provider = '' AND model = '' THEN kg_relations ELSE 0 END as kg_relations
		FROM usage_snapshots
		%s
	) sub
	GROUP BY bucket_time
	ORDER BY bucket_time`, bucketExpr, where)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get timeseries: %w", err)
	}
	defer rows.Close()

	var result []store.SnapshotTimeSeries
	for rows.Next() {
		var ts store.SnapshotTimeSeries
		if err := rows.Scan(
			&ts.BucketTime,
			&ts.InputTokens, &ts.OutputTokens,
			&ts.CacheReadTokens, &ts.CacheCreateTokens, &ts.ThinkingTokens,
			&ts.TotalCost,
			&ts.RequestCount, &ts.LLMCallCount, &ts.ToolCallCount,
			&ts.ErrorCount, &ts.UniqueUsers, &ts.AvgDurationMS,
			&ts.MemoryDocs, &ts.MemoryChunks,
			&ts.KGEntities, &ts.KGRelations,
		); err != nil {
			return nil, fmt.Errorf("scan timeseries: %w", err)
		}
		result = append(result, ts)
	}
	return result, rows.Err()
}

func (s *SQLiteSnapshotStore) GetBreakdown(ctx context.Context, q store.SnapshotQuery) ([]store.SnapshotBreakdown, error) {
	groupBy := q.GroupBy
	if groupBy == "" {
		groupBy = "provider"
	}

	var groupCol, orderExpr, extraFilter string
	switch groupBy {
	case "provider":
		groupCol = "provider"
		orderExpr = "SUM(CASE WHEN provider != '' THEN input_tokens ELSE 0 END) DESC"
		extraFilter = " AND provider != '' AND model != ''"
	case "model":
		groupCol = "model"
		orderExpr = "SUM(CASE WHEN provider != '' THEN input_tokens ELSE 0 END) DESC"
		extraFilter = " AND provider != '' AND model != ''"
	case "channel":
		groupCol = "channel"
		orderExpr = "SUM(CASE WHEN provider = '' AND model = '' THEN request_count ELSE 0 END) DESC"
		extraFilter = " AND channel != ''"
	case "agent":
		groupCol = "agent_id" // already TEXT in SQLite — no ::TEXT cast needed
		orderExpr = "SUM(CASE WHEN provider != '' THEN input_tokens ELSE 0 END) DESC"
		extraFilter = ""
	default:
		groupCol = "provider"
		orderExpr = "SUM(input_tokens) DESC"
		extraFilter = " AND provider != '' AND model != ''"
	}

	where, args := buildSnapshotWhere(ctx, q)
	if where == "" {
		where = " WHERE 1=1"
	}
	where += extraFilter

	query := fmt.Sprintf(`SELECT
		%s as key,
		SUM(CASE WHEN provider != '' THEN input_tokens ELSE 0 END),
		SUM(CASE WHEN provider != '' THEN output_tokens ELSE 0 END),
		SUM(CASE WHEN provider != '' THEN cache_read_tokens ELSE 0 END),
		SUM(CASE WHEN provider != '' THEN cache_create_tokens ELSE 0 END),
		SUM(CASE WHEN provider != '' THEN total_cost ELSE 0 END),
		SUM(CASE WHEN provider = '' AND model = '' THEN request_count ELSE 0 END),
		SUM(CASE WHEN provider != '' THEN llm_call_count ELSE 0 END),
		SUM(CASE WHEN provider = '' AND model = '' THEN tool_call_count ELSE 0 END),
		SUM(CASE WHEN provider = '' AND model = '' THEN error_count ELSE 0 END),
		CASE WHEN SUM(CASE WHEN provider = '' AND model = '' THEN request_count ELSE 0 END) > 0
			THEN SUM(CASE WHEN provider = '' AND model = '' THEN avg_duration_ms * request_count ELSE 0 END) /
				SUM(CASE WHEN provider = '' AND model = '' THEN request_count ELSE 0 END)
			ELSE 0 END
	FROM usage_snapshots
	%s
	GROUP BY %s
	ORDER BY %s`, groupCol, where, groupCol, orderExpr)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get breakdown: %w", err)
	}
	defer rows.Close()

	var result []store.SnapshotBreakdown
	for rows.Next() {
		var b store.SnapshotBreakdown
		if err := rows.Scan(
			&b.Key,
			&b.InputTokens, &b.OutputTokens,
			&b.CacheReadTokens, &b.CacheCreateTokens,
			&b.TotalCost,
			&b.RequestCount, &b.LLMCallCount, &b.ToolCallCount,
			&b.ErrorCount, &b.AvgDurationMS,
		); err != nil {
			return nil, fmt.Errorf("scan breakdown: %w", err)
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

func (s *SQLiteSnapshotStore) GetLatestBucket(ctx context.Context) (*time.Time, error) {
	var nt nullSqliteTime
	err := s.db.QueryRowContext(ctx, `SELECT MAX(bucket_hour) FROM usage_snapshots`).Scan(&nt)
	if err != nil {
		return nil, fmt.Errorf("get latest bucket: %w", err)
	}
	if !nt.Valid {
		return nil, nil
	}
	return &nt.Time, nil
}

// buildSnapshotWhere builds a WHERE clause with ? placeholders for SQLite.
func buildSnapshotWhere(ctx context.Context, q store.SnapshotQuery) (string, []any) {
	var conds []string
	var args []any

	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID != uuid.Nil {
			conds = append(conds, "tenant_id = ?")
			args = append(args, tenantID)
		}
	}
	if !q.From.IsZero() {
		conds = append(conds, "bucket_hour >= ?")
		args = append(args, q.From)
	}
	if !q.To.IsZero() {
		conds = append(conds, "bucket_hour < ?")
		args = append(args, q.To)
	}
	if q.AgentID != nil {
		conds = append(conds, "agent_id = ?")
		args = append(args, *q.AgentID)
	}
	if q.Provider != "" {
		conds = append(conds, "provider = ?")
		args = append(args, q.Provider)
	}
	if q.Model != "" {
		conds = append(conds, "model = ?")
		args = append(args, q.Model)
	}
	if q.Channel != "" {
		conds = append(conds, "channel = ?")
		args = append(args, q.Channel)
	}

	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}
