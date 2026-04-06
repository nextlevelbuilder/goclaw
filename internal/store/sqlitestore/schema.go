//go:build sqlite || sqliteonly

package sqlitestore

import (
	"database/sql"
	_ "embed"
	"fmt"
	"log/slog"
)

//go:embed schema.sql
var schemaSQL string

// SchemaVersion is the current SQLite schema version.
// Bump this when adding new migration steps below.
const SchemaVersion = 7

// migrations maps version → SQL to apply when upgrading FROM that version.
// schema.sql always represents the LATEST full schema (for fresh DBs).
// Existing DBs are patched incrementally via these steps.
//
// Example: to add a new column in the future:
//
//	var migrations = map[int]string{
//	    1: `ALTER TABLE agents ADD COLUMN new_col TEXT DEFAULT '';`,
//	}
//
// Then bump SchemaVersion to 2.
var migrations = map[int]string{
	// Version 1 → 2: add contact_type column to channel_contacts.
	1: `ALTER TABLE channel_contacts ADD COLUMN contact_type VARCHAR(20) NOT NULL DEFAULT 'user';`,
	// Version 2 → 3: promote cron payload fields to dedicated columns + add stateless flag.
	2: `ALTER TABLE cron_jobs ADD COLUMN stateless INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cron_jobs ADD COLUMN deliver INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cron_jobs ADD COLUMN deliver_channel TEXT NOT NULL DEFAULT '';
ALTER TABLE cron_jobs ADD COLUMN deliver_to TEXT NOT NULL DEFAULT '';
ALTER TABLE cron_jobs ADD COLUMN wake_heartbeat INTEGER NOT NULL DEFAULT 0;
UPDATE cron_jobs SET
  deliver = COALESCE(json_extract(payload, '$.deliver'), 0),
  deliver_channel = COALESCE(json_extract(payload, '$.channel'), ''),
  deliver_to = COALESCE(json_extract(payload, '$.to'), ''),
  wake_heartbeat = COALESCE(json_extract(payload, '$.wake_heartbeat'), 0)
WHERE payload IS NOT NULL;`,
	// Version 4 → 5: add thread_id, thread_type columns to channel_contacts for forum topic support.
	4: `ALTER TABLE channel_contacts ADD COLUMN thread_id VARCHAR(100);
ALTER TABLE channel_contacts ADD COLUMN thread_type VARCHAR(20);
DROP INDEX IF EXISTS idx_channel_contacts_tenant_type_sender;
CREATE UNIQUE INDEX idx_channel_contacts_tenant_type_sender
  ON channel_contacts(tenant_id, channel_type, sender_id, COALESCE(thread_id, ''));`,
	// Version 3 → 4: add subagent_tasks table for subagent lifecycle persistence.
	3: `CREATE TABLE IF NOT EXISTS subagent_tasks (
    id                TEXT PRIMARY KEY,
    tenant_id         TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    parent_agent_key  VARCHAR(255) NOT NULL,
    session_key       VARCHAR(500),
    subject           VARCHAR(255) NOT NULL,
    description       TEXT NOT NULL,
    status            VARCHAR(20) NOT NULL DEFAULT 'running',
    result            TEXT,
    depth             INTEGER NOT NULL DEFAULT 1,
    model             VARCHAR(255),
    provider          VARCHAR(255),
    iterations        INTEGER NOT NULL DEFAULT 0,
    input_tokens      INTEGER NOT NULL DEFAULT 0,
    output_tokens     INTEGER NOT NULL DEFAULT 0,
    origin_channel    VARCHAR(50),
    origin_chat_id    VARCHAR(255),
    origin_peer_kind  VARCHAR(20),
    origin_user_id    VARCHAR(255),
    spawned_by        TEXT,
    completed_at      TEXT,
    archived_at       TEXT,
    metadata          TEXT NOT NULL DEFAULT '{}',
    created_at        TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at        TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_subagent_tasks_parent_status ON subagent_tasks(tenant_id, parent_agent_key, status);
CREATE INDEX IF NOT EXISTS idx_subagent_tasks_session ON subagent_tasks(session_key);
CREATE INDEX IF NOT EXISTS idx_subagent_tasks_created ON subagent_tasks(tenant_id, created_at);`,
	// Version 5 → 6: secure CLI agent grants — replace agent_id with is_global + grants table.
	5: `ALTER TABLE secure_cli_binaries ADD COLUMN is_global BOOLEAN NOT NULL DEFAULT 1;
DROP INDEX IF EXISTS idx_secure_cli_unique_binary_agent;
DROP INDEX IF EXISTS idx_secure_cli_agent_id;
CREATE UNIQUE INDEX IF NOT EXISTS idx_secure_cli_unique_binary_tenant ON secure_cli_binaries(binary_name, tenant_id);
CREATE TABLE IF NOT EXISTS secure_cli_agent_grants (
    id              TEXT NOT NULL PRIMARY KEY,
    binary_id       TEXT NOT NULL REFERENCES secure_cli_binaries(id) ON DELETE CASCADE,
    agent_id        TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    deny_args       TEXT,
    deny_verbose    TEXT,
    timeout_seconds INTEGER,
    tips            TEXT,
    enabled         BOOLEAN NOT NULL DEFAULT 1,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id),
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(binary_id, agent_id, tenant_id)
);
CREATE INDEX IF NOT EXISTS idx_scag_binary ON secure_cli_agent_grants(binary_id);
CREATE INDEX IF NOT EXISTS idx_scag_agent ON secure_cli_agent_grants(agent_id);
CREATE INDEX IF NOT EXISTS idx_scag_tenant ON secure_cli_agent_grants(tenant_id);`,
	// Version 6 → 7: V3 tables (episodic, evolution, KG temporal) + promote other_config fields.
	6: `-- V3: episodic summaries
CREATE TABLE IF NOT EXISTS episodic_summaries (
    id          TEXT NOT NULL PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id     VARCHAR(255) NOT NULL DEFAULT '',
    session_key TEXT NOT NULL,
    summary     TEXT NOT NULL,
    l0_abstract TEXT NOT NULL DEFAULT '',
    key_topics  TEXT NOT NULL DEFAULT '[]',
    source_type TEXT NOT NULL DEFAULT 'session',
    source_id   TEXT,
    turn_count  INTEGER NOT NULL DEFAULT 0,
    token_count INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    expires_at  TEXT
);
CREATE INDEX IF NOT EXISTS idx_episodic_agent_user ON episodic_summaries(agent_id, user_id);
CREATE INDEX IF NOT EXISTS idx_episodic_tenant ON episodic_summaries(tenant_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_episodic_source_dedup ON episodic_summaries(agent_id, user_id, source_id)
    WHERE source_id IS NOT NULL;

-- V3: evolution metrics
CREATE TABLE IF NOT EXISTS agent_evolution_metrics (
    id          TEXT NOT NULL PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    session_key TEXT NOT NULL,
    metric_type TEXT NOT NULL,
    metric_key  TEXT NOT NULL,
    value       TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_evo_metrics_agent_type ON agent_evolution_metrics(agent_id, metric_type);
CREATE INDEX IF NOT EXISTS idx_evo_metrics_created ON agent_evolution_metrics(created_at);
CREATE INDEX IF NOT EXISTS idx_evo_metrics_tenant ON agent_evolution_metrics(tenant_id);

-- V3: evolution suggestions
CREATE TABLE IF NOT EXISTS agent_evolution_suggestions (
    id              TEXT NOT NULL PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id),
    agent_id        TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    suggestion_type TEXT NOT NULL,
    suggestion      TEXT NOT NULL,
    rationale       TEXT NOT NULL,
    parameters      TEXT,
    status          TEXT NOT NULL DEFAULT 'pending',
    reviewed_by     TEXT,
    reviewed_at     TEXT,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_evo_suggestions_agent ON agent_evolution_suggestions(agent_id, status);
CREATE INDEX IF NOT EXISTS idx_evo_suggestions_tenant ON agent_evolution_suggestions(tenant_id);

-- V3: KG temporal validity
ALTER TABLE kg_entities ADD COLUMN valid_from TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));
ALTER TABLE kg_entities ADD COLUMN valid_until TEXT;
CREATE INDEX IF NOT EXISTS idx_kg_entities_current ON kg_entities(agent_id, user_id) WHERE valid_until IS NULL;

ALTER TABLE kg_relations ADD COLUMN valid_from TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));
ALTER TABLE kg_relations ADD COLUMN valid_until TEXT;
CREATE INDEX IF NOT EXISTS idx_kg_relations_current ON kg_relations(agent_id, user_id) WHERE valid_until IS NULL;

-- Promote other_config fields to dedicated columns
ALTER TABLE agents ADD COLUMN emoji TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN agent_description TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN thinking_level TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN max_tokens INT NOT NULL DEFAULT 0;
ALTER TABLE agents ADD COLUMN self_evolve BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE agents ADD COLUMN skill_evolve BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE agents ADD COLUMN skill_nudge_interval INT NOT NULL DEFAULT 0;
ALTER TABLE agents ADD COLUMN reasoning_config TEXT NOT NULL DEFAULT '{}';
ALTER TABLE agents ADD COLUMN workspace_sharing TEXT NOT NULL DEFAULT '{}';
ALTER TABLE agents ADD COLUMN chatgpt_oauth_routing TEXT NOT NULL DEFAULT '{}';
ALTER TABLE agents ADD COLUMN shell_deny_groups TEXT NOT NULL DEFAULT '{}';
ALTER TABLE agents ADD COLUMN kg_dedup_config TEXT NOT NULL DEFAULT '{}';
UPDATE agents SET
  emoji = COALESCE(json_extract(other_config, '$.emoji'), ''),
  agent_description = COALESCE(json_extract(other_config, '$.description'), ''),
  thinking_level = COALESCE(json_extract(other_config, '$.thinking_level'), ''),
  max_tokens = COALESCE(json_extract(other_config, '$.max_tokens'), 0),
  self_evolve = COALESCE(json_extract(other_config, '$.self_evolve'), 0),
  skill_evolve = COALESCE(json_extract(other_config, '$.skill_evolve'), 0),
  skill_nudge_interval = COALESCE(json_extract(other_config, '$.skill_nudge_interval'), 0),
  reasoning_config = COALESCE(json_extract(other_config, '$.reasoning'), '{}'),
  workspace_sharing = COALESCE(json_extract(other_config, '$.workspace_sharing'), '{}'),
  chatgpt_oauth_routing = COALESCE(json_extract(other_config, '$.chatgpt_oauth_routing'), '{}'),
  shell_deny_groups = COALESCE(json_extract(other_config, '$.shell_deny_groups'), '{}'),
  kg_dedup_config = COALESCE(json_extract(other_config, '$.kg_dedup_config'), '{}')
WHERE other_config != '{}' AND other_config IS NOT NULL;
UPDATE agents SET other_config = json_remove(other_config,
  '$.emoji', '$.description', '$.thinking_level', '$.max_tokens',
  '$.self_evolve', '$.skill_evolve', '$.skill_nudge_interval',
  '$.reasoning', '$.workspace_sharing', '$.chatgpt_oauth_routing',
  '$.shell_deny_groups', '$.kg_dedup_config');`,
}

// EnsureSchema creates tables if they don't exist and applies incremental migrations.
//
// Flow:
//  1. Fresh DB (no schema_version row) → apply full schema.sql + set version = SchemaVersion
//  2. Existing DB with version < SchemaVersion → apply patches sequentially
//  3. Existing DB with version == SchemaVersion → no-op
//  4. Always: seed master tenant (idempotent)
func EnsureSchema(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER NOT NULL PRIMARY KEY
	)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	var current int
	err := db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&current)
	if err == sql.ErrNoRows {
		// Fresh database — apply full schema.
		slog.Info("sqlite: applying initial schema", "version", SchemaVersion)
		tx, txErr := db.Begin()
		if txErr != nil {
			return fmt.Errorf("begin schema tx: %w", txErr)
		}
		if _, err := tx.Exec(schemaSQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply schema: %w", err)
		}
		if _, err := tx.Exec("INSERT INTO schema_version (version) VALUES (?)", SchemaVersion); err != nil {
			tx.Rollback()
			return fmt.Errorf("set schema version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit schema tx: %w", err)
		}
		return seedMasterTenant(db)
	}
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	// Apply incremental migrations for existing DBs.
	if current < SchemaVersion {
		slog.Info("sqlite: migrating schema", "from", current, "to", SchemaVersion)
		for v := current; v < SchemaVersion; v++ {
			patch, ok := migrations[v]
			if !ok {
				return fmt.Errorf("sqlite: missing migration for version %d → %d", v, v+1)
			}
			tx, txErr := db.Begin()
			if txErr != nil {
				return fmt.Errorf("begin migration tx v%d: %w", v, txErr)
			}
			if _, err := tx.Exec(patch); err != nil {
				tx.Rollback()
				return fmt.Errorf("apply migration v%d: %w", v, err)
			}
			if _, err := tx.Exec(
				"UPDATE schema_version SET version = ? WHERE version = ?", v+1, v,
			); err != nil {
				tx.Rollback()
				return fmt.Errorf("update schema version v%d: %w", v, err)
			}
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit migration v%d: %w", v, err)
			}
			slog.Info("sqlite: applied migration", "version", v+1)
		}
	}

	return seedMasterTenant(db)
}

// seedMasterTenant ensures the master tenant row exists (idempotent).
func seedMasterTenant(db *sql.DB) error {
	_, err := db.Exec(
		`INSERT OR IGNORE INTO tenants (id, name, slug, status) VALUES (?, 'Master', 'master', 'active')`,
		"0193a5b0-7000-7000-8000-000000000001",
	)
	if err != nil {
		slog.Warn("sqlite: seed master tenant failed", "error", err)
	}
	return nil
}
