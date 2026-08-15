//go:build sqliteonly

package cmd

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/sqlitestore"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// setupStoresAndTracing creates SQLite-only stores, tracing collector, and snapshot worker.
// Built with -tags sqliteonly: no PostgreSQL dependency compiled in.
func setupStoresAndTracing(
	cfg *config.Config,
	dataDir string,
	msgBus *bus.MessageBus,
) (*store.Stores, *tracing.Collector, *tracing.SnapshotWorker) {
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
	stores, err := sqlitestore.NewSQLiteStores(storeCfg)
	if err != nil {
		slog.Error("failed to create SQLite stores", "error", err, "path", sqlitePath)
		os.Exit(1)
	}

	// SQLite-only always defaults to Lite edition unless explicitly overridden.
	if os.Getenv("GOCLAW_EDITION") == "" {
		edition.SetCurrent(edition.Lite)
		slog.Info("edition: lite (auto, sqliteonly build)")
	}
	slog.Info("storage backend: sqlite (sqliteonly build)", "path", sqlitePath)

	traceCollector, snapshotWorker := wireTracingAndCron(cfg, stores, msgBus, dataDir)
	return stores, traceCollector, snapshotWorker
}
