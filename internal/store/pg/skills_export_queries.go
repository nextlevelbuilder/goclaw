package pg

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/google/uuid"
)

// CustomSkillExport holds portable skill metadata (no internal UUIDs in references).
type CustomSkillExport struct {
	ID          string          `json:"id" db:"-"` // UUID string — used as key for grant references within archive
	Name        string          `json:"name" db:"name"`
	Slug        string          `json:"slug" db:"slug"`
	Description *string         `json:"description,omitempty" db:"description"`
	Visibility  string          `json:"visibility" db:"visibility"`
	Version     int             `json:"version" db:"version"`
	Frontmatter json.RawMessage `json:"frontmatter,omitempty" db:"frontmatter"`
	Tags        []string        `json:"tags,omitempty" db:"tags"`
	Deps        json.RawMessage `json:"deps,omitempty" db:"deps"`
	FilePath    string          `json:"file_path,omitempty" db:"file_path"` // original path — for reading file content
}

// SkillGrantWithKey references a skill grant by agent_key (portable cross-system).
type SkillGrantWithKey struct {
	AgentKey      string `json:"agent_key" db:"agent_key"`
	PinnedVersion int    `json:"pinned_version" db:"pinned_version"`
}

// SkillsExportPreview holds lightweight counts for the export preview endpoint.
type SkillsExportPreview struct {
	CustomSkills int `json:"custom_skills" db:"custom_skills"`
	TotalGrants  int `json:"total_grants" db:"total_grants"`
}

// ExportCustomSkills returns all non-system skills scoped to the current tenant.
func ExportCustomSkills(ctx context.Context, db *sql.DB) ([]CustomSkillExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 1)
	if err != nil {
		return nil, err
	}
	var scanned []customSkillExportRow
	if err := pkgSqlxDB.SelectContext(ctx, &scanned,
		"SELECT id, name, slug, description, visibility, version, frontmatter, tags, deps, file_path"+
			" FROM skills WHERE is_system = false"+tc+
			" ORDER BY name",
		tcArgs...,
	); err != nil {
		return nil, err
	}
	result := make([]CustomSkillExport, len(scanned))
	for i := range scanned {
		result[i] = scanned[i].toCustomSkillExport()
	}
	return result, nil
}

// ExportSkillGrantsWithAgentKey returns all agent grants for a skill, resolved to agent_key.
func ExportSkillGrantsWithAgentKey(ctx context.Context, db *sql.DB, skillID uuid.UUID) ([]SkillGrantWithKey, error) {
	tc, tcArgs, _, err := scopeClauseAlias(ctx, 2, "g")
	if err != nil {
		return nil, err
	}
	var result []SkillGrantWithKey
	if err := pkgSqlxDB.SelectContext(ctx, &result,
		"SELECT a.agent_key, g.pinned_version"+
			" FROM skill_agent_grants g"+
			" JOIN agents a ON a.id = g.agent_id"+
			" WHERE g.skill_id = $1"+tc,
		append([]any{skillID}, tcArgs...)...,
	); err != nil {
		return nil, err
	}
	return result, nil
}

// ExportSkillsPreview returns aggregate counts for skills export preview.
// Uses two separate queries to avoid parameter index complexity with repeated scope clauses.
func ExportSkillsPreview(ctx context.Context, db *sql.DB) (*SkillsExportPreview, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 1)
	if err != nil {
		return nil, err
	}

	var p SkillsExportPreview
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM skills WHERE is_system = false"+tc,
		tcArgs...,
	).Scan(&p.CustomSkills); err != nil {
		return nil, err
	}

	tc2, tcArgs2, _, err := scopeClauseAlias(ctx, 1, "g")
	if err != nil {
		return nil, err
	}
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM skill_agent_grants g"+
			" JOIN skills s ON s.id = g.skill_id"+
			" WHERE s.is_system = false"+tc2,
		tcArgs2...,
	).Scan(&p.TotalGrants); err != nil {
		return nil, err
	}
	return &p, nil
}

// ExportSkillGrantAgentKeys returns all unique agent_keys that have grants for non-system skills.
// Used for import to pre-resolve agent_key → agent_id.
func ExportSkillGrantAgentKeys(ctx context.Context, db *sql.DB) ([]string, error) {
	tc, tcArgs, _, err := scopeClauseAlias(ctx, 1, "g")
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		"SELECT DISTINCT a.agent_key"+
			" FROM skill_agent_grants g"+
			" JOIN skills s ON s.id = g.skill_id"+
			" JOIN agents a ON a.id = g.agent_id"+
			" WHERE s.is_system = false"+tc,
		tcArgs...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			continue
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

// ImportSkillGrant upserts an agent grant for an imported skill.
// Uses ON CONFLICT to handle re-imports gracefully.
func ImportSkillGrant(ctx context.Context, db *sql.DB, skillID, agentID uuid.UUID, pinnedVersion int, grantedBy string) error {
	tid := tenantIDForInsert(ctx)
	_, err := db.ExecContext(ctx,
		`INSERT INTO skill_agent_grants (id, skill_id, agent_id, pinned_version, granted_by, created_at, tenant_id)
		 VALUES ($1, $2, $3, $4, $5, NOW(), $6)
		 ON CONFLICT (skill_id, agent_id) DO UPDATE SET pinned_version = EXCLUDED.pinned_version`,
		uuid.Must(uuid.NewV7()), skillID, agentID, pinnedVersion, grantedBy, tid,
	)
	return err
}
