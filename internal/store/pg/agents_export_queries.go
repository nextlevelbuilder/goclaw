package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
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
	SkillID       string `json:"skill_id"`
	PinnedVersion int    `json:"pinned_version"`
	GrantedBy     string `json:"granted_by"`
}

type MCPGrantExport struct {
	ServerID        string          `json:"server_id"`
	Enabled         bool            `json:"enabled"`
	ToolAllow       json.RawMessage `json:"tool_allow,omitempty"`
	ToolDeny        json.RawMessage `json:"tool_deny,omitempty"`
	ConfigOverrides json.RawMessage `json:"config_overrides,omitempty"`
	GrantedBy       string          `json:"granted_by"`
}

type CronJobExport struct {
	Name           string          `json:"name"`
	ScheduleKind   string          `json:"schedule_kind"`
	CronExpression *string         `json:"cron_expression,omitempty"`
	IntervalMS     *int64          `json:"interval_ms,omitempty"`
	RunAt          *string         `json:"run_at,omitempty"`
	Timezone       *string         `json:"timezone,omitempty"`
	Payload        json.RawMessage `json:"payload"`
	DeleteAfterRun bool            `json:"delete_after_run"`
}

type ConfigPermissionExport struct {
	Scope      string          `json:"scope"`
	ConfigType string          `json:"config_type"`
	UserID     string          `json:"user_id"`
	Permission string          `json:"permission"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	GrantedBy  *string         `json:"granted_by,omitempty"`
}

type UserProfileExport struct {
	UserID    string  `json:"user_id"`
	Workspace *string `json:"workspace,omitempty"`
}

type UserOverrideExport struct {
	UserID   string          `json:"user_id"`
	Provider *string         `json:"provider,omitempty"`
	Model    *string         `json:"model,omitempty"`
	Settings json.RawMessage `json:"settings,omitempty"`
}

type ExportPreview struct {
	ContextFiles     int `json:"context_files"`
	UserContextFiles int `json:"user_context_files_users"`
	MemoryGlobal     int `json:"memory_global"`
	MemoryPerUser    int `json:"memory_per_user"`
	KGEntities       int `json:"kg_entities"`
	KGRelations      int `json:"kg_relations"`
	CronJobs         int `json:"cron_jobs"`
	UserProfiles     int `json:"user_profiles"`
	UserOverrides    int `json:"user_overrides"`
	// Team section
	TeamTasks   int `json:"team_tasks"`
	TeamMembers int `json:"team_members"`
	AgentLinks  int `json:"agent_links"`
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
		rows, err := db.QueryContext(ctx,
			"SELECT id, agent_id, user_id, external_id, name, entity_type, description,"+
				" properties, source_id, confidence, created_at, updated_at"+
				" FROM kg_entities WHERE agent_id = $1"+tc+
				" AND id > $"+itoa(cursorParam)+
				" ORDER BY id LIMIT $"+itoa(limitParam),
			args...,
		)
		if err != nil {
			return nil, err
		}

		batch, scanErr := scanEntities(rows)
		rows.Close()
		if scanErr != nil {
			return nil, scanErr
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
		rows, err := db.QueryContext(ctx,
			"SELECT id, agent_id, user_id, source_entity_id, relation_type, target_entity_id,"+
				" confidence, properties, created_at"+
				" FROM kg_relations WHERE agent_id = $1"+tc+
				" AND id > $"+itoa(cursorParam)+
				" ORDER BY id LIMIT $"+itoa(limitParam),
			args...,
		)
		if err != nil {
			return nil, err
		}

		batch, scanErr := scanRelations(rows)
		rows.Close()
		if scanErr != nil {
			return nil, scanErr
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
			(SELECT COUNT(*) FROM user_agent_overrides   WHERE agent_id = $1`+tc+`) AS user_overrides
	`, args...).Scan(
		&p.ContextFiles, &p.UserContextFiles,
		&p.MemoryGlobal, &p.MemoryPerUser,
		&p.KGEntities, &p.KGRelations,
		&p.CronJobs,
		&p.UserProfiles, &p.UserOverrides,
	)
	if err != nil {
		return nil, err
	}

	// Team counts (separate query — agent may not be a lead)
	p.TeamTasks, p.TeamMembers, p.AgentLinks, _ = ExportTeamPreviewCounts(ctx, db, agentID)

	return &p, nil
}

// ExportSkillGrants returns all skill grants for the given agent.
func ExportSkillGrants(ctx context.Context, db *sql.DB, agentID uuid.UUID) ([]SkillGrantExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		"SELECT skill_id, pinned_version, granted_by FROM skill_agent_grants WHERE agent_id = $1"+tc,
		append([]any{agentID}, tcArgs...)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SkillGrantExport
	for rows.Next() {
		var g SkillGrantExport
		if err := rows.Scan(&g.SkillID, &g.PinnedVersion, &g.GrantedBy); err != nil {
			slog.Warn("export.scan", "error", err)
			continue
		}
		result = append(result, g)
	}
	return result, rows.Err()
}

// ExportMCPGrants returns all MCP server grants for the given agent.
func ExportMCPGrants(ctx context.Context, db *sql.DB, agentID uuid.UUID) ([]MCPGrantExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		"SELECT server_id, enabled, tool_allow, tool_deny, config_overrides, granted_by"+
			" FROM mcp_agent_grants WHERE agent_id = $1"+tc,
		append([]any{agentID}, tcArgs...)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []MCPGrantExport
	for rows.Next() {
		var g MCPGrantExport
		if err := rows.Scan(&g.ServerID, &g.Enabled, &g.ToolAllow, &g.ToolDeny, &g.ConfigOverrides, &g.GrantedBy); err != nil {
			slog.Warn("export.scan", "error", err)
			continue
		}
		result = append(result, g)
	}
	return result, rows.Err()
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
	rows, err := db.QueryContext(ctx,
		"SELECT scope, config_type, user_id, permission, metadata, granted_by"+
			" FROM agent_config_permissions WHERE agent_id = $1"+tc,
		append([]any{agentID}, tcArgs...)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ConfigPermissionExport
	for rows.Next() {
		var p ConfigPermissionExport
		if err := rows.Scan(&p.Scope, &p.ConfigType, &p.UserID, &p.Permission, &p.Metadata, &p.GrantedBy); err != nil {
			slog.Warn("export.scan", "error", err)
			continue
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// ExportUserProfiles returns all user profiles for the given agent.
func ExportUserProfiles(ctx context.Context, db *sql.DB, agentID uuid.UUID) ([]UserProfileExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		"SELECT user_id, workspace FROM user_agent_profiles WHERE agent_id = $1"+tc,
		append([]any{agentID}, tcArgs...)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []UserProfileExport
	for rows.Next() {
		var p UserProfileExport
		if err := rows.Scan(&p.UserID, &p.Workspace); err != nil {
			slog.Warn("export.scan", "error", err)
			continue
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// ExportUserOverrides returns all user model overrides for the given agent.
func ExportUserOverrides(ctx context.Context, db *sql.DB, agentID uuid.UUID) ([]UserOverrideExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		"SELECT user_id, provider, model, settings"+
			" FROM user_agent_overrides WHERE agent_id = $1"+tc,
		append([]any{agentID}, tcArgs...)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []UserOverrideExport
	for rows.Next() {
		var o UserOverrideExport
		if err := rows.Scan(&o.UserID, &o.Provider, &o.Model, &o.Settings); err != nil {
			slog.Warn("export.scan", "error", err)
			continue
		}
		result = append(result, o)
	}
	return result, rows.Err()
}

