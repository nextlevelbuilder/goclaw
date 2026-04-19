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

	initSqlx(db)

	slog.Info("sqlite stores initialized", "path", cfg.SQLitePath)

	// F15: SecureCLI requires encryption key — skip if empty.
	var secureCLI store.SecureCLIStore
	if cfg.EncryptionKey != "" {
		secureCLI = NewSQLiteSecureCLIStore(db, cfg.EncryptionKey)
	} else {
		slog.Warn("securecli: encryption key empty, store disabled")
	}

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
		SubagentTasks:   NewSQLiteSubagentTaskStore(db),
		AgentLinks:      NewSQLiteAgentLinkStore(db),
		SecureCLI:            secureCLI,
		SecureCLIGrants:      NewSQLiteSecureCLIAgentGrantStore(db),
		Episodic:             NewSQLiteEpisodicStore(db),
		EvolutionMetrics:     NewSQLiteEvolutionMetricsStore(db),
		EvolutionSuggestions: NewSQLiteEvolutionSuggestionStore(db),
		KnowledgeGraph:       NewSQLiteKnowledgeGraphStore(db),
		Vault:                NewSQLiteVaultStore(db),
		Hooks:                NewSQLiteHookStore(db),
	}, nil
}
