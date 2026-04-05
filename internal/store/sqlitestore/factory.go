//go:build sqlite || sqliteonly

package sqlitestore

import (
	"fmt"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// NewSQLiteStores creates all stores backed by SQLite.
// Mirrors pg.NewPGStores() — returns the same *store.Stores struct.
func NewSQLiteStores(cfg store.StoreConfig) (*store.Stores, error) {
	db, err := OpenDB(cfg.SQLitePath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Apply schema (create tables on first run, migrate on upgrade).
	if err := EnsureSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("ensure schema: %w", err)
	}

	slog.Info("sqlite stores initialized", "path", cfg.SQLitePath)

	return &store.Stores{
		DB:                    db,
		Sessions:              NewSQLiteSessionStore(db),
		Agents:                NewSQLiteAgentStore(db),
		Providers:             NewSQLiteProviderStore(db, cfg.EncryptionKey),
		Tracing:               NewSQLiteTracingStore(db),
		ConfigSecrets:         NewSQLiteConfigSecretsStore(db, cfg.EncryptionKey),
		BuiltinTools:          NewSQLiteBuiltinToolStore(db),
		Heartbeats:            NewSQLiteHeartbeatStore(db),
		Tenants:               NewSQLiteTenantStore(db),
		BuiltinToolTenantCfgs: NewSQLiteBuiltinToolTenantConfigStore(db),
		SkillTenantCfgs:       NewSQLiteSkillTenantConfigStore(db),
		SystemConfigs:         NewSQLiteSystemConfigStore(db),
		Snapshots:             NewSQLiteSnapshotStore(db),
		Cron:                  NewSQLiteCronStore(db),
		ChannelInstances:      NewSQLiteChannelInstanceStore(db, cfg.EncryptionKey),
		Pairing:               NewSQLitePairingStore(db),
		PendingMessages:       NewSQLitePendingMessageStore(db),
		Contacts:              NewSQLiteContactStore(db),
		Teams:  NewSQLiteTeamStore(db),
		Skills: NewSQLiteSkillStore(db, cfg.SkillsStorageDir),
		MCP:    NewSQLiteMCPServerStore(db, cfg.EncryptionKey),
		Activity:         NewSQLiteActivityStore(db),
		APIKeys:          NewSQLiteAPIKeyStore(db),
		ConfigPermissions: NewSQLiteConfigPermissionStore(db),
		Memory:         NewSQLiteMemoryStore(db),
		SubagentTasks:  NewSQLiteSubagentTaskStore(),
		// Phase 2 Batch B+C stores (nil = gracefully skipped by gateway):
		// AgentLinks, KnowledgeGraph, SecureCLI
	}, nil
}
