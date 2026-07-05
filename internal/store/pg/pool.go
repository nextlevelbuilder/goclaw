package pg

import (
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// PoolApplicationName tags every gateway pool connection via the PostgreSQL
// application_name runtime parameter. Pre-restore safety checks use it to tell
// the gateway's own connections apart from external clients (issue #1338).
const PoolApplicationName = "goclaw"

// OpenDB creates a database/sql connection to Postgres using pgx driver.
func OpenDB(dsn string) (*sql.DB, error) {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}
	if config.RuntimeParams == nil {
		config.RuntimeParams = map[string]string{}
	}
	// Respect a caller-supplied application_name; otherwise tag as the gateway.
	if config.RuntimeParams["application_name"] == "" {
		config.RuntimeParams["application_name"] = PoolApplicationName
	}
	db := stdlib.OpenDB(*config)

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	slog.Info("postgres connected", "dsn_len", len(dsn))
	return db, nil
}
