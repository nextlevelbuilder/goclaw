package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// CustomSkillExport holds portable skill metadata (no internal UUIDs in references).
type CustomSkillExport struct {
	ID          string          `json:"id"` // UUID string — used as key for grant references within archive
	Name        string          `json:"name"`
	Slug        string          `json:"slug"`
	Description *string         `json:"description,omitempty"`
	Visibility  string          `json:"visibility"`
	Version     int             `json:"version"`
	Frontmatter json.RawMessage `json:"frontmatter,omitempty"`
	Tags        []string        `json:"tags,omitempty"`
	Deps        json.RawMessage `json:"deps,omitempty"`
	FilePath    string          `json:"file_path,omitempty"` // original path — for reading file content
}

// SkillGrantWithKey references a skill grant by agent_key (portable cross-system).
type SkillGrantWithKey struct {
	AgentKey      string `json:"agent_key"`
	PinnedVersion int    `json:"pinned_version"`
}

// SkillsExportPreview holds lightweight counts for the export preview endpoint.
type SkillsExportPreview struct {
	CustomSkills int `json:"custom_skills"`
	TotalGrants  int `json:"total_grants"`
}

// ExportCustomSkills returns all non-system skills scoped to the current tenant.
func ExportCustomSkills(ctx context.Context, db *sql.DB) ([]CustomSkillExport, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 1)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		"SELECT id, name, slug, description, visibility, version, frontmatter, tags, deps, file_path"+
			" FROM skills WHERE is_system = false"+tc+
			" ORDER BY name",
		tcArgs...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []CustomSkillExport
	for rows.Next() {
		var (
			id          uuid.UUID
			name        string
			slug        string
			desc        *string
			visibility  string
			version     int
			fmRaw       []byte
			tags        []string
			depsRaw     []byte
			filePath    *string
		)
		if err := rows.Scan(&id, &name, &slug, &desc, &visibility, &version, &fmRaw, pq.Array(&tags), &depsRaw, &filePath); err != nil {
			slog.Warn("skills_export.scan", "error", err)
			continue
		}
		sk := CustomSkillExport{
			ID:         id.String(),
			Name:       name,
			Slug:       slug,
			Description: desc,
			Visibility: visibility,
			Version:    version,
			Tags:       tags,
		}
		if len(fmRaw) > 0 {
			sk.Frontmatter = json.RawMessage(fmRaw)
		}
		if len(depsRaw) > 0 {
			sk.Deps = json.RawMessage(depsRaw)
		}
		if filePath != nil {
			sk.FilePath = *filePath
		}
		result = append(result, sk)
	}
	return result, rows.Err()
}

// ExportSkillGrantsWithAgentKey returns all agent grants for a skill, resolved to agent_key.
func ExportSkillGrantsWithAgentKey(ctx context.Context, db *sql.DB, skillID uuid.UUID) ([]SkillGrantWithKey, error) {
	tc, tcArgs, _, err := scopeClauseAlias(ctx, 2, "g")
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		"SELECT a.agent_key, g.pinned_version"+
			" FROM skill_agent_grants g"+
			" JOIN agents a ON a.id = g.agent_id"+
			" WHERE g.skill_id = $1"+tc,
		append([]any{skillID}, tcArgs...)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SkillGrantWithKey
	for rows.Next() {
		var g SkillGrantWithKey
		if err := rows.Scan(&g.AgentKey, &g.PinnedVersion); err != nil {
			slog.Warn("skills_export.grants.scan", "error", err)
			continue
		}
		result = append(result, g)
	}
	return result, rows.Err()
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
