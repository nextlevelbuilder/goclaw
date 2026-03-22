package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

var secretKeyPattern = regexp.MustCompile(`(?i)(^|_)(TOKEN|SECRET|PASSWORD|API_KEY)($|_)`)

// PGProjectStore implements store.ProjectStore backed by Postgres.
type PGProjectStore struct {
	db *sql.DB
}

func NewPGProjectStore(db *sql.DB) *PGProjectStore {
	return &PGProjectStore{db: db}
}

// --- Project CRUD ---

func (s *PGProjectStore) CreateProject(ctx context.Context, p *store.Project) error {
	query := `INSERT INTO projects (name, slug, channel_type, chat_id, team_id, description, status, created_by)
	           VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id, created_at, updated_at`
	return s.db.QueryRowContext(ctx, query,
		p.Name, p.Slug, p.ChannelType, p.ChatID, nilUUID(p.TeamID),
		p.Description, p.Status, p.CreatedBy,
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
}

func (s *PGProjectStore) GetProject(ctx context.Context, id uuid.UUID) (*store.Project, error) {
	query := `SELECT id, name, slug, channel_type, chat_id, team_id, description, status, created_by, created_at, updated_at
	           FROM projects WHERE id = $1`
	return s.scanProject(s.db.QueryRowContext(ctx, query, id))
}

func (s *PGProjectStore) GetProjectBySlug(ctx context.Context, slug string) (*store.Project, error) {
	query := `SELECT id, name, slug, channel_type, chat_id, team_id, description, status, created_by, created_at, updated_at
	           FROM projects WHERE slug = $1`
	return s.scanProject(s.db.QueryRowContext(ctx, query, slug))
}

func (s *PGProjectStore) GetProjectByChatID(ctx context.Context, channelType, chatID string) (*store.Project, error) {
	query := `SELECT id, name, slug, channel_type, chat_id, team_id, description, status, created_by, created_at, updated_at
	           FROM projects WHERE channel_type = $1 AND chat_id = $2 AND status = 'active'`
	p, err := s.scanProject(s.db.QueryRowContext(ctx, query, channelType, chatID))
	if err == sql.ErrNoRows {
		return nil, nil // no project — not an error
	}
	return p, err
}

func (s *PGProjectStore) scanProject(row *sql.Row) (*store.Project, error) {
	p := &store.Project{}
	err := row.Scan(
		&p.ID, &p.Name, &p.Slug, &p.ChannelType, &p.ChatID, &p.TeamID,
		&p.Description, &p.Status, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *PGProjectStore) ListProjects(ctx context.Context) ([]store.Project, error) {
	query := `SELECT id, name, slug, channel_type, chat_id, team_id, description, status, created_by, created_at, updated_at
	           FROM projects ORDER BY name`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]store.Project, 0)
	for rows.Next() {
		var p store.Project
		if err := rows.Scan(
			&p.ID, &p.Name, &p.Slug, &p.ChannelType, &p.ChatID, &p.TeamID,
			&p.Description, &p.Status, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			continue
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (s *PGProjectStore) UpdateProject(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	return execMapUpdate(ctx, s.db, "projects", id, updates)
}

func (s *PGProjectStore) DeleteProject(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM projects WHERE id = $1", id)
	return err
}

// --- MCP overrides ---

// SetMCPOverride upserts env overrides for a project+server.
// Rejects keys that look like secrets (TOKEN, SECRET, PASSWORD, API_KEY).
func (s *PGProjectStore) SetMCPOverride(ctx context.Context, projectID uuid.UUID, serverName string, envOverrides map[string]string) error {
	for key := range envOverrides {
		if secretKeyPattern.MatchString(key) {
			return fmt.Errorf("env key %q contains secret pattern (TOKEN/SECRET/PASSWORD/API_KEY) — use mcp_servers.env for secrets", key)
		}
	}
	envJSON, err := json.Marshal(envOverrides)
	if err != nil {
		return err
	}
	query := `INSERT INTO project_mcp_overrides (project_id, server_name, env_overrides)
	           VALUES ($1, $2, $3)
	           ON CONFLICT (project_id, server_name) DO UPDATE SET env_overrides = $3, updated_at = NOW()`
	_, err = s.db.ExecContext(ctx, query, projectID, serverName, envJSON)
	return err
}

func (s *PGProjectStore) RemoveMCPOverride(ctx context.Context, projectID uuid.UUID, serverName string) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM project_mcp_overrides WHERE project_id = $1 AND server_name = $2",
		projectID, serverName)
	return err
}

func (s *PGProjectStore) GetMCPOverrides(ctx context.Context, projectID uuid.UUID) ([]store.ProjectMCPOverride, error) {
	query := `SELECT id, project_id, server_name, env_overrides, enabled
	           FROM project_mcp_overrides WHERE project_id = $1 ORDER BY server_name`
	rows, err := s.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]store.ProjectMCPOverride, 0)
	for rows.Next() {
		var o store.ProjectMCPOverride
		var envJSON []byte
		if err := rows.Scan(&o.ID, &o.ProjectID, &o.ServerName, &envJSON, &o.Enabled); err != nil {
			continue
		}
		o.EnvOverrides = make(map[string]string)
		if len(envJSON) > 0 {
			if err := json.Unmarshal(envJSON, &o.EnvOverrides); err != nil {
				continue
			}
		}
		result = append(result, o)
	}
	return result, rows.Err()
}

// GetMCPOverridesMap returns {serverName: {envKey: envVal}} for runtime env injection.
func (s *PGProjectStore) GetMCPOverridesMap(ctx context.Context, projectID uuid.UUID) (map[string]map[string]string, error) {
	query := `SELECT server_name, env_overrides FROM project_mcp_overrides
	           WHERE project_id = $1 AND enabled = true`
	rows, err := s.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]map[string]string)
	for rows.Next() {
		var serverName string
		var envJSON []byte
		if err := rows.Scan(&serverName, &envJSON); err != nil {
			return nil, err
		}
		env := make(map[string]string)
		if err := json.Unmarshal(envJSON, &env); err != nil {
			return nil, err
		}
		result[serverName] = env
	}
	return result, rows.Err()
}
