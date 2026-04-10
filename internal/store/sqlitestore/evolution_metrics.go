//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteEvolutionMetricsStore implements store.EvolutionMetricsStore backed by SQLite.
type SQLiteEvolutionMetricsStore struct {
	db *sql.DB
}

// NewSQLiteEvolutionMetricsStore creates a new SQLite-backed evolution metrics store.
func NewSQLiteEvolutionMetricsStore(db *sql.DB) *SQLiteEvolutionMetricsStore {
	return &SQLiteEvolutionMetricsStore{db: db}
}

func (s *SQLiteEvolutionMetricsStore) RecordMetric(ctx context.Context, m store.EvolutionMetric) error {
	tenantID := tenantIDForInsert(ctx)
	if tenantID == uuid.Nil {
		return fmt.Errorf("evolution.RecordMetric: tenant_id required in context")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_evolution_metrics
		 (id, tenant_id, agent_id, session_key, metric_type, metric_key, value)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		m.ID.String(), tenantID.String(), m.AgentID.String(),
		m.SessionKey, string(m.MetricType), m.MetricKey, string(m.Value))
	return err
}

func (s *SQLiteEvolutionMetricsStore) QueryMetrics(ctx context.Context, agentID uuid.UUID, metricType store.MetricType, since time.Time, limit int) ([]store.EvolutionMetric, error) {
	tenantID := tenantIDForInsert(ctx)
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, agent_id, session_key, metric_type, metric_key, value, created_at
		 FROM agent_evolution_metrics
		 WHERE agent_id = ? AND metric_type = ? AND created_at >= ? AND tenant_id = ?
		 ORDER BY created_at DESC LIMIT ?`,
		agentID.String(), string(metricType), since.UTC().Format(time.RFC3339Nano), tenantID.String(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metrics []store.EvolutionMetric
	for rows.Next() {
		var m store.EvolutionMetric
		var idStr, tenantStr, agentStr string
		var valueBytes []byte
		var createdAt sqliteTime
		if err := rows.Scan(&idStr, &tenantStr, &agentStr, &m.SessionKey,
			&m.MetricType, &m.MetricKey, &valueBytes, &createdAt); err != nil {
			return nil, err
		}
		m.ID, _ = uuid.Parse(idStr)
		m.TenantID, _ = uuid.Parse(tenantStr)
		m.AgentID, _ = uuid.Parse(agentStr)
		m.Value = valueBytes
		m.CreatedAt = createdAt.Time
		metrics = append(metrics, m)
	}
	return metrics, rows.Err()
}

func (s *SQLiteEvolutionMetricsStore) AggregateToolMetrics(ctx context.Context, agentID uuid.UUID, since time.Time) ([]store.ToolAggregate, error) {
	tenantID := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT metric_key,
		        COUNT(*) AS call_count,
		        AVG(CASE WHEN COALESCE(json_extract(value, '$.success'),'false') = 'true' THEN 1.0 ELSE 0.0 END) AS success_rate,
		        AVG(COALESCE(NULLIF(json_extract(value, '$.duration_ms'),''), 0)) AS avg_duration_ms
		 FROM agent_evolution_metrics
		 WHERE agent_id = ? AND metric_type = 'tool' AND created_at >= ? AND tenant_id = ?
		 GROUP BY metric_key
		 ORDER BY call_count DESC`,
		agentID.String(), since.UTC().Format(time.RFC3339Nano), tenantID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var aggs []store.ToolAggregate
	for rows.Next() {
		var a store.ToolAggregate
		if err := rows.Scan(&a.ToolName, &a.CallCount, &a.SuccessRate, &a.AvgDurationMs); err != nil {
			return nil, err
		}
		aggs = append(aggs, a)
	}
	return aggs, rows.Err()
}

func (s *SQLiteEvolutionMetricsStore) AggregateRetrievalMetrics(ctx context.Context, agentID uuid.UUID, since time.Time) ([]store.RetrievalAggregate, error) {
	tenantID := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT metric_key,
		        COUNT(*) AS query_count,
		        AVG(CASE WHEN COALESCE(json_extract(value, '$.used_in_reply'),'false') = 'true' THEN 1.0 ELSE 0.0 END) AS usage_rate,
		        AVG(COALESCE(NULLIF(json_extract(value, '$.top_score'),''), 0)) AS avg_score
		 FROM agent_evolution_metrics
		 WHERE agent_id = ? AND metric_type = 'retrieval' AND created_at >= ? AND tenant_id = ?
		 GROUP BY metric_key
		 ORDER BY query_count DESC`,
		agentID.String(), since.UTC().Format(time.RFC3339Nano), tenantID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var aggs []store.RetrievalAggregate
	for rows.Next() {
		var a store.RetrievalAggregate
		if err := rows.Scan(&a.Source, &a.QueryCount, &a.UsageRate, &a.AvgScore); err != nil {
			return nil, err
		}
		aggs = append(aggs, a)
	}
	return aggs, rows.Err()
}

func (s *SQLiteEvolutionMetricsStore) Cleanup(ctx context.Context, olderThan time.Time) (int64, error) {
	tenantID := tenantIDForInsert(ctx)
	var result sql.Result
	var err error
	if tenantID != uuid.Nil {
		result, err = s.db.ExecContext(ctx,
			`DELETE FROM agent_evolution_metrics WHERE created_at < ? AND tenant_id = ?`,
			olderThan.UTC().Format(time.RFC3339Nano), tenantID.String())
	} else {
		result, err = s.db.ExecContext(ctx,
			`DELETE FROM agent_evolution_metrics WHERE created_at < ?`,
			olderThan.UTC().Format(time.RFC3339Nano))
	}
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// Ensure SQLiteEvolutionMetricsStore implements store.EvolutionMetricsStore.
var _ store.EvolutionMetricsStore = (*SQLiteEvolutionMetricsStore)(nil)
