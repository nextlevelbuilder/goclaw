package base

import "regexp"

// ValidColumnName matches safe SQL identifiers (letters, digits, underscores).
// Defense-in-depth: prevents column name injection in BuildMapUpdate.
var ValidColumnName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// TablesWithUpdatedAt lists tables that have an updated_at column.
// Used by BuildMapUpdate to auto-set updated_at on writes.
var TablesWithUpdatedAt = map[string]bool{
	"agents": true, "llm_providers": true, "sessions": true,
	"channel_instances": true, "cron_jobs": true,
	"skills": true, "mcp_servers": true, "agent_links": true,
	"agent_teams": true, "team_tasks": true, "builtin_tools": true,
	"agent_context_files": true, "user_context_files": true,
	"user_agent_overrides": true, "config_secrets": true,
	"memory_documents": true, "memory_chunks": true, "embedding_cache": true,
	"vault_documents":     true,
	"secure_cli_binaries": true, "tenants": true,
}

// TableHasUpdatedAt returns true if the table has an updated_at column.
func TableHasUpdatedAt(table string) bool {
	return TablesWithUpdatedAt[table]
}
