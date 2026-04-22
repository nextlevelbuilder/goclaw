package store

import "database/sql"

// Stores is the top-level container for all storage backends.
type Stores struct {
	DB        *sql.DB // underlying connection
	Sessions  SessionStore
	Memory    MemoryStore
	Cron      CronStore
	Pairing   PairingStore
	Skills    SkillStore
	Agents    AgentStore
	Providers ProviderStore
	Tracing   TracingStore
	MCP              MCPServerStore
	ChannelInstances ChannelInstanceStore
	ConfigSecrets    ConfigSecretsStore
	AgentLinks       AgentLinkStore
	Teams            TeamStore
	BuiltinTools     BuiltinToolStore
	PendingMessages  PendingMessageStore
	KnowledgeGraph   KnowledgeGraphStore
	Contacts         ContactStore
	Activity         ActivityStore
	Snapshots        SnapshotStore
	SecureCLI           SecureCLIStore
	SecureCLIGrants     SecureCLIAgentGrantStore
	APIKeys             APIKeyStore
	Heartbeats        HeartbeatStore
	ConfigPermissions      ConfigPermissionStore
	Tenants                TenantStore
	BuiltinToolTenantCfgs  BuiltinToolTenantConfigStore
	SkillTenantCfgs        SkillTenantConfigStore
	SystemConfigs          SystemConfigStore
	SubagentTasks          SubagentTaskStore
	Vault                  VaultStore
	Episodic               EpisodicStore
	EvolutionMetrics       EvolutionMetricsStore
	EvolutionSuggestions   EvolutionSuggestionStore
	// Hooks is hooks.HookStore — typed as any to avoid import cycle
	// (hooks package imports store for context helpers).
	// Callers: type-assert to hooks.HookStore before use.
	Hooks any
}
