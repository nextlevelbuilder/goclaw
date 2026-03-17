package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGTeamStore implements store.TeamStore backed by Postgres.
type PGTeamStore struct {
	db *sql.DB
}

func NewPGTeamStore(db *sql.DB) *PGTeamStore {
	return &PGTeamStore{db: db}
}

// --- Column constants ---

const teamSelectCols = `id, name, lead_agent_id, description, status, settings, created_by, created_at, updated_at`

// ============================================================
// Team CRUD
// ============================================================

func (s *PGTeamStore) CreateTeam(ctx context.Context, team *store.TeamData) error {
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

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_teams (id, name, lead_agent_id, description, status, settings, created_by, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		team.ID, team.Name, team.LeadAgentID, team.Description,
		team.Status, settings, team.CreatedBy, now, now,
	)
	return err
}

func (s *PGTeamStore) GetTeam(ctx context.Context, teamID uuid.UUID) (*store.TeamData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+teamSelectCols+` FROM agent_teams WHERE id = $1`, teamID)
	return scanTeamRow(row)
}

func (s *PGTeamStore) UpdateTeam(ctx context.Context, teamID uuid.UUID, updates map[string]any) error {
	return execMapUpdate(ctx, s.db, "agent_teams", teamID, updates)
}

func (s *PGTeamStore) DeleteTeam(ctx context.Context, teamID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM agent_teams WHERE id = $1`, teamID)
	return err
}

func (s *PGTeamStore) ListTeams(ctx context.Context) ([]store.TeamData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.id, t.name, t.lead_agent_id, t.description, t.status, t.settings, t.created_by, t.created_at, t.updated_at,
		 COALESCE(a.agent_key, '') AS lead_agent_key,
		 COALESCE(a.display_name, '') AS lead_display_name
		 FROM agent_teams t
		 LEFT JOIN agents a ON a.id = t.lead_agent_id
		 ORDER BY t.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var teams []store.TeamData
	teamIndex := map[uuid.UUID]int{} // map team ID → index in teams slice
	for rows.Next() {
		var d store.TeamData
		var desc sql.NullString
		if err := rows.Scan(
			&d.ID, &d.Name, &d.LeadAgentID, &desc, &d.Status,
			&d.Settings, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt,
			&d.LeadAgentKey, &d.LeadDisplayName,
		); err != nil {
			return nil, err
		}
		if desc.Valid {
			d.Description = desc.String
		}
		teamIndex[d.ID] = len(teams)
		teams = append(teams, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Bulk-fetch all members for returned teams
	if len(teams) > 0 {
		mRows, err := s.db.QueryContext(ctx,
			`SELECT m.team_id, m.agent_id, m.role, m.joined_at,
			 COALESCE(a.agent_key, '') AS agent_key,
			 COALESCE(a.display_name, '') AS display_name,
			 COALESCE(a.frontmatter, '') AS frontmatter,
			 COALESCE(a.other_config->>'emoji', '') AS emoji
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
			if err := mRows.Scan(&m.TeamID, &m.AgentID, &m.Role, &m.JoinedAt, &m.AgentKey, &m.DisplayName, &m.Frontmatter, &m.Emoji); err != nil {
				return nil, err
			}
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

func (s *PGTeamStore) AddMember(ctx context.Context, teamID, agentID uuid.UUID, role string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_team_members (team_id, agent_id, role, joined_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (team_id, agent_id) DO UPDATE SET role = EXCLUDED.role`,
		teamID, agentID, role, time.Now(),
	)
	return err
}

func (s *PGTeamStore) RemoveMember(ctx context.Context, teamID, agentID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM agent_team_members WHERE team_id = $1 AND agent_id = $2`,
		teamID, agentID,
	)
	return err
}

func (s *PGTeamStore) ListMembers(ctx context.Context, teamID uuid.UUID) ([]store.TeamMemberData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT m.team_id, m.agent_id, m.role, m.joined_at,
		 COALESCE(a.agent_key, '') AS agent_key,
		 COALESCE(a.display_name, '') AS display_name,
		 COALESCE(a.frontmatter, '') AS frontmatter,
		 COALESCE(a.other_config->>'emoji', '') AS emoji
		 FROM agent_team_members m
		 JOIN agents a ON a.id = m.agent_id
		 WHERE m.team_id = $1 AND a.status = 'active'
		 ORDER BY m.joined_at`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []store.TeamMemberData
	for rows.Next() {
		var d store.TeamMemberData
		if err := rows.Scan(
			&d.TeamID, &d.AgentID, &d.Role, &d.JoinedAt,
			&d.AgentKey, &d.DisplayName, &d.Frontmatter, &d.Emoji,
		); err != nil {
			return nil, err
		}
		members = append(members, d)
	}
	return members, rows.Err()
}

func (s *PGTeamStore) ListIdleMembers(ctx context.Context, teamID uuid.UUID) ([]store.TeamMemberData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT m.team_id, m.agent_id, m.role, m.joined_at,
		 COALESCE(a.agent_key, '') AS agent_key,
		 COALESCE(a.display_name, '') AS display_name,
		 COALESCE(a.frontmatter, '') AS frontmatter,
		 COALESCE(a.other_config->>'emoji', '') AS emoji
		 FROM agent_team_members m
		 JOIN agents a ON a.id = m.agent_id
		 WHERE m.team_id = $1 AND a.status = 'active' AND m.role != $2
		   AND NOT EXISTS (
		     SELECT 1 FROM team_tasks t
		     WHERE t.owner_agent_id = m.agent_id AND t.team_id = $1 AND t.status = $3
		   )
		 ORDER BY m.joined_at`, teamID, store.TeamRoleLead, store.TeamTaskStatusInProgress)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []store.TeamMemberData
	for rows.Next() {
		var d store.TeamMemberData
		if err := rows.Scan(
			&d.TeamID, &d.AgentID, &d.Role, &d.JoinedAt,
			&d.AgentKey, &d.DisplayName, &d.Frontmatter, &d.Emoji,
		); err != nil {
			return nil, err
		}
		members = append(members, d)
	}
	return members, rows.Err()
}

func (s *PGTeamStore) GetTeamForAgent(ctx context.Context, agentID uuid.UUID) (*store.TeamData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT t.id, t.name, t.lead_agent_id, t.description, t.status, t.settings, t.created_by, t.created_at, t.updated_at
		 FROM agent_teams t
		 WHERE (
		   t.lead_agent_id = $1
		   OR EXISTS (SELECT 1 FROM agent_team_members m WHERE m.team_id = t.id AND m.agent_id = $1)
		 ) AND t.status = $2
		 ORDER BY (t.lead_agent_id = $1) DESC
		 LIMIT 1`, agentID, store.TeamStatusActive)

	d, err := scanTeamRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return d, err
}

func (s *PGTeamStore) KnownUserIDs(ctx context.Context, teamID uuid.UUID, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT s.user_id
		 FROM sessions s
		 JOIN agent_team_members m ON m.agent_id = s.agent_id
		 WHERE m.team_id = $1 AND s.user_id != ''
		 ORDER BY s.user_id
		 LIMIT $2`, teamID, limit)
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
// Scan helpers
// ============================================================

func scanTeamRow(row *sql.Row) (*store.TeamData, error) {
	var d store.TeamData
	var desc sql.NullString
	err := row.Scan(
		&d.ID, &d.Name, &d.LeadAgentID, &desc, &d.Status,
		&d.Settings, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if desc.Valid {
		d.Description = desc.String
	}
	return &d, nil
}
