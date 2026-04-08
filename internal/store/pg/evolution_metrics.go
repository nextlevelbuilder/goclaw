package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGEvolutionMetricsStore implements store.EvolutionMetricsStore backed by PostgreSQL.
type PGEvolutionMetricsStore struct {
	db *sql.DB
}

// NewPGEvolutionMetricsStore creates a new PG-backed evolution metrics store.
func NewPGEvolutionMetricsStore(db *sql.DB) *PGEvolutionMetricsStore {
	return &PGEvolutionMetricsStore{db: db}
}

func (s *PGEvolutionMetricsStore) RecordMetric(ctx context.Context, m store.EvolutionMetric) error {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		return fmt.Errorf("evolution.RecordMetric: tenant_id required in context")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_evolution_metrics (id, tenant_id, agent_id, session_key, metric_type, metric_key, value)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		m.ID, tenantID, m.AgentID, m.SessionKey, m.MetricType, m.MetricKey, m.Value)
	return err
}

func (s *PGEvolutionMetricsStore) QueryMetrics(ctx context.Context, agentID uuid.UUID, metricType store.MetricType, since time.Time, limit int) ([]store.EvolutionMetric, error) {
	tenantID := store.TenantIDFromContext(ctx)
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, agent_id, session_key, metric_type, metric_key, value, created_at
		 FROM agent_evolution_metrics
		 WHERE agent_id = $1 AND metric_type = $2 AND created_at >= $3 AND tenant_id = $4
		 ORDER BY created_at DESC LIMIT $5`,
		agentID, metricType, since, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metrics []store.EvolutionMetric
	for rows.Next() {
		var m store.EvolutionMetric
		if err := rows.Scan(&m.ID, &m.TenantID, &m.AgentID, &m.SessionKey, &m.MetricType, &m.MetricKey, &m.Value, &m.CreatedAt); err != nil {
			return nil, err
		}
		metrics = append(metrics, m)
	}
	return metrics, rows.Err()
}

func (s *PGEvolutionMetricsStore) AggregateToolMetrics(ctx context.Context, agentID uuid.UUID, since time.Time) ([]store.ToolAggregate, error) {
	tenantID := store.TenantIDFromContext(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT metric_key,
		        COUNT(*) AS call_count,
		        AVG(CASE WHEN COALESCE(value->>'success','false') = 'true' THEN 1.0 ELSE 0.0 END) AS success_rate,
		        AVG(COALESCE(NULLIF(value->>'duration_ms','')::numeric, 0)) AS avg_duration_ms
		 FROM agent_evolution_metrics
		 WHERE agent_id = $1 AND metric_type = 'tool' AND created_at >= $2 AND tenant_id = $3
		 GROUP BY metric_key
		 ORDER BY call_count DESC`,
		agentID, since, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var aggs []store.ToolAggregate
	for rows.Next() {
		var a store.ToolAggregate
		var avgMs float64
		if err := rows.Scan(&a.ToolName, &a.CallCount, &a.SuccessRate, &avgMs); err != nil {
			return nil, err
		}
		a.AvgDurationMs = avgMs
		aggs = append(aggs, a)
	}
	return aggs, rows.Err()
}

func (s *PGEvolutionMetricsStore) AggregateRetrievalMetrics(ctx context.Context, agentID uuid.UUID, since time.Time) ([]store.RetrievalAggregate, error) {
	tenantID := store.TenantIDFromContext(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT metric_key,
		        COUNT(*) AS query_count,
		        AVG(CASE WHEN COALESCE(value->>'used_in_reply','false') = 'true' THEN 1.0 ELSE 0.0 END) AS usage_rate,
		        AVG(COALESCE(NULLIF(value->>'top_score','')::numeric, 0)) AS avg_score
		 FROM agent_evolution_metrics
		 WHERE agent_id = $1 AND metric_type = 'retrieval' AND created_at >= $2 AND tenant_id = $3
		 GROUP BY metric_key
		 ORDER BY query_count DESC`,
		agentID, since, tenantID)
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

func (s *PGEvolutionMetricsStore) Cleanup(ctx context.Context, olderThan time.Time) (int64, error) {
	tenantID := store.TenantIDFromContext(ctx)
	var result sql.Result
	var err error
	if tenantID != uuid.Nil {
		result, err = s.db.ExecContext(ctx,
			`DELETE FROM agent_evolution_metrics WHERE created_at < $1 AND tenant_id = $2`,
			olderThan, tenantID)
	} else {
		result, err = s.db.ExecContext(ctx,
			`DELETE FROM agent_evolution_metrics WHERE created_at < $1`,
			olderThan)
	}
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// RecordToolMetric is a convenience helper for recording tool execution metrics.
func RecordToolMetric(ctx context.Context, s store.EvolutionMetricsStore, agentID uuid.UUID, sessionKey, toolName string, success bool, durationMs int64) {
	value, _ := json.Marshal(map[string]any{
		"success":     success,
		"duration_ms": durationMs,
	})
	_ = s.RecordMetric(ctx, store.EvolutionMetric{
		ID:         uuid.New(),
		TenantID:   store.TenantIDFromContext(ctx),
		AgentID:    agentID,
		SessionKey: sessionKey,
		MetricType: store.MetricTool,
		MetricKey:  toolName,
		Value:      value,
	})
}
