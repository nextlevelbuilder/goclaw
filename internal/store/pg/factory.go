package pg

import (
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// NewPGStores creates all stores backed by Postgres.
func NewPGStores(cfg store.StoreConfig) (*store.Stores, error) {
	db, err := OpenDB(cfg.PostgresDSN)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	initSqlx(db)

	memCfg := DefaultPGMemoryConfig()

	skillsDir := cfg.SkillsStorageDir
	if skillsDir == "" {
		skillsDir = config.ResolvedDataDirFromEnv() + "/skills-store"
	}

	return &store.Stores{
		DB:        db,
		Sessions:  NewPGSessionStore(db),
		Memory:    NewPGMemoryStore(db, memCfg),
		Cron:      NewPGCronStore(db),
		Pairing:   NewPGPairingStore(db),
		Skills:    NewPGSkillStore(db, skillsDir),
		Agents:    NewPGAgentStore(db),
		Providers: NewPGProviderStore(db, cfg.EncryptionKey),
		Tracing:   NewPGTracingStore(db),
		MCP:              NewPGMCPServerStore(db, cfg.EncryptionKey),
		ChannelInstances: NewPGChannelInstanceStore(db, cfg.EncryptionKey),
		ConfigSecrets:    NewPGConfigSecretsStore(db, cfg.EncryptionKey),
		AgentLinks:       NewPGAgentLinkStore(db),
		Teams:            NewPGTeamStore(db),
		BuiltinTools:     NewPGBuiltinToolStore(db),
		PendingMessages:  NewPGPendingMessageStore(db),
		KnowledgeGraph:   NewPGKnowledgeGraphStore(db),
		Contacts:         NewPGContactStore(db),
		Activity:         NewPGActivityStore(db),
		Snapshots:        NewPGSnapshotStore(db),
		SecureCLI:           NewPGSecureCLIStore(db, cfg.EncryptionKey),
		SecureCLIGrants:     NewPGSecureCLIAgentGrantStore(db),
		APIKeys:             NewPGAPIKeyStore(db),
		Heartbeats:        NewPGHeartbeatStore(db),
		ConfigPermissions:     NewPGConfigPermissionStore(db),
		Tenants:               NewPGTenantStore(db),
		BuiltinToolTenantCfgs: NewPGBuiltinToolTenantConfigStore(db),
		SkillTenantCfgs:       NewPGSkillTenantConfigStore(db),
		SystemConfigs:         NewPGSystemConfigStore(db),
		SubagentTasks:         NewPGSubagentTaskStore(db),
		Vault:                 NewPGVaultStore(db),
		Episodic:              NewPGEpisodicStore(db),
		EvolutionMetrics:      NewPGEvolutionMetricsStore(db),
		EvolutionSuggestions:  NewPGEvolutionSuggestionStore(db),
	}, nil
}
