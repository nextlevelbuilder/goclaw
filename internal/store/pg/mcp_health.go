package pg

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// --- Health Check History ---

func (s *PGMCPServerStore) InsertHealthCheck(ctx context.Context, check *store.MCPHealthCheck) error {
	if check.ID == uuid.Nil {
		check.ID = uuid.New()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mcp_health_checks (id, server_id, server_name, tenant_id, status, latency_ms, error, tool_count, health_failures, checked_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		check.ID, check.ServerID, check.ServerName, check.TenantID,
		check.Status, check.LatencyMs, check.Error, check.ToolCount, check.HealthFailures,
		check.CheckedAt)
	return err
}

func (s *PGMCPServerStore) ListHealthChecks(ctx context.Context, serverID uuid.UUID, limit, offset int) ([]store.MCPHealthCheck, int, error) {
	var total int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM mcp_health_checks WHERE server_id = $1`, serverID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count health checks: %w", err)
	}
	if total == 0 {
		return nil, 0, nil
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, server_id, server_name, tenant_id, status, latency_ms, error, tool_count, health_failures, checked_at
		 FROM mcp_health_checks WHERE server_id = $1 ORDER BY checked_at DESC LIMIT $2 OFFSET $3`,
		serverID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list health checks: %w", err)
	}
	defer rows.Close()

	var result []store.MCPHealthCheck
	for rows.Next() {
		var h store.MCPHealthCheck
		var latency sql.NullInt64
		var errStr sql.NullString
		if err := rows.Scan(&h.ID, &h.ServerID, &h.ServerName, &h.TenantID,
			&h.Status, &latency, &errStr, &h.ToolCount, &h.HealthFailures, &h.CheckedAt); err != nil {
			return nil, 0, fmt.Errorf("scan health check: %w", err)
		}
		if latency.Valid {
			v := int(latency.Int64)
			h.LatencyMs = &v
		}
		if errStr.Valid {
			h.Error = errStr.String
		}
		result = append(result, h)
	}
	return result, total, rows.Err()
}

func (s *PGMCPServerStore) DeleteHealthChecksBefore(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM mcp_health_checks WHERE checked_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
