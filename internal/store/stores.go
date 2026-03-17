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
	CustomTools      CustomToolStore
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
	SecureCLI        SecureCLIStore
	APIKeys          APIKeyStore
}
