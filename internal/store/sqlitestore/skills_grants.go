//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SkillGrantInfo is a simplified grant record for API responses.
type SkillGrantInfo struct {
	SkillID       uuid.UUID `json:"skill_id" db:"skill_id"`
	PinnedVersion int       `json:"pinned_version" db:"pinned_version"`
	GrantedBy     string    `json:"granted_by" db:"granted_by"`
}

// GrantToAgent grants a skill to an agent with version pinning.
func (s *SQLiteSkillStore) GrantToAgent(ctx context.Context, skillID, agentID uuid.UUID, version int, grantedBy string, canManage ...bool) error {
	if err := store.ValidateUserID(grantedBy); err != nil {
		return err
	}
	// Upsert grant.
	id := store.GenNewID()
	now := time.Now().UTC()
	tid := tenantIDForInsert(ctx)
	if err := s.verifySkillGrantScope(ctx, skillID, agentID, tid); err != nil {
		return err
	}
	var err error
	if len(canManage) > 0 {
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO skill_agent_grants (id, skill_id, agent_id, pinned_version, granted_by, can_manage, created_at, tenant_id)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT (skill_id, agent_id) DO UPDATE SET
			    pinned_version = excluded.pinned_version,
			    granted_by = excluded.granted_by,
			    can_manage = excluded.can_manage`,
			id, skillID, agentID, version, grantedBy, canManage[0], now, tid,
		)
	} else {
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO skill_agent_grants (id, skill_id, agent_id, pinned_version, granted_by, created_at, tenant_id)
			 VALUES (?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT (skill_id, agent_id) DO UPDATE SET
			    pinned_version = excluded.pinned_version,
			    granted_by = excluded.granted_by`,
			id, skillID, agentID, version, grantedBy, now, tid,
		)
	}
	if err != nil {
		return err
	}

	// Auto-promote: private → internal.
	_, err = s.db.ExecContext(ctx,
		`UPDATE skills
		 SET visibility = 'internal', updated_at = ?
		 WHERE id = ? AND visibility = 'private' AND (is_system = 1 OR tenant_id = ?)`,
		time.Now().UTC(), skillID, tid)
	if err != nil {
		slog.Warn("skill_grants: failed to auto-promote visibility", "skill_id", skillID, "error", err)
	}

	s.BumpVersion()
	return nil
}

// RevokeFromAgent revokes a skill grant from an agent.
func (s *SQLiteSkillStore) RevokeFromAgent(ctx context.Context, skillID, agentID uuid.UUID) error {
	tid := tenantIDForInsert(ctx)
	if err := s.verifySkillInGrantScope(ctx, skillID, tid); err != nil {
		return err
	}
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		"DELETE FROM skill_agent_grants WHERE skill_id = ? AND agent_id = ?"+tClause,
		append([]any{skillID, agentID}, tArgs...)...)
	if err != nil {
		return err
	}

	// Auto-demote: internal → private when no grants remain.
	_, err = s.db.ExecContext(ctx,
		`UPDATE skills SET visibility = 'private', updated_at = ?
		 WHERE id = ? AND visibility = 'internal' AND (is_system = 1 OR tenant_id = ?)
		   AND NOT EXISTS (SELECT 1 FROM skill_agent_grants WHERE skill_id = ?)
		   AND NOT EXISTS (SELECT 1 FROM skill_user_grants WHERE skill_id = ?)`,
		time.Now().UTC(), skillID, tid, skillID, skillID)
	if err != nil {
		slog.Warn("skill_grants: failed to auto-demote visibility", "skill_id", skillID, "error", err)
	}

	s.BumpVersion()
	return nil
}

func (s *SQLiteSkillStore) verifySkillGrantScope(ctx context.Context, skillID, agentID, tenantID uuid.UUID) error {
	if err := s.verifySkillInGrantScope(ctx, skillID, tenantID); err != nil {
		return err
	}

	var agentTenantID uuid.UUID
	if err := s.db.QueryRowContext(ctx,
		"SELECT tenant_id FROM agents WHERE id = ?", agentID,
	).Scan(&agentTenantID); err != nil {
		return fmt.Errorf("agent not found")
	}
	if agentTenantID != tenantID {
		return fmt.Errorf("agent not found")
	}
	return nil
}

func (s *SQLiteSkillStore) verifySkillInGrantScope(ctx context.Context, skillID, tenantID uuid.UUID) error {
	var skillTenantID uuid.UUID
	var isSystem bool
	if err := s.db.QueryRowContext(ctx,
		"SELECT tenant_id, is_system FROM skills WHERE id = ?", skillID,
	).Scan(&skillTenantID, &isSystem); err != nil {
		return fmt.Errorf("skill not found")
	}
	if !isSystem && skillTenantID != tenantID {
		return fmt.Errorf("skill not found")
	}
	return nil
}

// ListAgentGrants returns all skill grants for an agent.
func (s *SQLiteSkillStore) ListAgentGrants(ctx context.Context, agentID uuid.UUID) ([]SkillGrantInfo, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT skill_id, pinned_version, granted_by FROM skill_agent_grants WHERE agent_id = ?"+tClause,
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

// ListAgentGrantsForSkill returns all agent grants for one skill.
func (s *SQLiteSkillStore) ListAgentGrantsForSkill(ctx context.Context, skillID uuid.UUID) ([]store.SkillAgentGrantInfo, error) {
	if err := s.verifySkillInGrantScope(ctx, skillID, tenantIDForInsert(ctx)); err != nil {
		return nil, err
	}
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT sag.agent_id, COALESCE(a.agent_key, ''), COALESCE(a.display_name, ''),
		        sag.pinned_version, sag.granted_by, sag.can_manage
		   FROM skill_agent_grants sag
		   LEFT JOIN agents a ON a.id = sag.agent_id
		  WHERE sag.skill_id = ?`+strings.ReplaceAll(tClause, "tenant_id", "sag.tenant_id")+`
		  ORDER BY sag.created_at DESC`,
		append([]any{skillID}, tArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.SkillAgentGrantInfo
	for rows.Next() {
		var g store.SkillAgentGrantInfo
		if err := rows.Scan(&g.AgentID, &g.AgentKey, &g.DisplayName, &g.PinnedVersion, &g.GrantedBy, &g.CanManage); err != nil {
			slog.Warn("skill_grants: scan error in ListAgentGrantsForSkill", "error", err)
			continue
		}
		result = append(result, g)
	}
	return result, rows.Err()
}

func (s *SQLiteSkillStore) attachSkillAgentMetadata(ctx context.Context, skills []store.SkillInfo) {
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
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return
	}
	args = append(args, tArgs...)
	rows, err := s.db.QueryContext(ctx,
		`SELECT sag.skill_id, sag.agent_id, COALESCE(a.agent_key, ''), COALESCE(a.display_name, '')
		   FROM skill_agent_grants sag
		   LEFT JOIN agents a ON a.id = sag.agent_id
		  WHERE sag.skill_id IN (`+strings.Join(placeholders, ",")+`) AND sag.can_manage = 1`+strings.ReplaceAll(tClause, "tenant_id", "sag.tenant_id")+`
		  ORDER BY sag.created_at DESC`,
		args...)
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

func (s *SQLiteSkillStore) attachVerifiedCreatorAgents(
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
	clauses := make([]string, 0, 2)
	args := make([]any, 0, len(creatorIDs)+len(creatorKeys)+1)
	if len(creatorIDs) > 0 {
		placeholders := make([]string, len(creatorIDs))
		for i, id := range creatorIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		clauses = append(clauses, "a.id IN ("+strings.Join(placeholders, ",")+")")
	}
	if len(creatorKeys) > 0 {
		placeholders := make([]string, len(creatorKeys))
		for i, key := range creatorKeys {
			placeholders[i] = "?"
			args = append(args, key)
		}
		clauses = append(clauses, "a.agent_key IN ("+strings.Join(placeholders, ",")+")")
	}
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return
	}
	args = append(args, tArgs...)
	rows, err := s.db.QueryContext(ctx,
		`SELECT a.id, COALESCE(a.agent_key, ''), COALESCE(a.display_name, '')
		   FROM agents a
		  WHERE a.deleted_at IS NULL AND (`+strings.Join(clauses, " OR ")+`)`+strings.ReplaceAll(tClause, "tenant_id", "a.tenant_id"),
		args...)
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
func (s *SQLiteSkillStore) AgentCanManageSkill(ctx context.Context, skillID, agentID uuid.UUID) (bool, error) {
	if err := s.verifySkillInGrantScope(ctx, skillID, tenantIDForInsert(ctx)); err != nil {
		return false, err
	}
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return false, err
	}
	var canManage bool
	err = s.db.QueryRowContext(ctx,
		"SELECT can_manage FROM skill_agent_grants WHERE skill_id = ? AND agent_id = ?"+tClause,
		append([]any{skillID, agentID}, tArgs...)...).Scan(&canManage)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return canManage, nil
}

// GrantToUser grants a skill to a user.
func (s *SQLiteSkillStore) GrantToUser(ctx context.Context, skillID uuid.UUID, userID, grantedBy string) error {
	if err := store.ValidateUserID(userID); err != nil {
		return err
	}
	if err := store.ValidateUserID(grantedBy); err != nil {
		return err
	}
	id := store.GenNewID()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO skill_user_grants (id, skill_id, user_id, granted_by, created_at, tenant_id)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (skill_id, user_id) DO NOTHING`,
		id, skillID, userID, grantedBy, time.Now().UTC(), tenantIDForInsert(ctx),
	)
	return err
}

// RevokeFromUser revokes a skill grant from a user.
func (s *SQLiteSkillStore) RevokeFromUser(ctx context.Context, skillID uuid.UUID, userID string) error {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		"DELETE FROM skill_user_grants WHERE skill_id = ? AND user_id = ?"+tClause,
		append([]any{skillID, userID}, tArgs...)...)
	return err
}

// ListAccessible returns skills accessible to a given agent+user combination.
// See PGSkillStore.ListAccessible for the ACTOR-vs-SCOPE dual-match rationale.
func (s *SQLiteSkillStore) ListAccessible(ctx context.Context, agentID uuid.UUID, userID string) ([]store.SkillInfo, error) {
	actorID := store.ActorIDFromContext(ctx)
	if actorID == "" {
		actorID = userID
	}
	tClause, tArgs, err := scopeClauseAlias(ctx, "s")
	if err != nil {
		return nil, err
	}
	tenantCond := ""
	stcJoin := ""
	stcFilter := ""
	if len(tArgs) > 0 {
		tenantCond = " AND (s.is_system = 1 OR s.tenant_id = ?)"
		stcJoin = " LEFT JOIN skill_tenant_configs stc ON s.id = stc.skill_id AND stc.tenant_id = ?"
		stcFilter = " AND (stc.enabled IS NULL OR stc.enabled = 1)"
	}

	// Positional args: agentID, userID, actorID, [tenantID x2 if tenant-scoped], userID, actorID (private-owner clause)
	queryArgs := []any{agentID, userID, actorID}
	if len(tArgs) > 0 {
		queryArgs = append(queryArgs, tArgs...) // tenant cond
		queryArgs = append(queryArgs, tArgs...) // stc join
	}
	// Remove tClause (aliased scope) — we handle it manually above.
	_ = tClause

	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT s.name, s.slug, s.description, s.version, s.file_path FROM skills s
		LEFT JOIN skill_agent_grants sag ON s.id = sag.skill_id AND sag.agent_id = ?
		LEFT JOIN skill_user_grants sug ON s.id = sug.skill_id AND (sug.user_id = ? OR sug.user_id = ?)`+stcJoin+`
		WHERE s.status = 'active'`+tenantCond+stcFilter+` AND (
			s.is_system = 1
			OR s.visibility = 'public'
			OR (s.visibility = 'private' AND (s.owner_id = ? OR s.owner_id = ?))
			OR (s.visibility = 'internal' AND (sag.id IS NOT NULL OR sug.id IS NOT NULL))
		)
		ORDER BY s.name`,
		append(queryArgs, userID, actorID)...,
	)
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

// ListWithGrantStatus returns all active skills with grant status for a specific agent.
func (s *SQLiteSkillStore) ListWithGrantStatus(ctx context.Context, agentID uuid.UUID) ([]store.SkillWithGrantStatus, error) {
	tClause, tArgs, err := scopeClauseAlias(ctx, "s")
	if err != nil {
		return nil, err
	}
	tenantCond := ""
	grantTenantCond := ""
	if len(tArgs) > 0 {
		tenantCond = " AND (s.is_system = 1 OR s.tenant_id = ?)"
		grantTenantCond = " AND sag.tenant_id = ?"
	}
	_ = tClause

	queryArgs := []any{agentID}
	if len(tArgs) > 0 {
		queryArgs = append(queryArgs, tArgs...)
		queryArgs = append(queryArgs, tArgs...)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT s.id, s.name, s.slug, COALESCE(s.description, ''), s.visibility, s.version,
		        (sag.id IS NOT NULL) AS granted,
		        COALESCE(sag.can_manage, 0) AS can_manage,
		        sag.pinned_version,
		        s.is_system
		 FROM skills s
		 LEFT JOIN skill_agent_grants sag ON s.id = sag.skill_id AND sag.agent_id = ?`+grantTenantCond+`
		 WHERE s.status = 'active'`+tenantCond+`
		 ORDER BY s.name`, queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.SkillWithGrantStatus
	for rows.Next() {
		var r store.SkillWithGrantStatus
		if err := rows.Scan(&r.ID, &r.Name, &r.Slug, &r.Description, &r.Visibility,
			&r.Version, &r.Granted, &r.CanManage, &r.PinnedVer, &r.IsSystem); err != nil {
			slog.Warn("skill_grants: scan error in ListWithGrantStatus", "error", err)
			continue
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
