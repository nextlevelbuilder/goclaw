package pg

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// PoolApplicationName tags every gateway pool connection via the PostgreSQL
// application_name runtime parameter. Pre-restore safety checks use it to tell
// the gateway's own connections apart from external clients (issue #1338).
const PoolApplicationName = "goclaw"

// Connection pool sizing, overridable for deployments that raise LaneMain:
//
//	GOCLAW_PG_MAX_OPEN_CONNS=25
//	GOCLAW_PG_MAX_IDLE_CONNS=10
//
// A connection is held per query, not per agent run, so the pool does not cap
// concurrency directly — it caps how many queries can be in flight. The cost of
// undersizing shows up as latency at burst, when many runs start together and
// each loads context and history at the same moment, which is why the ceiling
// wants to move with LaneMain rather than stay fixed below it.
//
// Postgres' own max_connections is the real limit: keep the sum across all
// gateway processes under it.
const (
	defaultMaxOpenConns = 25
	defaultMaxIdleConns = 10
)

// poolEnv reads a positive int from an env var, falling back to defaultVal.
func poolEnv(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultVal
}

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

	maxOpen := poolEnv("GOCLAW_PG_MAX_OPEN_CONNS", defaultMaxOpenConns)
	maxIdle := poolEnv("GOCLAW_PG_MAX_IDLE_CONNS", defaultMaxIdleConns)
	if maxIdle > maxOpen {
		maxIdle = maxOpen
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	slog.Info("postgres connected", "dsn_len", len(dsn), "max_open", maxOpen, "max_idle", maxIdle)
	return db, nil
}
