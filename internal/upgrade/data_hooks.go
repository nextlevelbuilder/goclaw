package upgrade

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// DataHookFunc is a Go function that runs after a specific schema version's
// SQL migration has been applied.
type DataHookFunc func(ctx context.Context, db *sql.DB) error

type dataHook struct {
	SchemaVersion uint
	Name          string
	Fn            DataHookFunc
}

var registry []dataHook

// RegisterDataHook registers a Go data migration hook for a specific schema version.
// Name must be unique across all hooks. Hooks for the same version run in
// registration order.
func RegisterDataHook(schemaVersion uint, name string, fn DataHookFunc) {
	registry = append(registry, dataHook{
		SchemaVersion: schemaVersion,
		Name:          name,
		Fn:            fn,
	})
}

// PendingHooks returns the names of data hooks that haven't been applied yet.
func PendingHooks(ctx context.Context, db *sql.DB) ([]string, error) {
	if err := ensureDataMigrationsTable(ctx, db); err != nil {
		return nil, err
	}

	applied, err := loadApplied(ctx, db)
	if err != nil {
		return nil, err
	}

	var pending []string
	for _, hook := range registry {
		if !applied[hook.Name] {
			pending = append(pending, hook.Name)
		}
	}
	return pending, nil
}

// RunPendingHooks executes all data hooks that haven't been applied yet.
// Each hook is tracked in the data_migrations table to ensure idempotency.
func RunPendingHooks(ctx context.Context, db *sql.DB) (int, error) {
	if err := ensureDataMigrationsTable(ctx, db); err != nil {
		return 0, fmt.Errorf("ensure data_migrations table: %w", err)
	}

	applied, err := loadApplied(ctx, db)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, hook := range registry {
		if applied[hook.Name] {
			continue
		}

		slog.Info("running data migration hook",
			"name", hook.Name,
			"schema_version", hook.SchemaVersion,
		)
		start := time.Now()

		if err := hook.Fn(ctx, db); err != nil {
			return count, fmt.Errorf("data hook %q failed: %w", hook.Name, err)
		}

		// Record completion.
		_, err := db.ExecContext(ctx,
			"INSERT INTO data_migrations (name, version, applied_at) VALUES ($1, $2, NOW())",
			hook.Name, hook.SchemaVersion,
		)
		if err != nil {
			return count, fmt.Errorf("record hook %q: %w", hook.Name, err)
		}

		slog.Info("data migration hook complete",
			"name", hook.Name,
			"duration", time.Since(start),
		)
		count++
	}

	return count, nil
}

func ensureDataMigrationsTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS data_migrations (
			name       VARCHAR(255) PRIMARY KEY,
			version    INT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	return err
}

func loadApplied(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "SELECT name FROM data_migrations")
	if err != nil {
		return nil, fmt.Errorf("query data_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		applied[name] = true
	}
	return applied, rows.Err()
}
