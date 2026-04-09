//go:build !sqliteonly

package backup

import (
	"context"
	"database/sql"
	"log/slog"
)

// DiscoverTenantTables queries information_schema for all tables with a tenant_id column.
// Returns table names as a set. Used to detect unregistered tables at backup time.
func DiscoverTenantTables(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT table_name FROM information_schema.columns
		 WHERE column_name = 'tenant_id' AND table_schema = 'public'
		 ORDER BY table_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		result[name] = true
	}
	return result, rows.Err()
}

// ValidateTableRegistry cross-checks the hardcoded table registry against
// the actual database schema. Returns warnings for unregistered tables.
func ValidateTableRegistry(ctx context.Context, db *sql.DB) []string {
	discovered, err := DiscoverTenantTables(ctx, db)
	if err != nil {
		slog.Warn("backup.validate_registry", "error", err)
		return nil
	}

	// Build set of registered table names
	registered := make(map[string]bool)
	for _, t := range TenantTables() {
		registered[t.Name] = true
	}

	// Ephemeral tables intentionally excluded from backup
	skipped := map[string]bool{
		"traces": true, "spans": true, "usage_snapshots": true,
		"activity_logs": true, "embedding_cache": true,
		"pairing_requests": true, "paired_devices": true,
		"channel_pending_messages": true, "cron_run_logs": true,
		"team_user_grants": true,
	}

	var warnings []string
	for table := range discovered {
		if !registered[table] && !skipped[table] {
			warnings = append(warnings, "table "+table+" has tenant_id but is not in backup registry — data will be skipped")
		}
	}
	return warnings
}
