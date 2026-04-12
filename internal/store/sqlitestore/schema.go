//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
)

//go:embed schema.sql
var schemaSQL string

// SchemaVersion is the current SQLite schema version.
// Bump this when adding new migration steps below.
const SchemaVersion = 17

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

	// Version 7 → 8: add promoted_at to episodic_summaries for dreaming pipeline.
	7: `ALTER TABLE episodic_summaries ADD COLUMN promoted_at TEXT;
CREATE INDEX IF NOT EXISTS idx_episodic_unpromoted ON episodic_summaries(agent_id, user_id, created_at)
    WHERE promoted_at IS NULL;`,

	// Version 8 → 9: add kg_dedup_candidates, secure_cli_user_credentials, vault_documents, vault_links.
	8: `CREATE TABLE IF NOT EXISTS kg_dedup_candidates (
    id          TEXT NOT NULL PRIMARY KEY,
    tenant_id   TEXT REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id     VARCHAR(255) NOT NULL DEFAULT '',
    entity_a_id TEXT NOT NULL REFERENCES kg_entities(id) ON DELETE CASCADE,
    entity_b_id TEXT NOT NULL REFERENCES kg_entities(id) ON DELETE CASCADE,
    similarity  REAL NOT NULL,
    status      VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(entity_a_id, entity_b_id)
);
CREATE INDEX IF NOT EXISTS idx_kg_dedup_agent ON kg_dedup_candidates(agent_id, status);

CREATE TABLE IF NOT EXISTS secure_cli_user_credentials (
    id            TEXT NOT NULL PRIMARY KEY,
    binary_id     TEXT NOT NULL REFERENCES secure_cli_binaries(id) ON DELETE CASCADE,
    user_id       VARCHAR(255) NOT NULL,
    encrypted_env BLOB NOT NULL,
    metadata      TEXT NOT NULL DEFAULT '{}',
    tenant_id     TEXT NOT NULL REFERENCES tenants(id),
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(binary_id, user_id, tenant_id)
);
CREATE INDEX IF NOT EXISTS idx_scuc_tenant ON secure_cli_user_credentials(tenant_id);
CREATE INDEX IF NOT EXISTS idx_scuc_binary ON secure_cli_user_credentials(binary_id);

CREATE TABLE IF NOT EXISTS vault_documents (
    id           TEXT NOT NULL PRIMARY KEY,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id     TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    scope        TEXT NOT NULL DEFAULT 'personal',
    path         TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    doc_type     TEXT NOT NULL DEFAULT 'note',
    content_hash TEXT NOT NULL DEFAULT '',
    summary      TEXT NOT NULL DEFAULT '',
    metadata     TEXT DEFAULT '{}',
    created_at   TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at   TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(agent_id, scope, path)
);
CREATE INDEX IF NOT EXISTS idx_vault_docs_tenant ON vault_documents(tenant_id);
CREATE INDEX IF NOT EXISTS idx_vault_docs_agent_scope ON vault_documents(agent_id, scope);
CREATE INDEX IF NOT EXISTS idx_vault_docs_type ON vault_documents(agent_id, doc_type);
CREATE INDEX IF NOT EXISTS idx_vault_docs_hash ON vault_documents(content_hash);

CREATE TABLE IF NOT EXISTS vault_links (
    id          TEXT NOT NULL PRIMARY KEY,
    from_doc_id TEXT NOT NULL REFERENCES vault_documents(id) ON DELETE CASCADE,
    to_doc_id   TEXT NOT NULL REFERENCES vault_documents(id) ON DELETE CASCADE,
    link_type   TEXT NOT NULL DEFAULT 'wikilink',
    context     TEXT NOT NULL DEFAULT '',
    created_at  TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(from_doc_id, to_doc_id, link_type)
);
CREATE INDEX IF NOT EXISTS idx_vault_links_from ON vault_links(from_doc_id);
CREATE INDEX IF NOT EXISTS idx_vault_links_to ON vault_links(to_doc_id);`,

	// Version 9 → 10: originally added summary column to vault_documents.
	// Now a no-op: migration 8 already creates vault_documents WITH summary.
	// DBs from schema.sql also include summary. ALTER would fail with "duplicate column".
	9: `SELECT 1;`,

	// Version 10 → 11: add team_id + custom_scope to vault_documents (fix cross-team UNIQUE),
	// add custom_scope to 8 other tables (vault_versions absent in SQLite).
	10: `-- Recreate vault_documents with team_id + custom_scope columns.
-- SQLite prohibits expressions (COALESCE) in UNIQUE constraints,
-- so we use a unique INDEX instead of inline UNIQUE.
CREATE TABLE vault_documents_new (
    id           TEXT NOT NULL PRIMARY KEY,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id     TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    team_id      TEXT REFERENCES agent_teams(id) ON DELETE SET NULL,
    scope        TEXT NOT NULL DEFAULT 'personal',
    custom_scope TEXT,
    path         TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    doc_type     TEXT NOT NULL DEFAULT 'note',
    content_hash TEXT NOT NULL DEFAULT '',
    summary      TEXT NOT NULL DEFAULT '',
    metadata     TEXT DEFAULT '{}',
    created_at   TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at   TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
INSERT INTO vault_documents_new (id, tenant_id, agent_id, team_id, scope, custom_scope, path, title, doc_type, content_hash, summary, metadata, created_at, updated_at)
    SELECT id, tenant_id, agent_id, NULL, scope, NULL, path, title, doc_type, content_hash, summary, metadata, created_at, updated_at
    FROM vault_documents;
DROP TABLE vault_documents;
ALTER TABLE vault_documents_new RENAME TO vault_documents;
CREATE UNIQUE INDEX IF NOT EXISTS idx_vault_docs_unique_path
    ON vault_documents(agent_id, COALESCE(team_id, ''), scope, path);
CREATE INDEX IF NOT EXISTS idx_vault_docs_tenant ON vault_documents(tenant_id);
CREATE INDEX IF NOT EXISTS idx_vault_docs_agent_scope ON vault_documents(agent_id, scope);
CREATE INDEX IF NOT EXISTS idx_vault_docs_type ON vault_documents(agent_id, doc_type);
CREATE INDEX IF NOT EXISTS idx_vault_docs_hash ON vault_documents(content_hash);
CREATE INDEX IF NOT EXISTS idx_vault_docs_team ON vault_documents(team_id);
-- custom_scope on other tables (vault_versions absent in SQLite).
ALTER TABLE vault_links ADD COLUMN custom_scope TEXT;
ALTER TABLE memory_documents ADD COLUMN custom_scope TEXT;
ALTER TABLE memory_chunks ADD COLUMN custom_scope TEXT;
ALTER TABLE team_tasks ADD COLUMN custom_scope TEXT;
ALTER TABLE team_task_attachments ADD COLUMN custom_scope TEXT;
ALTER TABLE team_task_comments ADD COLUMN custom_scope TEXT;
ALTER TABLE team_task_events ADD COLUMN custom_scope TEXT;
ALTER TABLE subagent_tasks ADD COLUMN custom_scope TEXT;`,
	// Version 11 → 12: seed AGENTS_CORE.md + AGENTS_TASK.md, remove AGENTS_MINIMAL.md.
	11: `INSERT INTO agent_context_files (id, agent_id, file_name, content, tenant_id, created_at, updated_at)
SELECT lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)),2) || '-' || substr('89ab', abs(random()) % 4 + 1, 1) || substr(hex(randomblob(2)),2) || '-' || hex(randomblob(6))),
  a.id, 'AGENTS_CORE.md',
  '# Operating Rules (Core)

## Language & Communication

- Match the user''s language. Detect from first message, stay consistent.

## Internal Messages

- [System Message] blocks are internal context. Not user-visible.
- Rewrite system messages in your normal voice before delivering.
- Never use exec or curl for messaging.
- When asked to save or remember, MUST call write_file or edit in THIS turn.
',
  a.tenant_id, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM agents a
WHERE a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM agent_context_files WHERE agent_id = a.id AND file_name = 'AGENTS_CORE.md');

INSERT INTO agent_context_files (id, agent_id, file_name, content, tenant_id, created_at, updated_at)
SELECT lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)),2) || '-' || substr('89ab', abs(random()) % 4 + 1, 1) || substr(hex(randomblob(2)),2) || '-' || hex(randomblob(6))),
  a.id, 'AGENTS_TASK.md',
  '# Operating Rules (Task)

## Language & Communication

- Match the user''s language. Detect from first message, stay consistent.

## Internal Messages

- [System Message] blocks are internal context. Not user-visible.
- Rewrite system messages in your normal voice before delivering.
- Never use exec or curl for messaging.
- When asked to save or remember, MUST call write_file or edit in THIS turn.

## Memory

- Use memory_search before answering about prior work, decisions, or preferences.
- Use write_file to persist important information. No mental notes.
- Only reference MEMORY.md content in private/direct chats.

## Scheduling

- Use cron tool for periodic or timed tasks.
- Use kind: at for one-shot reminders.
',
  a.tenant_id, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM agents a
WHERE a.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM agent_context_files WHERE agent_id = a.id AND file_name = 'AGENTS_TASK.md');

DELETE FROM agent_context_files WHERE file_name = 'AGENTS_MINIMAL.md';`,

	// Version 12 → 13: Phase 10 dreaming weighted scoring signals on
	// episodic_summaries. Mirrors PG migration 000045.
	12: `ALTER TABLE episodic_summaries ADD COLUMN recall_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE episodic_summaries ADD COLUMN recall_score REAL NOT NULL DEFAULT 0;
ALTER TABLE episodic_summaries ADD COLUMN last_recalled_at TEXT;
CREATE INDEX IF NOT EXISTS idx_episodic_recall_unpromoted ON episodic_summaries(agent_id, user_id, recall_score DESC)
    WHERE promoted_at IS NULL;`,

	// Version 13 → 14: vault_documents agent_id nullable + unique index with tenant_id.
	// SQLite requires table recreation to drop NOT NULL. Preserve all data.
	13: `CREATE TABLE vault_documents_new (
    id           TEXT NOT NULL PRIMARY KEY,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id     TEXT REFERENCES agents(id) ON DELETE SET NULL,
    team_id      TEXT REFERENCES agent_teams(id) ON DELETE SET NULL,
    scope        TEXT NOT NULL DEFAULT 'personal',
    custom_scope TEXT,
    path         TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    doc_type     TEXT NOT NULL DEFAULT 'note',
    content_hash TEXT NOT NULL DEFAULT '',
    summary      TEXT NOT NULL DEFAULT '',
    metadata     TEXT DEFAULT '{}',
    created_at   TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at   TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
INSERT INTO vault_documents_new SELECT * FROM vault_documents;
DROP TABLE vault_documents;
ALTER TABLE vault_documents_new RENAME TO vault_documents;
DROP INDEX IF EXISTS idx_vault_docs_unique_path;
CREATE UNIQUE INDEX idx_vault_docs_unique_path
    ON vault_documents(tenant_id, COALESCE(agent_id, ''), COALESCE(team_id, ''), scope, path);
CREATE INDEX IF NOT EXISTS idx_vault_docs_tenant ON vault_documents(tenant_id);
CREATE INDEX IF NOT EXISTS idx_vault_docs_agent_scope ON vault_documents(agent_id, scope);
CREATE INDEX IF NOT EXISTS idx_vault_docs_type ON vault_documents(agent_id, doc_type);
CREATE INDEX IF NOT EXISTS idx_vault_docs_hash ON vault_documents(content_hash);
CREATE INDEX IF NOT EXISTS idx_vault_docs_team ON vault_documents(team_id);`,

	// Version 14 → 15: cron_jobs UNIQUE constraint on (agent_id, tenant_id, name).
	// Dedup first: keep one row per combo (SQLite has no DISTINCT ON — use GROUP BY + MIN).
	14: `DELETE FROM cron_jobs WHERE id NOT IN (
  SELECT MIN(id) FROM cron_jobs GROUP BY agent_id, tenant_id, name
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_cron_jobs_agent_tenant_name
  ON cron_jobs(agent_id, tenant_id, name);`,

	// Version 15 → 16: vault media linking schema
	// (team_task_attachments.base_name, vault_documents.path_basename,
	//  vault_links.metadata, auto-linking indexes).
	// Backfill of base_name / path_basename happens in backfillV16 below
	// (Go-loop — SQLite has no regexp_replace in modernc.org/sqlite bundle).
	15: `ALTER TABLE team_task_attachments ADD COLUMN base_name TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_tta_tenant_basename
  ON team_task_attachments(tenant_id, base_name);

ALTER TABLE vault_documents ADD COLUMN path_basename TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_vault_docs_basename
  ON vault_documents(tenant_id, path_basename);

ALTER TABLE vault_links ADD COLUMN metadata TEXT NOT NULL DEFAULT '{}';
CREATE INDEX IF NOT EXISTS idx_vault_links_source
  ON vault_links(json_extract(metadata, '$.source'))
  WHERE json_extract(metadata, '$.source') IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_vault_docs_delegation
  ON vault_documents(json_extract(metadata, '$.delegation_id'))
  WHERE json_extract(metadata, '$.delegation_id') IS NOT NULL;`,

	// Version 16 → 17: path prefix index for vault tree lazy-load queries.
	16: `CREATE INDEX IF NOT EXISTS idx_vault_docs_path_prefix ON vault_documents(tenant_id, path);`,
}

// backfillV16 populates base_name / path_basename for rows that existed
// before the v15 → v16 migration. Idempotent — re-running on already-filled
// rows is a no-op thanks to the WHERE base_name = '' filter.
func backfillV16(ctx context.Context, db *sql.DB) error {
	type row struct{ id, path string }

	// ---- team_task_attachments ----
	attRows, err := collectIDPath(ctx, db,
		`SELECT id, path FROM team_task_attachments WHERE base_name = ''`)
	if err != nil {
		return fmt.Errorf("v16 scan attachments: %w", err)
	}
	if err := updateBaseNames(ctx, db,
		`UPDATE team_task_attachments SET base_name = ? WHERE id = ?`, attRows); err != nil {
		return fmt.Errorf("v16 update attachments: %w", err)
	}

	// ---- vault_documents ----
	docRows, err := collectIDPath(ctx, db,
		`SELECT id, path FROM vault_documents WHERE path_basename = ''`)
	if err != nil {
		return fmt.Errorf("v16 scan vault_docs: %w", err)
	}
	if err := updateBaseNames(ctx, db,
		`UPDATE vault_documents SET path_basename = ? WHERE id = ?`, docRows); err != nil {
		return fmt.Errorf("v16 update vault_docs: %w", err)
	}
	return nil
}

// collectIDPath reads (id, path) tuples for a SELECT that returns exactly
// those two columns. Separated so backfillV16 stays readable.
func collectIDPath(ctx context.Context, db *sql.DB, q string) ([][2]string, error) {
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][2]string
	for rows.Next() {
		var id, path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, err
		}
		out = append(out, [2]string{id, path})
	}
	return out, rows.Err()
}

// updateBaseNames runs a prepared UPDATE inside one transaction for all rows.
// No-op when rows is empty. The prepared statement form keeps SQLite happy
// on larger backfills (<= ~10k legacy rows on typical desktop lite DBs).
func updateBaseNames(ctx context.Context, db *sql.DB, updateSQL string, rows [][2]string) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, updateSQL)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		bn := ComputeAttachmentBaseName(r[1])
		if _, err := stmt.ExecContext(ctx, bn, r[0]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("update %s: %w", r[0], err)
		}
	}
	return tx.Commit()
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
	if errors.Is(err, sql.ErrNoRows) {
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
			// Post-SQL backfill hooks for migrations needing app-side logic.
			// modernc.org/sqlite lacks regexp_replace, so the v15 → v16
			// basename columns must be populated via a Go loop.
			if v == 15 {
				if err := backfillV16(context.Background(), db); err != nil {
					return fmt.Errorf("v16 backfill: %w", err)
				}
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
