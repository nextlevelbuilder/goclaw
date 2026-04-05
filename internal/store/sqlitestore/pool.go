//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log/slog"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// connectionPragmas are applied to EVERY new SQLite connection.
// Critical: PRAGMA settings are per-connection in SQLite. Using db.Exec()
// only applies to ONE connection in the pool — other connections won't have
// busy_timeout, causing immediate SQLITE_BUSY errors under concurrency.
var connectionPragmas = []string{
	"PRAGMA journal_mode = WAL",
	"PRAGMA busy_timeout = 15000",
	"PRAGMA synchronous = NORMAL",
	"PRAGMA cache_size = -8000", // 8MB cache
	"PRAGMA foreign_keys = ON",
}

// pragmaConnector wraps a sql.Driver to apply PRAGMAs on every new connection.
// This ensures ALL connections in the pool have busy_timeout, WAL mode, etc.
type pragmaConnector struct {
	driver  driver.Driver
	dsn     string
	pragmas []string
}

func (c *pragmaConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.driver.Open(c.dsn)
	if err != nil {
		return nil, err
	}

	// Apply PRAGMAs to this specific connection.
	for _, p := range c.pragmas {
		if execer, ok := conn.(driver.ExecerContext); ok {
			if _, execErr := execer.ExecContext(ctx, p, nil); execErr != nil {
				slog.Warn("sqlite pragma failed on new conn", "pragma", p, "error", execErr)
			}
		} else if execer, ok := conn.(driver.Execer); ok { //nolint:staticcheck
			if _, execErr := execer.Exec(p, nil); execErr != nil {
				slog.Warn("sqlite pragma failed on new conn", "pragma", p, "error", execErr)
			}
		}
	}
	return conn, nil
}

func (c *pragmaConnector) Driver() driver.Driver { return c.driver }

// OpenDB opens a SQLite database at the given path with WAL mode and recommended pragmas.
// Uses modernc.org/sqlite (pure Go, zero CGo).
//
// Desktop app concurrency model:
// - WAL mode: allows concurrent readers alongside a single writer
// - busy_timeout=15000: ALL connections wait up to 15s before SQLITE_BUSY
// - _txlock=immediate: write transactions acquire lock immediately (fail-fast on contention)
//
// PRAGMAs are applied per-connection via pragmaConnector, ensuring every
// connection in the pool has consistent settings (busy_timeout, WAL, etc.).
func OpenDB(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_txlock=immediate", path)

	// Get the registered driver to wrap with pragmaConnector.
	drv, err := getSQLiteDriver()
	if err != nil {
		return nil, err
	}

	db := sql.OpenDB(&pragmaConnector{
		driver:  drv,
		dsn:     dsn,
		pragmas: connectionPragmas,
	})

	// SQLite is single-writer; WAL allows concurrent readers.
	// 4 connections: up to 3 readers + 1 writer can proceed in parallel,
	// reducing connection pool starvation during concurrent operations.
	db.SetMaxOpenConns(4)

	// Verify connection works (also triggers first pragma application).
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	return db, nil
}

// getSQLiteDriver retrieves the registered "sqlite" driver instance.
func getSQLiteDriver() (driver.Driver, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("get sqlite driver: %w", err)
	}
	drv := db.Driver()
	db.Close()
	return drv, nil
}
