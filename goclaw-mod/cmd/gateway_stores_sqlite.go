//go:build sqlite && !sqliteonly

package cmd

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/internal/store/sqlitestore"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// setupStoresAndTracing creates stores (PG or SQLite based on config), tracing collector,
// snapshot worker, and wires cron config.
// Built with -tags sqlite: supports both backends, selected via GOCLAW_STORAGE_BACKEND env.
func setupStoresAndTracing(
	cfg *config.Config,
	dataDir string,
	msgBus *bus.MessageBus,
) (*store.Stores, *tracing.Collector, *tracing.SnapshotWorker) {
	backend := cfg.Database.StorageBackend
	if backend == "" {
		backend = "postgres"
	}

	var stores *store.Stores

	switch backend {
	case "sqlite":
		sqlitePath := cfg.Database.SQLitePath
		if sqlitePath == "" {
			sqlitePath = filepath.Join(dataDir, "goclaw.db")
		}
		storeCfg := store.StoreConfig{
			SQLitePath:       sqlitePath,
			StorageBackend:   "sqlite",
			EncryptionKey:    os.Getenv("GOCLAW_ENCRYPTION_KEY"),
			SkillsStorageDir: filepath.Join(dataDir, "skills-store"),
		}
		s, err := sqlitestore.NewSQLiteStores(storeCfg)
		if err != nil {
			slog.Error("failed to create SQLite stores", "error", err, "path", sqlitePath)
			os.Exit(1)
		}
		stores = s
		// SQLite backend auto-defaults to Lite edition unless explicitly overridden.
		if os.Getenv("GOCLAW_EDITION") == "" {
			edition.SetCurrent(edition.Lite)
			slog.Info("edition: lite (auto, sqlite backend)")
		}
		slog.Info("storage backend: sqlite", "path", sqlitePath)

	case "postgres":
		if cfg.Database.PostgresDSN == "" {
			slog.Error("GOCLAW_POSTGRES_DSN is required. Set it in your environment or .env.local file.")
			os.Exit(1)
		}
		if err := checkSchemaOrAutoUpgrade(cfg.Database.PostgresDSN); err != nil {
			slog.Error("schema compatibility check failed", "error", err)
			os.Exit(1)
		}
		storeCfg := store.StoreConfig{
			PostgresDSN:      cfg.Database.PostgresDSN,
			EncryptionKey:    os.Getenv("GOCLAW_ENCRYPTION_KEY"),
			SkillsStorageDir: filepath.Join(dataDir, "skills-store"),
		}
		s, err := pg.NewPGStores(storeCfg)
		if err != nil {
			slog.Error("failed to create PG stores", "error", err)
			os.Exit(1)
		}
		stores = s
		slog.Info("storage backend: postgres")

	default:
		slog.Error("unknown GOCLAW_STORAGE_BACKEND; expected 'postgres' or 'sqlite'", "value", backend)
		os.Exit(1)
	}

	traceCollector, snapshotWorker := wireTracingAndCron(cfg, stores, msgBus, dataDir)
	return stores, traceCollector, snapshotWorker
}
