//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"log/slog"
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
func (s *SQLiteSkillStore) GrantToAgent(ctx context.Context, skillID, agentID uuid.UUID, version int, grantedBy string) error {
	if err := store.ValidateUserID(grantedBy); err != nil {
		return err
	}

	// Upsert grant.
	id := store.GenNewID()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO skill_agent_grants (id, skill_id, agent_id, pinned_version, granted_by, created_at, tenant_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (skill_id, agent_id) DO UPDATE SET pinned_version = excluded.pinned_version`,
		id, skillID, agentID, version, grantedBy, time.Now().UTC(), tenantIDForInsert(ctx),
	)
	if err != nil {
		return err
	}

	// Auto-promote: private → internal.
	_, err = s.db.ExecContext(ctx,
		`UPDATE skills SET visibility = 'internal', updated_at = ? WHERE id = ? AND visibility = 'private'`,
		time.Now().UTC(), skillID)
	if err != nil {
		slog.Warn("skill_grants: failed to auto-promote visibility", "skill_id", skillID, "error", err)
	}

	s.BumpVersion()
	return nil
}

// RevokeFromAgent revokes a skill grant from an agent.
func (s *SQLiteSkillStore) RevokeFromAgent(ctx context.Context, skillID, agentID uuid.UUID) error {
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
		 WHERE id = ? AND visibility = 'internal'
		   AND NOT EXISTS (SELECT 1 FROM skill_agent_grants WHERE skill_id = ?)`,
		time.Now().UTC(), skillID, skillID)
	if err != nil {
		slog.Warn("skill_grants: failed to auto-demote visibility", "skill_id", skillID, "error", err)
	}

	s.BumpVersion()
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
	if len(tArgs) > 0 {
		tenantCond = " AND (s.is_system = 1 OR s.tenant_id = ?)"
	}
	_ = tClause

	queryArgs := []any{agentID}
	if len(tArgs) > 0 {
		queryArgs = append(queryArgs, tArgs...)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT s.id, s.name, s.slug, COALESCE(s.description, ''), s.visibility, s.version,
		        (sag.id IS NOT NULL) AS granted,
		        sag.pinned_version,
		        s.is_system
		 FROM skills s
		 LEFT JOIN skill_agent_grants sag ON s.id = sag.skill_id AND sag.agent_id = ?
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
			&r.Version, &r.Granted, &r.PinnedVer, &r.IsSystem); err != nil {
			slog.Warn("skill_grants: scan error in ListWithGrantStatus", "error", err)
			continue
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
