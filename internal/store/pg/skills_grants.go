package pg

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// GrantToAgent grants a skill to an agent with version pinning.
// Auto-promotes visibility from 'private' to 'internal' so the skill
// becomes accessible via ListAccessible for granted agents.
// Validates both the skill and agent belong to the requesting tenant.
func (s *PGSkillStore) GrantToAgent(ctx context.Context, skillID, agentID uuid.UUID, version int, grantedBy string, canManage ...bool) error {
	if err := store.ValidateUserID(grantedBy); err != nil {
		return err
	}
	tid := tenantIDForInsert(ctx)
	if err := s.verifySkillGrantScope(ctx, skillID, agentID, tid); err != nil {
		return err
	}

	now := time.Now()
	var err error
	if len(canManage) > 0 {
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO skill_agent_grants (id, skill_id, agent_id, pinned_version, granted_by, can_manage, created_at, tenant_id)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			 ON CONFLICT (skill_id, agent_id) DO UPDATE SET
			    pinned_version = EXCLUDED.pinned_version,
			    granted_by = EXCLUDED.granted_by,
			    can_manage = EXCLUDED.can_manage`,
			store.GenNewID(), skillID, agentID, version, grantedBy, canManage[0], now, tid,
		)
	} else {
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO skill_agent_grants (id, skill_id, agent_id, pinned_version, granted_by, created_at, tenant_id)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)
			 ON CONFLICT (skill_id, agent_id) DO UPDATE SET
			    pinned_version = EXCLUDED.pinned_version,
			    granted_by = EXCLUDED.granted_by`,
			store.GenNewID(), skillID, agentID, version, grantedBy, now, tid,
		)
	}
	if err != nil {
		return err
	}

	// Auto-promote: private → internal (so ListAccessible query includes it for granted agents)
	_, err = s.db.ExecContext(ctx,
		`UPDATE skills
		 SET visibility = 'internal', updated_at = NOW()
		 WHERE id = $1 AND visibility = 'private' AND (is_system = true OR tenant_id = $2)`,
		skillID, tid)
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
	tid := tenantIDForInsert(ctx)
	if err := s.verifySkillInGrantScope(ctx, skillID, tid); err != nil {
		return err
	}
	tClause, tArgs, _, err := scopeClause(ctx, 3)
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
		 WHERE id = $1 AND visibility = 'internal' AND (is_system = true OR tenant_id = $2)
		   AND NOT EXISTS (SELECT 1 FROM skill_agent_grants WHERE skill_id = $1)
		   AND NOT EXISTS (SELECT 1 FROM skill_user_grants WHERE skill_id = $1)`,
		skillID, tid)
	if err != nil {
		slog.Warn("skill_grants: failed to auto-demote visibility", "skill_id", skillID, "error", err)
	}

	s.BumpVersion()
	return nil
}

func (s *PGSkillStore) verifySkillGrantScope(ctx context.Context, skillID, agentID, tenantID uuid.UUID) error {
	if err := s.verifySkillInGrantScope(ctx, skillID, tenantID); err != nil {
		return err
	}

	var agentTenantID uuid.UUID
	if err := s.db.QueryRowContext(ctx,
		"SELECT tenant_id FROM agents WHERE id = $1", agentID,
	).Scan(&agentTenantID); err != nil {
		return fmt.Errorf("agent not found")
	}
	if agentTenantID != tenantID {
		return fmt.Errorf("agent not found")
	}
	return nil
}

func (s *PGSkillStore) verifySkillInGrantScope(ctx context.Context, skillID, tenantID uuid.UUID) error {
	var skillTenantID uuid.UUID
	var isSystem bool
	if err := s.db.QueryRowContext(ctx,
		"SELECT tenant_id, is_system FROM skills WHERE id = $1", skillID,
	).Scan(&skillTenantID, &isSystem); err != nil {
		return fmt.Errorf("skill not found")
	}
	if !isSystem && skillTenantID != tenantID {
		return fmt.Errorf("skill not found")
	}
	return nil
}

// ListAgentGrants returns all skill grants for an agent.
func (s *PGSkillStore) ListAgentGrants(ctx context.Context, agentID uuid.UUID) ([]SkillGrantInfo, error) {
	tClause, tArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	var result []SkillGrantInfo
	err = pkgSqlxDB.SelectContext(ctx, &result,
		"SELECT skill_id, pinned_version, granted_by FROM skill_agent_grants WHERE agent_id = $1"+tClause,
		append([]any{agentID}, tArgs...)...)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ListAgentGrantsForSkill returns all agent grants for one skill.
func (s *PGSkillStore) ListAgentGrantsForSkill(ctx context.Context, skillID uuid.UUID) ([]store.SkillAgentGrantInfo, error) {
	if err := s.verifySkillInGrantScope(ctx, skillID, tenantIDForInsert(ctx)); err != nil {
		return nil, err
	}
	tClause, tArgs, _, err := scopeClauseAlias(ctx, 2, "sag")
	if err != nil {
		return nil, err
	}
	var result []store.SkillAgentGrantInfo
	err = pkgSqlxDB.SelectContext(ctx, &result,
		`SELECT sag.agent_id, COALESCE(a.agent_key, '') AS agent_key, COALESCE(a.display_name, '') AS display_name,
		        sag.pinned_version, sag.granted_by, sag.can_manage
		   FROM skill_agent_grants sag
		   LEFT JOIN agents a ON a.id = sag.agent_id
		  WHERE sag.skill_id = $1`+tClause+`
		  ORDER BY sag.created_at DESC`,
		append([]any{skillID}, tArgs...)...)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *PGSkillStore) attachSkillAgentMetadata(ctx context.Context, skills []store.SkillInfo) {
	if len(skills) == 0 {
		return
	}
	ids := make([]uuid.UUID, 0, len(skills))
	byID := make(map[uuid.UUID]int, len(skills))
	creatorIDs := make([]uuid.UUID, 0)
	creatorByID := make(map[uuid.UUID][]int)
	creatorKeys := make([]string, 0)
	creatorByKey := make(map[string][]int)
	for i := range skills {
		id, err := uuid.Parse(skills[i].ID)
		if err != nil {
			continue
		}
		ids = append(ids, id)
		byID[id] = i
		if ref := skills[i].CreatorAgent; ref != nil {
			skills[i].CreatorAgent = nil
			if ref.ID != "" {
				if agentID, err := uuid.Parse(ref.ID); err == nil {
					if _, exists := creatorByID[agentID]; !exists {
						creatorIDs = append(creatorIDs, agentID)
					}
					creatorByID[agentID] = append(creatorByID[agentID], i)
				}
			}
			if ref.AgentKey != "" {
				if _, exists := creatorByKey[ref.AgentKey]; !exists {
					creatorKeys = append(creatorKeys, ref.AgentKey)
				}
				creatorByKey[ref.AgentKey] = append(creatorByKey[ref.AgentKey], i)
			}
		}
	}
	s.attachVerifiedCreatorAgents(ctx, skills, creatorIDs, creatorByID, creatorKeys, creatorByKey)
	if len(ids) == 0 {
		return
	}
	tClause, tArgs, _, err := scopeClauseAlias(ctx, 2, "sag")
	if err != nil {
		return
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT sag.skill_id, sag.agent_id, COALESCE(a.agent_key, ''), COALESCE(a.display_name, '')
		   FROM skill_agent_grants sag
		   LEFT JOIN agents a ON a.id = sag.agent_id
		  WHERE sag.skill_id = ANY($1) AND sag.can_manage = true`+tClause+`
		  ORDER BY sag.created_at DESC`,
		append([]any{pq.Array(ids)}, tArgs...)...)
	if err != nil {
		slog.Warn("skill_grants: failed to attach manager agents", "error", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var skillID, agentID uuid.UUID
		var agentKey, displayName string
		if err := rows.Scan(&skillID, &agentID, &agentKey, &displayName); err != nil {
			continue
		}
		i, ok := byID[skillID]
		if !ok {
			continue
		}
		skills[i].ManagerAgents = append(skills[i].ManagerAgents, store.SkillAgentRef{
			ID:          agentID.String(),
			AgentKey:    agentKey,
			DisplayName: displayName,
		})
	}
}

func (s *PGSkillStore) attachVerifiedCreatorAgents(
	ctx context.Context,
	skills []store.SkillInfo,
	creatorIDs []uuid.UUID,
	creatorByID map[uuid.UUID][]int,
	creatorKeys []string,
	creatorByKey map[string][]int,
) {
	if len(creatorIDs) == 0 && len(creatorKeys) == 0 {
		return
	}
	tClause, tArgs, _, err := scopeClauseAlias(ctx, 3, "a")
	if err != nil {
		return
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT a.id, COALESCE(a.agent_key, ''), COALESCE(a.display_name, '')
		   FROM agents a
		  WHERE a.deleted_at IS NULL AND (a.id = ANY($1) OR a.agent_key = ANY($2))`+tClause,
		append([]any{pq.Array(creatorIDs), pq.Array(creatorKeys)}, tArgs...)...)
	if err != nil {
		slog.Warn("skill_grants: failed to resolve creator agents", "error", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var agentID uuid.UUID
		var agentKey, displayName string
		if err := rows.Scan(&agentID, &agentKey, &displayName); err != nil {
			continue
		}
		ref := store.SkillAgentRef{
			ID:          agentID.String(),
			AgentKey:    agentKey,
			DisplayName: displayName,
		}
		for _, i := range creatorByID[agentID] {
			skills[i].CreatorAgent = &ref
		}
		for _, i := range creatorByKey[agentKey] {
			skills[i].CreatorAgent = &ref
		}
	}
}

// AgentCanManageSkill reports whether an agent has explicit edit/delete rights for a skill.
func (s *PGSkillStore) AgentCanManageSkill(ctx context.Context, skillID, agentID uuid.UUID) (bool, error) {
	if err := s.verifySkillInGrantScope(ctx, skillID, tenantIDForInsert(ctx)); err != nil {
		return false, err
	}
	tClause, tArgs, _, err := scopeClause(ctx, 3)
	if err != nil {
		return false, err
	}
	var canManage bool
	err = s.db.QueryRowContext(ctx,
		"SELECT can_manage FROM skill_agent_grants WHERE skill_id = $1 AND agent_id = $2"+tClause,
		append([]any{skillID, agentID}, tArgs...)...).Scan(&canManage)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return canManage, nil
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
	tClause, tArgs, _, err := scopeClause(ctx, 3)
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
//
// To preserve visibility across the ACTOR-vs-SCOPE split (#915), the query
// matches owner_id and user grant rows against BOTH the caller-supplied
// userID (scope identity, legacy rows) and the actor identity from ctx
// (individual sender, new rows). In DM contexts these are identical and
// the OR clause collapses; in group chats they diverge and the OR ensures
// a publisher can still see their own private skill.
func (s *PGSkillStore) ListAccessible(ctx context.Context, agentID uuid.UUID, userID string) ([]store.SkillInfo, error) {
	actorID := store.ActorIDFromContext(ctx)
	if actorID == "" {
		actorID = userID
	}
	// tenant filter: system skills bypass it, tenant-owned skills are filtered
	// (bumped from $3 to $4 to accommodate the new actorID bind at $3).
	tc, tcArgs, _, err := scopeClause(ctx, 4)
	if err != nil {
		return nil, err
	}
	tenantCond := ""
	if tc != "" {
		// tc is " AND tenant_id = $4"; we need it as an OR condition inside the WHERE
		tenantCond = fmt.Sprintf(" AND (s.is_system = true OR s.tenant_id = $%d)", 4)
		_ = tc // tcArgs carries the value
	}
	// LEFT JOIN skill_tenant_configs to exclude per-tenant disabled skills.
	// stc.enabled = false → skill explicitly disabled for this tenant.
	stcJoin := ""
	stcFilter := ""
	if len(tcArgs) > 0 {
		stcJoin = fmt.Sprintf(" LEFT JOIN skill_tenant_configs stc ON s.id = stc.skill_id AND stc.tenant_id = $%d", 4)
		stcFilter = " AND (stc.enabled IS NULL OR stc.enabled = true)"
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT s.name, s.slug, s.description, s.version, s.file_path FROM skills s
		LEFT JOIN skill_agent_grants sag ON s.id = sag.skill_id AND sag.agent_id = $1
		LEFT JOIN skill_user_grants sug ON s.id = sug.skill_id AND (sug.user_id = $2 OR sug.user_id = $3)`+stcJoin+`
		WHERE s.status = 'active'`+tenantCond+stcFilter+` AND (
			s.is_system = true
			OR s.visibility = 'public'
			OR (s.visibility = 'private' AND (s.owner_id = $2 OR s.owner_id = $3))
			OR (s.visibility = 'internal' AND (sag.id IS NOT NULL OR sug.id IS NOT NULL))
		)
		ORDER BY s.name`, append([]any{agentID, userID, actorID}, tcArgs...)...)
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
	SkillID       uuid.UUID `json:"skill_id" db:"skill_id"`
	PinnedVersion int       `json:"pinned_version" db:"pinned_version"`
	GrantedBy     string    `json:"granted_by" db:"granted_by"`
}

// ListWithGrantStatus returns all active skills with grant status for a specific agent.
func (s *PGSkillStore) ListWithGrantStatus(ctx context.Context, agentID uuid.UUID) ([]store.SkillWithGrantStatus, error) {
	tc, tcArgs, _, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	tenantCond := ""
	grantTenantCond := ""
	if tc != "" {
		tenantCond = fmt.Sprintf(" AND (s.is_system = true OR s.tenant_id = $%d)", 2)
		grantTenantCond = fmt.Sprintf(" AND sag.tenant_id = $%d", 2)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT s.id, s.name, s.slug, COALESCE(s.description, ''), s.visibility, s.version,
		        (sag.id IS NOT NULL) AS granted,
		        COALESCE(sag.can_manage, false) AS can_manage,
		        sag.pinned_version,
		        s.is_system
		 FROM skills s
		 LEFT JOIN skill_agent_grants sag ON s.id = sag.skill_id AND sag.agent_id = $1`+grantTenantCond+`
		 WHERE s.status = 'active'`+tenantCond+`
		 ORDER BY s.name`, append([]any{agentID}, tcArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.SkillWithGrantStatus
	for rows.Next() {
		var r store.SkillWithGrantStatus
		if err := rows.Scan(&r.ID, &r.Name, &r.Slug, &r.Description, &r.Visibility, &r.Version, &r.Granted, &r.CanManage, &r.PinnedVer, &r.IsSystem); err != nil {
			slog.Warn("skill_grants: scan error in ListWithGrantStatus", "error", err)
			continue
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
