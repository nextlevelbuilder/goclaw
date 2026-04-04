//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteTeamStore implements store.TeamStore backed by SQLite.
type SQLiteTeamStore struct {
	db *sql.DB
}

// SetEmbeddingProvider is a no-op for SQLite — vector search not supported.
func (s *SQLiteTeamStore) SetEmbeddingProvider(_ store.EmbeddingProvider) {}

func NewSQLiteTeamStore(db *sql.DB) *SQLiteTeamStore {
	return &SQLiteTeamStore{db: db}
}

const teamSelectCols = `id, name, lead_agent_id, description, status, settings, created_by, created_at, updated_at`

// ============================================================
// Team CRUD
// ============================================================

func (s *SQLiteTeamStore) CreateTeam(ctx context.Context, team *store.TeamData) error {
	if team.ID == uuid.Nil {
		team.ID = store.GenNewID()
	}
	now := time.Now()
	team.CreatedAt = now
	team.UpdatedAt = now

	settings := team.Settings
	if len(settings) == 0 {
		settings = json.RawMessage(`{}`)
	}

	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_teams (id, name, lead_agent_id, description, status, settings, created_by, created_at, updated_at, tenant_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		team.ID, team.Name, team.LeadAgentID, team.Description,
		team.Status, settings, team.CreatedBy, now, now, tenantID,
	)
	return err
}

func (s *SQLiteTeamStore) GetTeam(ctx context.Context, teamID uuid.UUID) (*store.TeamData, error) {
	if store.IsCrossTenant(ctx) {
		row := s.db.QueryRowContext(ctx,
			`SELECT `+teamSelectCols+` FROM agent_teams WHERE id = ?`, teamID)
		return scanTeamRow(row)
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+teamSelectCols+` FROM agent_teams WHERE id = ? AND tenant_id = ?`, teamID, tenantID)
	d, err := scanTeamRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return d, err
}

func (s *SQLiteTeamStore) UpdateTeam(ctx context.Context, teamID uuid.UUID, updates map[string]any) error {
	if store.IsCrossTenant(ctx) {
		return execMapUpdate(ctx, s.db, "agent_teams", teamID, updates)
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required for update")
	}
	return execMapUpdateWhereTenant(ctx, s.db, "agent_teams", updates, teamID, tid)
}

func (s *SQLiteTeamStore) DeleteTeam(ctx context.Context, teamID uuid.UUID) error {
	if store.IsCrossTenant(ctx) {
		_, err := s.db.ExecContext(ctx, `DELETE FROM agent_teams WHERE id = ?`, teamID)
		return err
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required for delete")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM agent_teams WHERE id = ? AND tenant_id = ?`, teamID, tid)
	return err
}

func (s *SQLiteTeamStore) ListTeams(ctx context.Context) ([]store.TeamData, error) {
	var tenantFilter string
	var queryArgs []any
	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return nil, nil
		}
		tenantFilter = " WHERE t.tenant_id = ?"
		queryArgs = append(queryArgs, tenantID)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT t.id, t.name, t.lead_agent_id, t.description, t.status, t.settings, t.created_by, t.created_at, t.updated_at,
		 COALESCE(a.agent_key, '') AS lead_agent_key,
		 COALESCE(a.display_name, '') AS lead_display_name
		 FROM agent_teams t
		 LEFT JOIN agents a ON a.id = t.lead_agent_id`+tenantFilter+`
		 ORDER BY t.created_at`, queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var teams []store.TeamData
	teamIndex := map[uuid.UUID]int{}
	for rows.Next() {
		var d store.TeamData
		var desc sql.NullString
		createdAt, updatedAt := scanTimePair()
		if err := rows.Scan(
			&d.ID, &d.Name, &d.LeadAgentID, &desc, &d.Status,
			&d.Settings, &d.CreatedBy, createdAt, updatedAt,
			&d.LeadAgentKey, &d.LeadDisplayName,
		); err != nil {
			return nil, err
		}
		d.CreatedAt = createdAt.Time
		d.UpdatedAt = updatedAt.Time
		if desc.Valid {
			d.Description = desc.String
		}
		teamIndex[d.ID] = len(teams)
		teams = append(teams, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(teams) > 0 {
		mRows, err := s.db.QueryContext(ctx,
			`SELECT m.team_id, m.agent_id, m.role, m.joined_at,
			 COALESCE(a.agent_key, '') AS agent_key,
			 COALESCE(a.display_name, '') AS display_name,
			 COALESCE(a.frontmatter, '') AS frontmatter,
			 COALESCE(json_extract(a.other_config, '$.emoji'), '') AS emoji
			 FROM agent_team_members m
			 JOIN agents a ON a.id = m.agent_id
			 WHERE a.status = 'active'
			 ORDER BY m.joined_at`)
		if err != nil {
			return nil, err
		}
		defer mRows.Close()

		for mRows.Next() {
			var m store.TeamMemberData
			var joinedAt sqliteTime
			if err := mRows.Scan(&m.TeamID, &m.AgentID, &m.Role, &joinedAt, &m.AgentKey, &m.DisplayName, &m.Frontmatter, &m.Emoji); err != nil {
				return nil, err
			}
			m.JoinedAt = joinedAt.Time
			if idx, ok := teamIndex[m.TeamID]; ok {
				teams[idx].Members = append(teams[idx].Members, m)
				teams[idx].MemberCount++
			}
		}
		if err := mRows.Err(); err != nil {
			return nil, err
		}
	}

	return teams, nil
}

// ============================================================
// Members
// ============================================================

func (s *SQLiteTeamStore) AddMember(ctx context.Context, teamID, agentID uuid.UUID, role string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_team_members (team_id, agent_id, role, joined_at, tenant_id)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (team_id, agent_id) DO UPDATE SET role = excluded.role`,
		teamID, agentID, role, time.Now(), tenantIDForInsert(ctx),
	)
	return err
}

func (s *SQLiteTeamStore) RemoveMember(ctx context.Context, teamID, agentID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM agent_team_members WHERE team_id = ? AND agent_id = ?`,
		teamID, agentID,
	)
	return err
}

func (s *SQLiteTeamStore) ListMembers(ctx context.Context, teamID uuid.UUID) ([]store.TeamMemberData, error) {
	q := `SELECT m.team_id, m.agent_id, m.role, m.joined_at,
		 COALESCE(a.agent_key, '') AS agent_key,
		 COALESCE(a.display_name, '') AS display_name,
		 COALESCE(a.frontmatter, '') AS frontmatter,
		 COALESCE(json_extract(a.other_config, '$.emoji'), '') AS emoji
		 FROM agent_team_members m
		 JOIN agents a ON a.id = m.agent_id
		 JOIN agent_teams at2 ON at2.id = m.team_id
		 WHERE m.team_id = ? AND a.status = 'active'`
	args := []any{teamID}

	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid != uuid.Nil {
			q += " AND at2.tenant_id = ?"
			args = append(args, tid)
		}
	}
	q += ` ORDER BY m.joined_at`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []store.TeamMemberData
	for rows.Next() {
		var d store.TeamMemberData
		var joinedAt sqliteTime
		if err := rows.Scan(
			&d.TeamID, &d.AgentID, &d.Role, &joinedAt,
			&d.AgentKey, &d.DisplayName, &d.Frontmatter, &d.Emoji,
		); err != nil {
			return nil, err
		}
		d.JoinedAt = joinedAt.Time
		members = append(members, d)
	}
	return members, rows.Err()
}

func (s *SQLiteTeamStore) ListIdleMembers(ctx context.Context, teamID uuid.UUID) ([]store.TeamMemberData, error) {
	q := `SELECT m.team_id, m.agent_id, m.role, m.joined_at,
		 COALESCE(a.agent_key, '') AS agent_key,
		 COALESCE(a.display_name, '') AS display_name,
		 COALESCE(a.frontmatter, '') AS frontmatter,
		 COALESCE(json_extract(a.other_config, '$.emoji'), '') AS emoji
		 FROM agent_team_members m
		 JOIN agents a ON a.id = m.agent_id
		 JOIN agent_teams at2 ON at2.id = m.team_id
		 WHERE m.team_id = ? AND a.status = 'active' AND m.role != ?
		   AND NOT EXISTS (
		     SELECT 1 FROM team_tasks tt
		     WHERE tt.owner_agent_id = m.agent_id AND tt.team_id = ? AND tt.status = ?
		   )`
	args := []any{teamID, store.TeamRoleLead, teamID, store.TeamTaskStatusInProgress}

	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid != uuid.Nil {
			q += " AND at2.tenant_id = ?"
			args = append(args, tid)
		}
	}
	q += ` ORDER BY m.joined_at`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []store.TeamMemberData
	for rows.Next() {
		var d store.TeamMemberData
		var joinedAt sqliteTime
		if err := rows.Scan(
			&d.TeamID, &d.AgentID, &d.Role, &joinedAt,
			&d.AgentKey, &d.DisplayName, &d.Frontmatter, &d.Emoji,
		); err != nil {
			return nil, err
		}
		d.JoinedAt = joinedAt.Time
		members = append(members, d)
	}
	return members, rows.Err()
}

func (s *SQLiteTeamStore) GetTeamForAgent(ctx context.Context, agentID uuid.UUID) (*store.TeamData, error) {
	q := `SELECT t.id, t.name, t.lead_agent_id, t.description, t.status, t.settings, t.created_by, t.created_at, t.updated_at
		 FROM agent_teams t
		 WHERE (
		   t.lead_agent_id = ?
		   OR EXISTS (SELECT 1 FROM agent_team_members m WHERE m.team_id = t.id AND m.agent_id = ?)
		 ) AND t.status = ?`
	args := []any{agentID, agentID, store.TeamStatusActive}

	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid != uuid.Nil {
			q += " AND t.tenant_id = ?"
			args = append(args, tid)
		}
	}
	q += ` ORDER BY (t.lead_agent_id = ?) DESC LIMIT 1`
	args = append(args, agentID)

	row := s.db.QueryRowContext(ctx, q, args...)
	d, err := scanTeamRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return d, err
}

func (s *SQLiteTeamStore) KnownUserIDs(ctx context.Context, teamID uuid.UUID, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT DISTINCT s.user_id
		 FROM sessions s
		 JOIN agent_team_members m ON m.agent_id = s.agent_id
		 JOIN agent_teams at2 ON at2.id = m.team_id
		 WHERE m.team_id = ? AND s.user_id != ''`
	args := []any{teamID}

	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid != uuid.Nil {
			q += " AND at2.tenant_id = ?"
			args = append(args, tid)
		}
	}
	q += ` ORDER BY s.user_id LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		users = append(users, uid)
	}
	return users, rows.Err()
}

// ============================================================
// Team user grants
// ============================================================

func (s *SQLiteTeamStore) GrantTeamAccess(ctx context.Context, teamID uuid.UUID, userID, role, grantedBy string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO team_user_grants (id, team_id, user_id, role, granted_by, created_at, tenant_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (team_id, user_id) DO UPDATE SET role = excluded.role, granted_by = excluded.granted_by`,
		store.GenNewID(), teamID, userID, role, grantedBy, time.Now(), tenantIDForInsert(ctx),
	)
	return err
}

func (s *SQLiteTeamStore) RevokeTeamAccess(ctx context.Context, teamID uuid.UUID, userID string) error {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`DELETE FROM team_user_grants WHERE team_id = ? AND user_id = ?`+tClause,
		append([]any{teamID, userID}, tArgs...)...)
	return err
}

func (s *SQLiteTeamStore) ListTeamGrants(ctx context.Context, teamID uuid.UUID) ([]store.TeamUserGrant, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, team_id, user_id, role, COALESCE(granted_by, ''), created_at
		 FROM team_user_grants WHERE team_id = ?`+tClause+` ORDER BY created_at DESC`,
		append([]any{teamID}, tArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.TeamUserGrant
	for rows.Next() {
		var g store.TeamUserGrant
		var createdAt sqliteTime
		if err := rows.Scan(&g.ID, &g.TeamID, &g.UserID, &g.Role, &g.GrantedBy, &createdAt); err != nil {
			return nil, err
		}
		g.CreatedAt = createdAt.Time
		result = append(result, g)
	}
	return result, rows.Err()
}

func (s *SQLiteTeamStore) ListUserTeams(ctx context.Context, userID string) ([]store.TeamData, error) {
	baseQuery := `SELECT ` + teamSelectCols + `
		 FROM agent_teams t
		 WHERE t.status = ?
		   AND EXISTS (SELECT 1 FROM team_user_grants g WHERE g.team_id = t.id AND g.user_id = ?)`
	args := []any{store.TeamStatusActive, userID}

	if !store.IsCrossTenant(ctx) {
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			return nil, nil
		}
		baseQuery += ` AND t.tenant_id = ?`
		args = append(args, tenantID)
	}
	baseQuery += ` ORDER BY t.created_at DESC`

	rows, err := s.db.QueryContext(ctx, baseQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var teams []store.TeamData
	for rows.Next() {
		var d store.TeamData
		var desc sql.NullString
		createdAt, updatedAt := scanTimePair()
		if err := rows.Scan(
			&d.ID, &d.Name, &d.LeadAgentID, &desc, &d.Status,
			&d.Settings, &d.CreatedBy, createdAt, updatedAt,
		); err != nil {
			return nil, err
		}
		d.CreatedAt = createdAt.Time
		d.UpdatedAt = updatedAt.Time
		if desc.Valid {
			d.Description = desc.String
		}
		teams = append(teams, d)
	}
	return teams, rows.Err()
}

func (s *SQLiteTeamStore) HasTeamAccess(ctx context.Context, teamID uuid.UUID, userID string) (bool, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return false, err
	}
	var exists bool
	err = s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM team_user_grants WHERE team_id = ? AND user_id = ?`+tClause+`)`,
		append([]any{teamID, userID}, tArgs...)...,
	).Scan(&exists)
	return exists, err
}

// GetTeamUnscoped returns a team by ID without tenant filtering. Server-internal only.
func (s *SQLiteTeamStore) GetTeamUnscoped(ctx context.Context, id uuid.UUID) (*store.TeamData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+teamSelectCols+` FROM agent_teams WHERE id = ?`, id)
	return scanTeamRow(row)
}

// ============================================================
// Scan helpers
// ============================================================

func scanTeamRow(row *sql.Row) (*store.TeamData, error) {
	var d store.TeamData
	var desc sql.NullString
	createdAt, updatedAt := scanTimePair()
	err := row.Scan(
		&d.ID, &d.Name, &d.LeadAgentID, &desc, &d.Status,
		&d.Settings, &d.CreatedBy, createdAt, updatedAt,
	)
	if err != nil {
		return nil, err
	}
	d.CreatedAt = createdAt.Time
	d.UpdatedAt = updatedAt.Time
	if desc.Valid {
		d.Description = desc.String
	}
	return &d, nil
}
