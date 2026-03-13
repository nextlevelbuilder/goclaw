package pg

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGSnapshotStore implements store.SnapshotStore backed by Postgres.
type PGSnapshotStore struct {
	db *sql.DB
}

func NewPGSnapshotStore(db *sql.DB) *PGSnapshotStore {
	return &PGSnapshotStore{db: db}
}

const snapshotFieldCount = 21

// maxBatchRows limits each INSERT to stay under PG's 65535 param limit (65535 / 21 ≈ 3120).
const maxBatchRows = 3000

func (s *PGSnapshotStore) UpsertSnapshots(ctx context.Context, snapshots []store.UsageSnapshot) error {
	if len(snapshots) == 0 {
		return nil
	}
	for start := 0; start < len(snapshots); start += maxBatchRows {
		end := min(start+maxBatchRows, len(snapshots))
		if err := s.upsertBatch(ctx, snapshots[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func (s *PGSnapshotStore) upsertBatch(ctx context.Context, snapshots []store.UsageSnapshot) error {
	var vals []string
	var args []any
	for i, snap := range snapshots {
		base := i * snapshotFieldCount
		placeholders := make([]string, snapshotFieldCount)
		for j := range snapshotFieldCount {
			placeholders[j] = fmt.Sprintf("$%d", base+j+1)
		}
		vals = append(vals, "("+strings.Join(placeholders, ", ")+")")
		args = append(args,
			snap.BucketHour, nilUUID(snap.AgentID), snap.Provider, snap.Model, snap.Channel,
			snap.InputTokens, snap.OutputTokens, snap.CacheReadTokens, snap.CacheCreateTokens, snap.ThinkingTokens,
			snap.TotalCost, snap.RequestCount, snap.LLMCallCount, snap.ToolCallCount,
			snap.ErrorCount, snap.UniqueUsers, snap.AvgDurationMS,
			snap.MemoryDocs, snap.MemoryChunks, snap.KGEntities, snap.KGRelations,
		)
	}

	query := `INSERT INTO usage_snapshots (
		bucket_hour, agent_id, provider, model, channel,
		input_tokens, output_tokens, cache_read_tokens, cache_create_tokens, thinking_tokens,
		total_cost, request_count, llm_call_count, tool_call_count,
		error_count, unique_users, avg_duration_ms,
		memory_docs, memory_chunks, kg_entities, kg_relations
	) VALUES ` + strings.Join(vals, ", ") + `
	ON CONFLICT (bucket_hour, COALESCE(agent_id, '00000000-0000-0000-0000-000000000000'), provider, model, channel)
	DO UPDATE SET
		input_tokens = EXCLUDED.input_tokens,
		output_tokens = EXCLUDED.output_tokens,
		cache_read_tokens = EXCLUDED.cache_read_tokens,
		cache_create_tokens = EXCLUDED.cache_create_tokens,
		thinking_tokens = EXCLUDED.thinking_tokens,
		total_cost = EXCLUDED.total_cost,
		request_count = EXCLUDED.request_count,
		llm_call_count = EXCLUDED.llm_call_count,
		tool_call_count = EXCLUDED.tool_call_count,
		error_count = EXCLUDED.error_count,
		unique_users = EXCLUDED.unique_users,
		avg_duration_ms = EXCLUDED.avg_duration_ms,
		memory_docs = EXCLUDED.memory_docs,
		memory_chunks = EXCLUDED.memory_chunks,
		kg_entities = EXCLUDED.kg_entities,
		kg_relations = EXCLUDED.kg_relations`

	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *PGSnapshotStore) GetTimeSeries(ctx context.Context, q store.SnapshotQuery) ([]store.SnapshotTimeSeries, error) {
	bucketExpr := "bucket_hour"
	if q.GroupBy == "day" {
		bucketExpr = "date_trunc('day', bucket_hour)"
	}

	where, args := buildSnapshotWhere(q)

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

func (s *PGSnapshotStore) GetBreakdown(ctx context.Context, q store.SnapshotQuery) ([]store.SnapshotBreakdown, error) {
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
		groupCol = "agent_id::TEXT"
		orderExpr = "SUM(CASE WHEN provider != '' THEN input_tokens ELSE 0 END) DESC"
		extraFilter = ""
	default:
		groupCol = "provider"
		orderExpr = "SUM(input_tokens) DESC"
		extraFilter = " AND provider != '' AND model != ''"
	}

	where, args := buildSnapshotWhere(q)
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

func (s *PGSnapshotStore) GetLatestBucket(ctx context.Context) (*time.Time, error) {
	var t sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT MAX(bucket_hour) FROM usage_snapshots`).Scan(&t)
	if err != nil {
		return nil, fmt.Errorf("get latest bucket: %w", err)
	}
	if !t.Valid {
		return nil, nil
	}
	return &t.Time, nil
}

// buildSnapshotWhere builds a dynamic WHERE clause from SnapshotQuery filters.
func buildSnapshotWhere(q store.SnapshotQuery) (string, []any) {
	var conds []string
	var args []any
	idx := 1

	if !q.From.IsZero() {
		conds = append(conds, fmt.Sprintf("bucket_hour >= $%d", idx))
		args = append(args, q.From)
		idx++
	}
	if !q.To.IsZero() {
		conds = append(conds, fmt.Sprintf("bucket_hour < $%d", idx))
		args = append(args, q.To)
		idx++
	}
	if q.AgentID != nil {
		conds = append(conds, fmt.Sprintf("agent_id = $%d", idx))
		args = append(args, *q.AgentID)
		idx++
	}
	if q.Provider != "" {
		conds = append(conds, fmt.Sprintf("provider = $%d", idx))
		args = append(args, q.Provider)
		idx++
	}
	if q.Model != "" {
		conds = append(conds, fmt.Sprintf("model = $%d", idx))
		args = append(args, q.Model)
		idx++
	}
	if q.Channel != "" {
		conds = append(conds, fmt.Sprintf("channel = $%d", idx))
		args = append(args, q.Channel)
		idx++
	}

	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}
