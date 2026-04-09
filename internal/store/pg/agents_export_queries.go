package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Export types — used exclusively by the agent export pipeline.

type AgentContextFileExport struct {
	FileName string
	Content  string
}

type UserContextFileExport struct {
	UserID   string
	FileName string
	Content  string
}

type MemoryDocExport struct {
	Path    string
	Content string
	UserID  string
}

// Phase 2 export types.

type SkillGrantExport struct {
	SkillID       string `json:"skill_id" db:"skill_id"`
	PinnedVersion int    `json:"pinned_version" db:"pinned_version"`
	GrantedBy     string `json:"granted_by" db:"granted_by"`
}

type MCPGrantExport struct {
	ServerID        string          `json:"server_id" db:"server_id"`
	Enabled         bool            `json:"enabled" db:"enabled"`
	ToolAllow       json.RawMessage `json:"tool_allow,omitempty" db:"tool_allow"`
	ToolDeny        json.RawMessage `json:"tool_deny,omitempty" db:"tool_deny"`
	ConfigOverrides json.RawMessage `json:"config_overrides,omitempty" db:"config_overrides"`
	GrantedBy       string          `json:"granted_by" db:"granted_by"`
}

type CronJobExport struct {
	Name           string          `json:"name" db:"name"`
	ScheduleKind   string          `json:"schedule_kind" db:"schedule_kind"`
	CronExpression *string         `json:"cron_expression,omitempty" db:"cron_expression"`
	IntervalMS     *int64          `json:"interval_ms,omitempty" db:"interval_ms"`
	RunAt          *string         `json:"run_at,omitempty" db:"run_at"`
	Timezone       *string         `json:"timezone,omitempty" db:"timezone"`
	Payload        json.RawMessage `json:"payload" db:"payload"`
	DeleteAfterRun bool            `json:"delete_after_run" db:"delete_after_run"`
}

type ConfigPermissionExport struct {
	Scope      string          `json:"scope" db:"scope"`
	ConfigType string          `json:"config_type" db:"config_type"`
	UserID     string          `json:"user_id" db:"user_id"`
	Permission string          `json:"permission" db:"permission"`
	Metadata   json.RawMessage `json:"metadata,omitempty" db:"metadata"`
	GrantedBy  *string         `json:"granted_by,omitempty" db:"granted_by"`
}

type UserProfileExport struct {
	UserID    string  `json:"user_id" db:"user_id"`
	Workspace *string `json:"workspace,omitempty" db:"workspace"`
}

type UserOverrideExport struct {
	UserID   string          `json:"user_id" db:"user_id"`
	Provider *string         `json:"provider,omitempty" db:"provider"`
	Model    *string         `json:"model,omitempty" db:"model"`
	Settings json.RawMessage `json:"settings,omitempty" db:"settings"`
}

// EpisodicSummaryExport is the portable representation of a Tier 2 episodic memory entry.
// Excludes: id, tenant_id, agent_id (reconstructed on import), embedding (vector), promoted_at.
type EpisodicSummaryExport struct {
	UserID     string   `json:"user_id"`
	SessionKey string   `json:"session_key"`
	Summary    string   `json:"summary"`
	KeyTopics  []string `json:"key_topics"`
	L0Abstract string   `json:"l0_abstract"`
	SourceType string   `json:"source_type"`
	SourceID   string   `json:"source_id"`
	TurnCount  int      `json:"turn_count"`
	TokenCount int      `json:"token_count"`
	CreatedAt  string   `json:"created_at"` // RFC3339 UTC
	ExpiresAt  *string  `json:"expires_at,omitempty"`
}

// VaultDocumentExport is the portable representation of a vault document.
// Excludes: id, tenant_id, agent_id, team_id (reconstructed on import), embedding (re-indexed by FS sync).
type VaultDocumentExport struct {
	Scope       string          `json:"scope" db:"scope"`
	CustomScope *string         `json:"custom_scope,omitempty" db:"custom_scope"`
	Path        string          `json:"path" db:"path"`
	Title       string          `json:"title" db:"title"`
	DocType     string          `json:"doc_type" db:"doc_type"`
	ContentHash string          `json:"content_hash" db:"content_hash"`
	Summary     string          `json:"summary" db:"summary"`
	Metadata    json.RawMessage `json:"metadata,omitempty" db:"metadata"`
	CreatedAt   string          `json:"created_at" db:"created_at"`
	UpdatedAt   string          `json:"updated_at" db:"updated_at"`
}

// VaultLinkExport is the portable representation of a vault link.
// Links reference documents by path (not UUID) for portability across systems.
type VaultLinkExport struct {
	FromDocPath string `json:"from_doc_path"`
	ToDocPath   string `json:"to_doc_path"`
	LinkType    string `json:"link_type" db:"link_type"`
	Context     string `json:"context" db:"context"`
	CreatedAt   string `json:"created_at" db:"created_at"`
}

// EvolutionMetricExport is the portable representation of an evolution metric data point.
// Excludes: id, tenant_id, agent_id (reconstructed on import).
type EvolutionMetricExport struct {
	SessionKey string          `json:"session_key" db:"session_key"`
	MetricType string          `json:"metric_type" db:"metric_type"`
	MetricKey  string          `json:"metric_key" db:"metric_key"`
	Value      json.RawMessage `json:"value" db:"value"`
	CreatedAt  string          `json:"created_at" db:"created_at"` // RFC3339 UTC
}

// EvolutionSuggestionExport is the portable representation of an evolution suggestion.
// Excludes: id, tenant_id, agent_id (reconstructed on import).
type EvolutionSuggestionExport struct {
	SuggestionType string          `json:"suggestion_type" db:"suggestion_type"`
	Suggestion     string          `json:"suggestion" db:"suggestion"`
	Rationale      string          `json:"rationale" db:"rationale"`
	Parameters     json.RawMessage `json:"parameters,omitempty" db:"parameters"`
	Status         string          `json:"status" db:"status"`
	ReviewedBy     string          `json:"reviewed_by,omitempty" db:"reviewed_by"`
	ReviewedAt     *string         `json:"reviewed_at,omitempty" db:"reviewed_at"`
	CreatedAt      string          `json:"created_at" db:"created_at"`
}

type ExportPreview struct {
	ContextFiles       int `json:"context_files" db:"context_files"`
	UserContextFiles   int `json:"user_context_files_users" db:"user_context_files_users"`
	MemoryGlobal       int `json:"memory_global" db:"memory_global"`
	MemoryPerUser      int `json:"memory_per_user" db:"memory_per_user"`
	KGEntities         int `json:"kg_entities" db:"kg_entities"`
	KGRelations        int `json:"kg_relations" db:"kg_relations"`
	CronJobs           int `json:"cron_jobs" db:"cron_jobs"`
	UserProfiles       int `json:"user_profiles" db:"user_profiles"`
	UserOverrides      int `json:"user_overrides" db:"user_overrides"`
	EpisodicSummaries  int `json:"episodic_summaries" db:"episodic_summaries"`
	// Evolution section (Stage 1 + Stage 2 self-evolution)
	EvolutionMetrics     int `json:"evolution_metrics" db:"evolution_metrics"`
	EvolutionSuggestions int `json:"evolution_suggestions" db:"evolution_suggestions"`
	// Vault section (Knowledge Vault documents and links)
	VaultDocuments int `json:"vault_documents" db:"vault_documents"`
	VaultLinks     int `json:"vault_links" db:"vault_links"`
	// Team section
	TeamTasks   int `json:"team_tasks" db:"team_tasks"`
	TeamMembers int `json:"team_members" db:"team_members"`
	AgentLinks  int `json:"agent_links" db:"agent_links"`
}

const exportBatchSize = 1000

// ExportAgentContextFiles returns all agent-level context files for the given agent.
func ExportAgentContextFiles(ctx context.Context, db *sql.DB, agentID uuid.UUID) ([]AgentContextFileExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		"SELECT file_name, content FROM agent_context_files WHERE agent_id = $1"+tc,
		append([]any{agentID}, tcArgs...)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []AgentContextFileExport
	for rows.Next() {
		var f AgentContextFileExport
		if err := rows.Scan(&f.FileName, &f.Content); err != nil {
			slog.Warn("export.scan", "error", err)
			continue
		}
		result = append(result, f)
	}
	return result, rows.Err()
}

// ExportUserContextFiles returns all per-user context files for the given agent (all users).
func ExportUserContextFiles(ctx context.Context, db *sql.DB, agentID uuid.UUID) ([]UserContextFileExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		"SELECT user_id, file_name, content FROM user_context_files WHERE agent_id = $1"+tc,
		append([]any{agentID}, tcArgs...)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []UserContextFileExport
	for rows.Next() {
		var f UserContextFileExport
		if err := rows.Scan(&f.UserID, &f.FileName, &f.Content); err != nil {
			slog.Warn("export.scan", "error", err)
			continue
		}
		result = append(result, f)
	}
	return result, rows.Err()
}

// ExportMemoryDocuments returns all memory documents for the given agent across all users,
// using cursor-based pagination to handle large datasets.
func ExportMemoryDocuments(ctx context.Context, db *sql.DB, agentID uuid.UUID) ([]MemoryDocExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}

	baseArgs := append([]any{agentID}, tcArgs...)
	cursorParam := len(baseArgs) + 1
	limitParam := cursorParam + 1

	var result []MemoryDocExport
	cursor := uuid.Nil

	for {
		args := append(append([]any{}, baseArgs...), cursor, exportBatchSize)
		rows, err := db.QueryContext(ctx,
			"SELECT id, path, content, COALESCE(user_id, '') FROM memory_documents"+
				" WHERE agent_id = $1"+tc+
				" AND id > $"+itoa(cursorParam)+
				" ORDER BY id LIMIT $"+itoa(limitParam),
			args...,
		)
		if err != nil {
			return nil, err
		}

		count := 0
		for rows.Next() {
			var id uuid.UUID
			var d MemoryDocExport
			if err := rows.Scan(&id, &d.Path, &d.Content, &d.UserID); err != nil {
				continue
			}
			result = append(result, d)
			cursor = id
			count++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if count < exportBatchSize {
			break
		}
	}
	return result, nil
}

// ExportKGEntities returns all KG entities for the given agent across all user scopes,
// using cursor-based pagination.
func ExportKGEntities(ctx context.Context, db *sql.DB, agentID uuid.UUID) ([]store.Entity, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}

	baseArgs := append([]any{agentID}, tcArgs...)
	cursorParam := len(baseArgs) + 1
	limitParam := cursorParam + 1

	var result []store.Entity
	cursor := uuid.Nil

	for {
		args := append(append([]any{}, baseArgs...), cursor, exportBatchSize)
		var eRows []entityTemporalRow
		if err := pkgSqlxDB.SelectContext(ctx, &eRows,
			"SELECT id, agent_id, user_id, external_id, name, entity_type, description,"+
				" properties, source_id, confidence, created_at, updated_at, valid_from, valid_until"+
				" FROM kg_entities WHERE agent_id = $1"+tc+
				" AND id > $"+itoa(cursorParam)+
				" ORDER BY id LIMIT $"+itoa(limitParam),
			args...,
		); err != nil {
			return nil, err
		}
		batch := make([]store.Entity, len(eRows))
		for i := range eRows {
			batch[i] = eRows[i].toEntity()
		}
		result = append(result, batch...)
		if len(batch) < exportBatchSize {
			break
		}
		// Advance cursor: last entity ID
		lastID := mustParseUUID(batch[len(batch)-1].ID)
		cursor = lastID
	}
	return result, nil
}

// ExportKGRelations returns all KG relations for the given agent across all user scopes,
// using cursor-based pagination.
func ExportKGRelations(ctx context.Context, db *sql.DB, agentID uuid.UUID) ([]store.Relation, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}

	baseArgs := append([]any{agentID}, tcArgs...)
	cursorParam := len(baseArgs) + 1
	limitParam := cursorParam + 1

	var result []store.Relation
	cursor := uuid.Nil

	for {
		args := append(append([]any{}, baseArgs...), cursor, exportBatchSize)
		var rRows []relationExportRow
		if err := pkgSqlxDB.SelectContext(ctx, &rRows,
			"SELECT id, agent_id, user_id, source_entity_id, relation_type, target_entity_id,"+
				" confidence, properties, created_at, valid_from, valid_until"+
				" FROM kg_relations WHERE agent_id = $1"+tc+
				" AND id > $"+itoa(cursorParam)+
				" ORDER BY id LIMIT $"+itoa(limitParam),
			args...,
		); err != nil {
			return nil, err
		}
		batch := make([]store.Relation, len(rRows))
		for i := range rRows {
			batch[i] = rRows[i].toRelation()
		}
		result = append(result, batch...)
		if len(batch) < exportBatchSize {
			break
		}
		lastID := mustParseUUID(batch[len(batch)-1].ID)
		cursor = lastID
	}
	return result, nil
}

// ExportPreviewCounts returns aggregate counts for all exportable sections of an agent.
func ExportPreviewCounts(ctx context.Context, db *sql.DB, agentID uuid.UUID) (*ExportPreview, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	args := append([]any{agentID}, tcArgs...)

	var p ExportPreview
	err = db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM agent_context_files    WHERE agent_id = $1`+tc+`) AS context_files,
			(SELECT COUNT(DISTINCT user_id) FROM user_context_files WHERE agent_id = $1`+tc+`) AS user_context_files_users,
			(SELECT COUNT(*) FROM memory_documents       WHERE agent_id = $1 AND user_id IS NULL`+tc+`) AS memory_global,
			(SELECT COUNT(*) FROM memory_documents       WHERE agent_id = $1 AND user_id IS NOT NULL`+tc+`) AS memory_per_user,
			(SELECT COUNT(*) FROM kg_entities            WHERE agent_id = $1`+tc+`) AS kg_entities,
			(SELECT COUNT(*) FROM kg_relations           WHERE agent_id = $1`+tc+`) AS kg_relations,
			(SELECT COUNT(*) FROM cron_jobs              WHERE agent_id = $1`+tc+`) AS cron_jobs,
			(SELECT COUNT(*) FROM user_agent_profiles    WHERE agent_id = $1`+tc+`) AS user_profiles,
			(SELECT COUNT(*) FROM user_agent_overrides   WHERE agent_id = $1`+tc+`) AS user_overrides,
			(SELECT COUNT(*) FROM episodic_summaries         WHERE agent_id = $1`+tc+`) AS episodic_summaries,
			(SELECT COUNT(*) FROM agent_evolution_metrics    WHERE agent_id = $1`+tc+`) AS evolution_metrics,
			(SELECT COUNT(*) FROM agent_evolution_suggestions WHERE agent_id = $1`+tc+`) AS evolution_suggestions
	`, args...).Scan(
		&p.ContextFiles, &p.UserContextFiles,
		&p.MemoryGlobal, &p.MemoryPerUser,
		&p.KGEntities, &p.KGRelations,
		&p.CronJobs,
		&p.UserProfiles, &p.UserOverrides,
		&p.EpisodicSummaries,
		&p.EvolutionMetrics, &p.EvolutionSuggestions,
	)
	if err != nil {
		return nil, err
	}

	// Team counts (separate query — agent may not be a lead)
	p.TeamTasks, p.TeamMembers, p.AgentLinks, _ = ExportTeamPreviewCounts(ctx, db, agentID)

	// Vault counts (separate query — vault_documents/links tables)
	_ = db.QueryRowContext(ctx,
		`SELECT
			(SELECT COUNT(*) FROM vault_documents WHERE agent_id = $1`+tc+`) AS vault_documents,
			(SELECT COUNT(*) FROM vault_links vl
			  JOIN vault_documents fd ON vl.from_doc_id = fd.id
			  WHERE fd.agent_id = $1`+tc+`) AS vault_links`,
		args...,
	).Scan(&p.VaultDocuments, &p.VaultLinks)

	return &p, nil
}

// ExportSkillGrants returns all skill grants for the given agent.
func ExportSkillGrants(ctx context.Context, db *sql.DB, agentID uuid.UUID) ([]SkillGrantExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	var result []SkillGrantExport
	err = pkgSqlxDB.SelectContext(ctx, &result,
		"SELECT skill_id, pinned_version, granted_by FROM skill_agent_grants WHERE agent_id = $1"+tc,
		append([]any{agentID}, tcArgs...)...,
	)
	return result, err
}

// ExportMCPGrants returns all MCP server grants for the given agent.
func ExportMCPGrants(ctx context.Context, db *sql.DB, agentID uuid.UUID) ([]MCPGrantExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	var result []MCPGrantExport
	err = pkgSqlxDB.SelectContext(ctx, &result,
		"SELECT server_id, enabled, tool_allow, tool_deny, config_overrides, granted_by"+
			" FROM mcp_agent_grants WHERE agent_id = $1"+tc,
		append([]any{agentID}, tcArgs...)...,
	)
	return result, err
}

// ExportCronJobs returns all cron jobs for the given agent.
func ExportCronJobs(ctx context.Context, db *sql.DB, agentID uuid.UUID) ([]CronJobExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		"SELECT name, schedule_kind, cron_expression, interval_ms,"+
			" to_char(run_at AT TIME ZONE 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS\"Z\"'), timezone, payload, delete_after_run"+
			" FROM cron_jobs WHERE agent_id = $1"+tc,
		append([]any{agentID}, tcArgs...)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []CronJobExport
	for rows.Next() {
		var j CronJobExport
		if err := rows.Scan(&j.Name, &j.ScheduleKind, &j.CronExpression, &j.IntervalMS,
			&j.RunAt, &j.Timezone, &j.Payload, &j.DeleteAfterRun); err != nil {
			slog.Warn("export.scan", "error", err)
			continue
		}
		result = append(result, j)
	}
	return result, rows.Err()
}

// ExportConfigPermissions returns all agent config permissions for the given agent.
func ExportConfigPermissions(ctx context.Context, db *sql.DB, agentID uuid.UUID) ([]ConfigPermissionExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	var result []ConfigPermissionExport
	err = pkgSqlxDB.SelectContext(ctx, &result,
		"SELECT scope, config_type, user_id, permission, metadata, granted_by"+
			" FROM agent_config_permissions WHERE agent_id = $1"+tc,
		append([]any{agentID}, tcArgs...)...,
	)
	return result, err
}

// ExportUserProfiles returns all user profiles for the given agent.
func ExportUserProfiles(ctx context.Context, db *sql.DB, agentID uuid.UUID) ([]UserProfileExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	var result []UserProfileExport
	err = pkgSqlxDB.SelectContext(ctx, &result,
		"SELECT user_id, workspace FROM user_agent_profiles WHERE agent_id = $1"+tc,
		append([]any{agentID}, tcArgs...)...,
	)
	return result, err
}

// ExportUserOverrides returns all user model overrides for the given agent.
func ExportUserOverrides(ctx context.Context, db *sql.DB, agentID uuid.UUID) ([]UserOverrideExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	var result []UserOverrideExport
	err = pkgSqlxDB.SelectContext(ctx, &result,
		"SELECT user_id, provider, model, settings"+
			" FROM user_agent_overrides WHERE agent_id = $1"+tc,
		append([]any{agentID}, tcArgs...)...,
	)
	return result, err
}

// ExportEvolutionMetrics returns all evolution metrics for the given agent using cursor-based pagination.
// Excludes: id, tenant_id, agent_id (reconstructed on import).
func ExportEvolutionMetrics(ctx context.Context, db *sql.DB, agentID uuid.UUID) ([]EvolutionMetricExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}

	baseArgs := append([]any{agentID}, tcArgs...)
	cursorParam := len(baseArgs) + 1
	limitParam := cursorParam + 1

	var result []EvolutionMetricExport
	cursor := uuid.Nil

	for {
		args := append(append([]any{}, baseArgs...), cursor, exportBatchSize)
		rows, err := db.QueryContext(ctx,
			"SELECT id, session_key, metric_type, metric_key, value,"+
				" to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS\"Z\"') AS created_at"+
				" FROM agent_evolution_metrics WHERE agent_id = $1"+tc+
				" AND id > $"+itoa(cursorParam)+
				" ORDER BY id LIMIT $"+itoa(limitParam),
			args...,
		)
		if err != nil {
			return nil, err
		}

		count := 0
		for rows.Next() {
			var id uuid.UUID
			var m EvolutionMetricExport
			if err := rows.Scan(&id, &m.SessionKey, &m.MetricType, &m.MetricKey, &m.Value, &m.CreatedAt); err != nil {
				slog.Warn("export.evolution_metrics.scan", "error", err)
				continue
			}
			result = append(result, m)
			cursor = id
			count++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if count < exportBatchSize {
			break
		}
	}
	return result, nil
}

// ExportEvolutionSuggestions returns all evolution suggestions for the given agent.
// Low volume (suggestions are human-reviewed), so simple SELECT without cursor pagination.
// Excludes: id, tenant_id, agent_id (reconstructed on import).
func ExportEvolutionSuggestions(ctx context.Context, db *sql.DB, agentID uuid.UUID) ([]EvolutionSuggestionExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		"SELECT suggestion_type, suggestion, rationale, parameters, status,"+
			" COALESCE(reviewed_by, ''),"+
			" to_char(reviewed_at AT TIME ZONE 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS\"Z\"'),"+
			" to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS\"Z\"')"+
			" FROM agent_evolution_suggestions WHERE agent_id = $1"+tc+
			" ORDER BY created_at",
		append([]any{agentID}, tcArgs...)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []EvolutionSuggestionExport
	for rows.Next() {
		var s EvolutionSuggestionExport
		if err := rows.Scan(
			&s.SuggestionType, &s.Suggestion, &s.Rationale, &s.Parameters, &s.Status,
			&s.ReviewedBy, &s.ReviewedAt, &s.CreatedAt,
		); err != nil {
			slog.Warn("export.evolution_suggestions.scan", "error", err)
			continue
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

// ExportEpisodicSummaries returns all episodic summaries for the given agent using cursor-based pagination.
// Excludes: id, tenant_id, agent_id (reconstructed), embedding, promoted_at.
func ExportEpisodicSummaries(ctx context.Context, db *sql.DB, agentID uuid.UUID) ([]EpisodicSummaryExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}

	baseArgs := append([]any{agentID}, tcArgs...)
	cursorParam := len(baseArgs) + 1
	limitParam := cursorParam + 1

	var result []EpisodicSummaryExport
	cursor := uuid.Nil

	for {
		args := append(append([]any{}, baseArgs...), cursor, exportBatchSize)
		rows, err := db.QueryContext(ctx,
			"SELECT user_id, session_key, summary, key_topics, l0_abstract,"+
				" source_type, source_id, turn_count, token_count,"+
				" to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS\"Z\"') AS created_at,"+
				" to_char(expires_at AT TIME ZONE 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS\"Z\"') AS expires_at,"+
				" id"+
				" FROM episodic_summaries WHERE agent_id = $1"+tc+
				" AND id > $"+itoa(cursorParam)+
				" ORDER BY id LIMIT $"+itoa(limitParam),
			args...,
		)
		if err != nil {
			return nil, err
		}

		count := 0
		for rows.Next() {
			var ep EpisodicSummaryExport
			var topics pq.StringArray
			var id uuid.UUID
			if err := rows.Scan(
				&ep.UserID, &ep.SessionKey, &ep.Summary, &topics, &ep.L0Abstract,
				&ep.SourceType, &ep.SourceID, &ep.TurnCount, &ep.TokenCount,
				&ep.CreatedAt, &ep.ExpiresAt,
				&id,
			); err != nil {
				slog.Warn("export.episodic.scan", "error", err)
				continue
			}
			ep.KeyTopics = []string(topics)
			result = append(result, ep)
			cursor = id
			count++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if count < exportBatchSize {
			break
		}
	}
	return result, nil
}

// ExportVaultDocuments returns all vault documents for the given agent using cursor-based pagination.
// Excludes: id, tenant_id, agent_id, team_id (reconstructed on import), embedding (re-indexed by FS sync).
func ExportVaultDocuments(ctx context.Context, db *sql.DB, agentID uuid.UUID) ([]VaultDocumentExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}

	baseArgs := append([]any{agentID}, tcArgs...)
	cursorParam := len(baseArgs) + 1
	limitParam := cursorParam + 1

	var result []VaultDocumentExport
	cursor := uuid.Nil

	for {
		args := append(append([]any{}, baseArgs...), cursor, exportBatchSize)
		rows, err := db.QueryContext(ctx,
			"SELECT scope, custom_scope, path, title, doc_type, content_hash, summary, metadata,"+
				" to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS\"Z\"') AS created_at,"+
				" to_char(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS\"Z\"') AS updated_at,"+
				" id"+
				" FROM vault_documents WHERE agent_id = $1"+tc+
				" AND id > $"+itoa(cursorParam)+
				" ORDER BY id LIMIT $"+itoa(limitParam),
			args...,
		)
		if err != nil {
			return nil, err
		}

		count := 0
		for rows.Next() {
			var d VaultDocumentExport
			var id uuid.UUID
			if err := rows.Scan(
				&d.Scope, &d.CustomScope, &d.Path, &d.Title, &d.DocType,
				&d.ContentHash, &d.Summary, &d.Metadata,
				&d.CreatedAt, &d.UpdatedAt, &id,
			); err != nil {
				slog.Warn("export.vault_documents.scan", "error", err)
				continue
			}
			result = append(result, d)
			cursor = id
			count++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if count < exportBatchSize {
			break
		}
	}
	return result, nil
}

// ExportVaultLinks returns all vault links for the given agent, resolving doc UUIDs to paths.
// Links are only exported where the source doc belongs to the agent.
func ExportVaultLinks(ctx context.Context, db *sql.DB, agentID uuid.UUID) ([]VaultLinkExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		"SELECT fd.path, td.path, vl.link_type, vl.context,"+
			" to_char(vl.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS\"Z\"')"+
			" FROM vault_links vl"+
			" JOIN vault_documents fd ON vl.from_doc_id = fd.id"+
			" JOIN vault_documents td ON vl.to_doc_id = td.id"+
			" WHERE fd.agent_id = $1"+tc+
			" ORDER BY vl.created_at",
		append([]any{agentID}, tcArgs...)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []VaultLinkExport
	for rows.Next() {
		var l VaultLinkExport
		if err := rows.Scan(&l.FromDocPath, &l.ToDocPath, &l.LinkType, &l.Context, &l.CreatedAt); err != nil {
			slog.Warn("export.vault_links.scan", "error", err)
			continue
		}
		result = append(result, l)
	}
	return result, rows.Err()
}

