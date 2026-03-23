package pg

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// GrantToAgent grants a skill to an agent with version pinning.
// Auto-promotes visibility from 'private' to 'internal' so the skill
// becomes accessible via ListAccessible for granted agents.
func (s *PGSkillStore) GrantToAgent(ctx context.Context, skillID, agentID uuid.UUID, version int, grantedBy string) error {
	if err := store.ValidateUserID(grantedBy); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO skill_agent_grants (id, skill_id, agent_id, pinned_version, granted_by, created_at, tenant_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (skill_id, agent_id) DO UPDATE SET pinned_version = EXCLUDED.pinned_version`,
		store.GenNewID(), skillID, agentID, version, grantedBy, time.Now(), tenantIDForInsert(ctx),
	)
	if err != nil {
		return err
	}

	// Auto-promote: private → internal (so ListAccessible query includes it for granted agents)
	_, err = s.db.ExecContext(ctx,
		`UPDATE skills SET visibility = 'internal', updated_at = NOW() WHERE id = $1 AND visibility = 'private'`,
		skillID)
	if err != nil {
		slog.Warn("skill_grants: failed to auto-promote visibility", "skill_id", skillID, "error", err)
		// Non-fatal: grant was already created successfully
	}

	s.BumpVersion()
	return nil
}

// RevokeFromAgent revokes a skill grant from an agent.
// Auto-demotes visibility from 'internal' back to 'private' when no agent grants remain.
func (s *PGSkillStore) RevokeFromAgent(ctx context.Context, skillID, agentID uuid.UUID) error {
	tClause, tArgs, err := tenantClauseN(ctx, 3)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		"DELETE FROM skill_agent_grants WHERE skill_id = $1 AND agent_id = $2"+tClause,
		append([]any{skillID, agentID}, tArgs...)...)
	if err != nil {
		return err
	}

	// Atomic auto-demote: set internal → private only if zero remaining grants.
	// Uses NOT EXISTS subquery so the check + update is a single atomic SQL statement,
	// avoiding a race window between COUNT and UPDATE.
	_, err = s.db.ExecContext(ctx,
		`UPDATE skills SET visibility = 'private', updated_at = NOW()
		 WHERE id = $1 AND visibility = 'internal'
		   AND NOT EXISTS (SELECT 1 FROM skill_agent_grants WHERE skill_id = $1)`,
		skillID)
	if err != nil {
		slog.Warn("skill_grants: failed to auto-demote visibility", "skill_id", skillID, "error", err)
	}

	s.BumpVersion()
	return nil
}

// ListAgentGrants returns all skill grants for an agent.
func (s *PGSkillStore) ListAgentGrants(ctx context.Context, agentID uuid.UUID) ([]SkillGrantInfo, error) {
	tClause, tArgs, err := tenantClauseN(ctx, 2)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT skill_id, pinned_version, granted_by FROM skill_agent_grants WHERE agent_id = $1"+tClause,
		append([]any{agentID}, tArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SkillGrantInfo
	for rows.Next() {
		var g SkillGrantInfo
		if err := rows.Scan(&g.SkillID, &g.PinnedVersion, &g.GrantedBy); err != nil {
			slog.Warn("skill_grants: scan error in ListAgentGrants", "error", err)
			continue
		}
		result = append(result, g)
	}
	return result, rows.Err()
}

// GrantToUser grants a skill to a user (for internal visibility skills).
func (s *PGSkillStore) GrantToUser(ctx context.Context, skillID uuid.UUID, userID, grantedBy string) error {
	if err := store.ValidateUserID(userID); err != nil {
		return err
	}
	if err := store.ValidateUserID(grantedBy); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO skill_user_grants (id, skill_id, user_id, granted_by, created_at, tenant_id)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (skill_id, user_id) DO NOTHING`,
		store.GenNewID(), skillID, userID, grantedBy, time.Now(), tenantIDForInsert(ctx),
	)
	return err
}

// RevokeFromUser revokes a skill grant from a user.
func (s *PGSkillStore) RevokeFromUser(ctx context.Context, skillID uuid.UUID, userID string) error {
	tClause, tArgs, err := tenantClauseN(ctx, 3)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		"DELETE FROM skill_user_grants WHERE skill_id = $1 AND user_id = $2"+tClause,
		append([]any{skillID, userID}, tArgs...)...)
	return err
}

// ListAccessible returns skills accessible to a given agent+user combination.
// Access logic: public → all, private → owner only, internal → check grants.
// System skills (is_system=true) are always visible regardless of tenant.
func (s *PGSkillStore) ListAccessible(ctx context.Context, agentID uuid.UUID, userID string) ([]store.SkillInfo, error) {
	// tenant filter: system skills bypass it, tenant-owned skills are filtered
	tc, tcArgs, err := tenantClauseN(ctx, 3)
	if err != nil {
		return nil, err
	}
	tenantCond := ""
	if tc != "" {
		// tc is " AND tenant_id = $3"; we need it as an OR condition inside the WHERE
		tenantCond = fmt.Sprintf(" AND (s.is_system = true OR s.tenant_id = $%d)", 3)
		_ = tc // tcArgs carries the value
	}
	// LEFT JOIN skill_tenant_configs to exclude per-tenant disabled skills.
	// stc.enabled = false → skill explicitly disabled for this tenant.
	stcJoin := ""
	stcFilter := ""
	if len(tcArgs) > 0 {
		stcJoin = fmt.Sprintf(" LEFT JOIN skill_tenant_configs stc ON s.id = stc.skill_id AND stc.tenant_id = $%d", 3)
		stcFilter = " AND (stc.enabled IS NULL OR stc.enabled = true)"
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT s.name, s.slug, s.description, s.version, s.file_path FROM skills s
		LEFT JOIN skill_agent_grants sag ON s.id = sag.skill_id AND sag.agent_id = $1
		LEFT JOIN skill_user_grants sug ON s.id = sug.skill_id AND sug.user_id = $2`+stcJoin+`
		WHERE s.status = 'active'`+tenantCond+stcFilter+` AND (
			s.is_system = true
			OR s.visibility = 'public'
			OR (s.visibility = 'private' AND s.owner_id = $2)
			OR (s.visibility = 'internal' AND (sag.id IS NOT NULL OR sug.id IS NOT NULL))
		)
		ORDER BY s.name`, append([]any{agentID, userID}, tcArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.SkillInfo
	for rows.Next() {
		var name, slug string
		var desc *string
		var version int
		var filePath *string
		if err := rows.Scan(&name, &slug, &desc, &version, &filePath); err != nil {
			slog.Warn("skill_grants: scan error in ListAccessible", "error", err)
			continue
		}
		result = append(result, buildSkillInfo("", name, slug, desc, version, s.baseDir, filePath))
	}
	return result, rows.Err()
}

// SkillGrantInfo is a simplified grant record for API responses.
type SkillGrantInfo struct {
	SkillID       uuid.UUID `json:"skill_id"`
	PinnedVersion int       `json:"pinned_version"`
	GrantedBy     string    `json:"granted_by"`
}

// SkillWithGrantStatus represents a skill with its grant status for a specific agent.
type SkillWithGrantStatus struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	Visibility  string    `json:"visibility"`
	Version     int       `json:"version"`
	Granted     bool      `json:"granted"`
	PinnedVer   *int      `json:"pinned_version,omitempty"`
	IsSystem    bool      `json:"is_system"`
}

// ListWithGrantStatus returns all active skills with grant status for a specific agent.
func (s *PGSkillStore) ListWithGrantStatus(ctx context.Context, agentID uuid.UUID) ([]SkillWithGrantStatus, error) {
	tc, tcArgs, err := tenantClauseN(ctx, 2)
	if err != nil {
		return nil, err
	}
	tenantCond := ""
	if tc != "" {
		tenantCond = fmt.Sprintf(" AND (s.is_system = true OR s.tenant_id = $%d)", 2)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT s.id, s.name, s.slug, COALESCE(s.description, ''), s.visibility, s.version,
		        (sag.id IS NOT NULL) AS granted,
		        sag.pinned_version,
		        s.is_system
		 FROM skills s
		 LEFT JOIN skill_agent_grants sag ON s.id = sag.skill_id AND sag.agent_id = $1
		 WHERE s.status = 'active'`+tenantCond+`
		 ORDER BY s.name`, append([]any{agentID}, tcArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SkillWithGrantStatus
	for rows.Next() {
		var r SkillWithGrantStatus
		if err := rows.Scan(&r.ID, &r.Name, &r.Slug, &r.Description, &r.Visibility, &r.Version, &r.Granted, &r.PinnedVer, &r.IsSystem); err != nil {
			slog.Warn("skill_grants: scan error in ListWithGrantStatus", "error", err)
			continue
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
