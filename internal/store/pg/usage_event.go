package pg

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type PGUsageEventStore struct {
	db *sql.DB
}

func NewPGUsageEventStore(db *sql.DB) *PGUsageEventStore {
	return &PGUsageEventStore{db: db}
}

const usageEventFieldCount = 28
const usageRollupFieldCount = 21

func (s *PGUsageEventStore) InsertEvent(ctx context.Context, event *store.UsageEvent) error {
	if event == nil {
		return nil
	}
	return s.InsertEvents(ctx, []store.UsageEvent{*event})
}

func (s *PGUsageEventStore) InsertEvents(ctx context.Context, events []store.UsageEvent) error {
	if len(events) == 0 {
		return nil
	}
	for i := range events {
		prepareUsageEvent(ctx, &events[i])
	}

	vals := make([]string, len(events))
	args := make([]any, 0, len(events)*usageEventFieldCount)
	for i, event := range events {
		base := i * usageEventFieldCount
		placeholders := make([]string, usageEventFieldCount)
		for j := range usageEventFieldCount {
			placeholders[j] = fmt.Sprintf("$%d", base+j+1)
		}
		vals[i] = "(" + strings.Join(placeholders, ", ") + ")"
		args = append(args,
			event.ID, event.TenantID, event.EventTime, event.BucketHour,
			event.EventType, event.ResourceType, event.ResourceName, event.ResourceID, event.Source,
			nilUUID(event.AgentID), nilUUID(event.TeamID), nilUUID(event.TraceID), nilUUID(event.SpanID),
			event.RunID, event.SessionKey, event.Channel, event.Provider, event.Model, event.Status,
			event.InputTokens, event.OutputTokens, event.TotalTokens, event.CostUSD,
			event.DurationMS, event.CallCount, event.ErrorCount, jsonOrNull(event.Metadata), event.CreatedAt,
		)
	}

	query := `INSERT INTO usage_events (
		id, tenant_id, event_time, bucket_hour,
		event_type, resource_type, resource_name, resource_id, source,
		agent_id, team_id, trace_id, span_id,
		run_id, session_key, channel, provider, model, status,
		input_tokens, output_tokens, total_tokens, cost_usd,
		duration_ms, call_count, error_count, metadata, created_at
	) VALUES ` + strings.Join(vals, ", ") + `
	ON CONFLICT DO NOTHING`
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *PGUsageEventStore) RefreshEventRollupHour(ctx context.Context, bucketHour time.Time) error {
	start := bucketHour.UTC().Truncate(time.Hour)
	end := start.Add(time.Hour)
	rows, err := s.db.QueryContext(ctx, `SELECT
		tenant_id,
		bucket_hour,
		event_type,
		resource_type,
		resource_name,
		source,
		agent_id,
		channel,
		provider,
		model,
		status,
		COALESCE(SUM(input_tokens), 0),
		COALESCE(SUM(output_tokens), 0),
		COALESCE(SUM(total_tokens), 0),
		COALESCE(SUM(cost_usd), 0),
		CASE WHEN COALESCE(SUM(call_count), 0) > 0
			THEN COALESCE(SUM(duration_ms * call_count), 0) / SUM(call_count)
			ELSE 0 END,
		COALESCE(SUM(call_count), 0),
		COALESCE(SUM(error_count), 0)
	FROM usage_events
	WHERE event_time >= $1 AND event_time < $2
	GROUP BY tenant_id, bucket_hour, event_type, resource_type, resource_name, source, agent_id, channel, provider, model, status`,
		start, end)
	if err != nil {
		return fmt.Errorf("aggregate usage event rollup: %w", err)
	}
	defer rows.Close()

	now := time.Now().UTC()
	var rollups []store.UsageEventRollup
	for rows.Next() {
		rollup := store.UsageEventRollup{ID: uuid.New(), CreatedAt: now, UpdatedAt: now}
		if err := rows.Scan(
			&rollup.TenantID, &rollup.BucketHour, &rollup.EventType, &rollup.ResourceType,
			&rollup.ResourceName, &rollup.Source, &rollup.AgentID, &rollup.Channel,
			&rollup.Provider, &rollup.Model, &rollup.Status,
			&rollup.InputTokens, &rollup.OutputTokens, &rollup.TotalTokens, &rollup.CostUSD,
			&rollup.DurationMS, &rollup.CallCount, &rollup.ErrorCount,
		); err != nil {
			return fmt.Errorf("scan usage event rollup: %w", err)
		}
		rollups = append(rollups, rollup)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return s.upsertEventRollups(ctx, rollups)
}

func (s *PGUsageEventStore) GetLatestEventRollupBucket(ctx context.Context) (*time.Time, error) {
	var t sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT MAX(bucket_hour) FROM usage_event_rollups`).Scan(&t)
	if err != nil {
		return nil, fmt.Errorf("get latest event rollup bucket: %w", err)
	}
	if !t.Valid {
		return nil, nil
	}
	return &t.Time, nil
}

func (s *PGUsageEventStore) upsertEventRollups(ctx context.Context, rollups []store.UsageEventRollup) error {
	if len(rollups) == 0 {
		return nil
	}
	vals := make([]string, len(rollups))
	args := make([]any, 0, len(rollups)*usageRollupFieldCount)
	for i, rollup := range rollups {
		base := i * usageRollupFieldCount
		placeholders := make([]string, usageRollupFieldCount)
		for j := range usageRollupFieldCount {
			placeholders[j] = fmt.Sprintf("$%d", base+j+1)
		}
		vals[i] = "(" + strings.Join(placeholders, ", ") + ")"
		args = append(args,
			rollup.ID, rollup.TenantID, rollup.BucketHour, rollup.EventType, rollup.ResourceType,
			rollup.ResourceName, rollup.Source, nilUUID(rollup.AgentID), rollup.Channel,
			rollup.Provider, rollup.Model, rollup.Status, rollup.InputTokens, rollup.OutputTokens,
			rollup.TotalTokens, rollup.CostUSD, rollup.DurationMS, rollup.CallCount,
			rollup.ErrorCount, rollup.CreatedAt, rollup.UpdatedAt,
		)
	}
	query := `INSERT INTO usage_event_rollups (
		id, tenant_id, bucket_hour, event_type, resource_type, resource_name, source,
		agent_id, channel, provider, model, status,
		input_tokens, output_tokens, total_tokens, cost_usd,
		duration_ms, call_count, error_count, created_at, updated_at
	) VALUES ` + strings.Join(vals, ", ") + `
	ON CONFLICT (
		tenant_id,
		bucket_hour,
		event_type,
		resource_type,
		resource_name,
		source,
		COALESCE(agent_id, '00000000-0000-0000-0000-000000000000'::uuid),
		channel,
		provider,
		model,
		status
	) DO UPDATE SET
		input_tokens = EXCLUDED.input_tokens,
		output_tokens = EXCLUDED.output_tokens,
		total_tokens = EXCLUDED.total_tokens,
		cost_usd = EXCLUDED.cost_usd,
		duration_ms = EXCLUDED.duration_ms,
		call_count = EXCLUDED.call_count,
		error_count = EXCLUDED.error_count,
		updated_at = EXCLUDED.updated_at`
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *PGUsageEventStore) GetEventTimeSeries(ctx context.Context, q store.UsageEventQuery) ([]store.UsageEventTimeSeries, error) {
	bucketExpr := "bucket_hour"
	if q.GroupBy == "day" {
		bucketExpr = "date_trunc('day', bucket_hour)"
	}
	where, args := buildUsageEventWhere(ctx, q, "bucket_hour")
	query := fmt.Sprintf(`SELECT
		%s AS bucket_time,
		COALESCE(SUM(call_count), 0),
		COALESCE(SUM(error_count), 0),
		COALESCE(SUM(input_tokens), 0),
		COALESCE(SUM(output_tokens), 0),
		COALESCE(SUM(total_tokens), 0),
		COALESCE(SUM(cost_usd), 0),
		CASE WHEN COALESCE(SUM(call_count), 0) > 0
			THEN COALESCE(SUM(duration_ms * call_count), 0) / SUM(call_count)
			ELSE 0 END
	FROM usage_event_rollups
	%s
	GROUP BY bucket_time
	ORDER BY bucket_time`, bucketExpr, where)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get usage event timeseries: %w", err)
	}
	defer rows.Close()

	var result []store.UsageEventTimeSeries
	for rows.Next() {
		var point store.UsageEventTimeSeries
		if err := rows.Scan(
			&point.BucketTime, &point.Calls, &point.Errors,
			&point.InputTokens, &point.OutputTokens, &point.TotalTokens,
			&point.CostUSD, &point.AvgDurationMS,
		); err != nil {
			return nil, fmt.Errorf("scan usage event timeseries: %w", err)
		}
		result = append(result, point)
	}
	return result, rows.Err()
}

func (s *PGUsageEventStore) GetEventBreakdown(ctx context.Context, q store.UsageEventQuery) ([]store.UsageEventBreakdown, error) {
	groupCol := usageEventGroupColumn(q.GroupBy)
	where, args := buildUsageEventWhere(ctx, q, "bucket_hour")
	if where == "" {
		where = " WHERE 1=1"
	}
	limit := q.Limit
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	args = append(args, limit)
	limitPlaceholder := fmt.Sprintf("$%d", len(args))

	query := fmt.Sprintf(`SELECT
		%s AS key,
		MIN(event_type),
		MIN(resource_type),
		MIN(resource_name),
		MIN(source),
		COALESCE(SUM(call_count), 0),
		COALESCE(SUM(error_count), 0),
		COALESCE(SUM(input_tokens), 0),
		COALESCE(SUM(output_tokens), 0),
		COALESCE(SUM(total_tokens), 0),
		COALESCE(SUM(cost_usd), 0),
		CASE WHEN COALESCE(SUM(call_count), 0) > 0
			THEN COALESCE(SUM(duration_ms * call_count), 0) / SUM(call_count)
			ELSE 0 END
	FROM usage_event_rollups
	%s
	GROUP BY %s
	ORDER BY SUM(call_count) DESC, key ASC
	LIMIT %s`, groupCol, where, groupCol, limitPlaceholder)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get usage event breakdown: %w", err)
	}
	defer rows.Close()

	var result []store.UsageEventBreakdown
	for rows.Next() {
		var row store.UsageEventBreakdown
		if err := rows.Scan(
			&row.Key, &row.EventType, &row.ResourceType, &row.ResourceName, &row.Source,
			&row.Calls, &row.Errors, &row.InputTokens, &row.OutputTokens, &row.TotalTokens,
			&row.CostUSD, &row.AvgDurationMS,
		); err != nil {
			return nil, fmt.Errorf("scan usage event breakdown: %w", err)
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func (s *PGUsageEventStore) GetEventSummary(ctx context.Context, q store.UsageEventQuery) (*store.UsageEventSummary, error) {
	where, args := buildUsageEventWhere(ctx, q, "bucket_hour")
	query := `SELECT
		COALESCE(SUM(call_count), 0),
		COALESCE(SUM(error_count), 0),
		COALESCE(SUM(input_tokens), 0),
		COALESCE(SUM(output_tokens), 0),
		COALESCE(SUM(total_tokens), 0),
		COALESCE(SUM(cost_usd), 0),
		CASE WHEN COALESCE(SUM(call_count), 0) > 0
			THEN COALESCE(SUM(duration_ms * call_count), 0) / SUM(call_count)
			ELSE 0 END
	FROM usage_event_rollups` + where
	var summary store.UsageEventSummary
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&summary.Calls, &summary.Errors, &summary.InputTokens, &summary.OutputTokens,
		&summary.TotalTokens, &summary.CostUSD, &summary.AvgDurationMS,
	); err != nil {
		return nil, fmt.Errorf("get usage event summary: %w", err)
	}
	return &summary, nil
}

func prepareUsageEvent(ctx context.Context, event *store.UsageEvent) {
	if event.ID == uuid.Nil {
		event.ID = uuid.New()
	}
	if event.TenantID == uuid.Nil {
		event.TenantID = store.TenantIDFromContext(ctx)
	}
	if event.TenantID == uuid.Nil {
		event.TenantID = store.MasterTenantID
	}
	if event.EventTime.IsZero() {
		event.EventTime = time.Now().UTC()
	}
	event.EventTime = event.EventTime.UTC()
	if event.BucketHour.IsZero() {
		event.BucketHour = event.EventTime.Truncate(time.Hour)
	}
	if event.CallCount <= 0 {
		event.CallCount = 1
	}
	if event.Status == "" {
		event.Status = "completed"
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
}

func buildUsageEventWhere(ctx context.Context, q store.UsageEventQuery, timeColumn string) (string, []any) {
	var conds []string
	var args []any
	idx := 1

	if !store.IsCrossTenant(ctx) {
		if tenantID := store.TenantIDFromContext(ctx); tenantID != uuid.Nil {
			conds = append(conds, fmt.Sprintf("tenant_id = $%d", idx))
			args = append(args, tenantID)
			idx++
		}
	}
	add := func(col string, value any) {
		conds = append(conds, fmt.Sprintf("%s = $%d", col, idx))
		args = append(args, value)
		idx++
	}
	if !q.From.IsZero() {
		conds = append(conds, fmt.Sprintf("%s >= $%d", timeColumn, idx))
		args = append(args, q.From.UTC())
		idx++
	}
	if !q.To.IsZero() {
		conds = append(conds, fmt.Sprintf("%s < $%d", timeColumn, idx))
		args = append(args, q.To.UTC())
		idx++
	}
	if q.AgentID != nil {
		add("agent_id", *q.AgentID)
	}
	if q.Channel != "" {
		add("channel", q.Channel)
	}
	if q.EventType != "" {
		add("event_type", q.EventType)
	}
	if q.ResourceType != "" {
		add("resource_type", q.ResourceType)
	}
	if q.ResourceName != "" {
		add("resource_name", q.ResourceName)
	}
	if q.Provider != "" {
		add("provider", q.Provider)
	}
	if q.Model != "" {
		add("model", q.Model)
	}
	if q.Status != "" {
		add("status", q.Status)
	}
	if q.Source != "" {
		add("source", q.Source)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func usageEventGroupColumn(groupBy string) string {
	switch groupBy {
	case "event_type":
		return "event_type"
	case "resource_type":
		return "resource_type"
	case "source":
		return "source"
	case "status":
		return "status"
	case "agent":
		return "COALESCE(agent_id::TEXT, '')"
	case "channel":
		return "channel"
	case "provider":
		return "provider"
	case "model":
		return "model"
	default:
		return "resource_name"
	}
}
