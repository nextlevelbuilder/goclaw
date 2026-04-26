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

// --- Health Check History ---

func (s *SQLiteMCPServerStore) InsertHealthCheck(ctx context.Context, check *store.MCPHealthCheck) error {
	if check.ID == uuid.Nil {
		check.ID = uuid.New()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mcp_health_checks (id, server_id, server_name, tenant_id, status, latency_ms, error, tool_count, health_failures, checked_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		check.ID.String(), check.ServerID.String(), check.ServerName, check.TenantID.String(),
		check.Status, check.LatencyMs, check.Error, check.ToolCount, check.HealthFailures,
		check.CheckedAt)
	return err
}

func (s *SQLiteMCPServerStore) ListHealthChecks(ctx context.Context, serverID uuid.UUID, limit, offset int) ([]store.MCPHealthCheck, int, error) {
	var total int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM mcp_health_checks WHERE server_id = ?`, serverID.String()).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count health checks: %w", err)
	}
	if total == 0 {
		return nil, 0, nil
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, server_id, server_name, tenant_id, status, latency_ms, error, tool_count, health_failures, checked_at
		 FROM mcp_health_checks WHERE server_id = ? ORDER BY checked_at DESC LIMIT ? OFFSET ?`,
		serverID.String(), limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list health checks: %w", err)
	}
	defer rows.Close()

	var result []store.MCPHealthCheck
	for rows.Next() {
		var h store.MCPHealthCheck
		var idStr, serverIDStr, tenantIDStr string
		var latency sql.NullInt64
		var errStr sql.NullString
		if err := rows.Scan(&idStr, &serverIDStr, &h.ServerName, &tenantIDStr,
			&h.Status, &latency, &errStr, &h.ToolCount, &h.HealthFailures, &h.CheckedAt); err != nil {
			return nil, 0, fmt.Errorf("scan health check: %w", err)
		}
		h.ID, _ = uuid.Parse(idStr)
		h.ServerID, _ = uuid.Parse(serverIDStr)
		h.TenantID, _ = uuid.Parse(tenantIDStr)
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

func (s *SQLiteMCPServerStore) DeleteHealthChecksBefore(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM mcp_health_checks WHERE checked_at < ?`, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
