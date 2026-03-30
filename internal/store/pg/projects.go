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

// secretKeyPattern matches env keys that likely contain secrets.
// These should be stored in mcp_servers.env (encrypted), not in project overrides (plaintext JSONB).
var secretKeyPattern = regexp.MustCompile(`(?i)(TOKEN|SECRET|PASSWORD|API_KEY)`)

type pgProjectStore struct {
	db *sql.DB
}

// NewPGProjectStore creates a new PostgreSQL-backed ProjectStore.
func NewPGProjectStore(db *sql.DB) store.ProjectStore {
	return &pgProjectStore{db: db}
}

func (s *pgProjectStore) CreateProject(ctx context.Context, p *store.Project) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO projects (name, slug, channel_type, chat_id, team_id, description, status, created_by, tenant_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id, created_at, updated_at`,
		p.Name, p.Slug, p.ChannelType, p.ChatID, p.TeamID, p.Description, p.Status, p.CreatedBy,
		store.TenantIDFromContext(ctx),
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
}

func (s *pgProjectStore) GetProject(ctx context.Context, id uuid.UUID) (*store.Project, error) {
	p := &store.Project{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, slug, channel_type, chat_id, team_id, description, status, created_by, created_at, updated_at
		 FROM projects WHERE id = $1`,
		id,
	).Scan(&p.ID, &p.Name, &p.Slug, &p.ChannelType, &p.ChatID, &p.TeamID, &p.Description, &p.Status, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *pgProjectStore) GetProjectBySlug(ctx context.Context, slug string) (*store.Project, error) {
	p := &store.Project{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, slug, channel_type, chat_id, team_id, description, status, created_by, created_at, updated_at
		 FROM projects WHERE slug = $1`,
		slug,
	).Scan(&p.ID, &p.Name, &p.Slug, &p.ChannelType, &p.ChatID, &p.TeamID, &p.Description, &p.Status, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// GetProjectByChatID looks up an active project by channel binding.
// Returns (nil, nil) if no project is bound — intentional for backward compatibility.
func (s *pgProjectStore) GetProjectByChatID(ctx context.Context, channelType, chatID string) (*store.Project, error) {
	p := &store.Project{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, slug, channel_type, chat_id, team_id, description, status, created_by, created_at, updated_at
		 FROM projects WHERE channel_type = $1 AND chat_id = $2 AND status = 'active'`,
		channelType, chatID,
	).Scan(&p.ID, &p.Name, &p.Slug, &p.ChannelType, &p.ChatID, &p.TeamID, &p.Description, &p.Status, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *pgProjectStore) ListProjects(ctx context.Context) ([]store.Project, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, slug, channel_type, chat_id, team_id, description, status, created_by, created_at, updated_at
		 FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []store.Project
	for rows.Next() {
		var p store.Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Slug, &p.ChannelType, &p.ChatID, &p.TeamID, &p.Description, &p.Status, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (s *pgProjectStore) UpdateProject(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	return execMapUpdate(ctx, s.db, "projects", id, updates)
}

func (s *pgProjectStore) DeleteProject(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, id)
	return err
}

// SetMCPOverride upserts environment variable overrides for an MCP server within a project.
// Rejects env keys matching secret patterns (TOKEN, SECRET, PASSWORD, API_KEY) to prevent
// accidental plaintext secret storage. Use mcp_servers.env for secrets instead.
func (s *pgProjectStore) SetMCPOverride(ctx context.Context, projectID uuid.UUID, serverName string, envOverrides map[string]string) error {
	for key := range envOverrides {
		if secretKeyPattern.MatchString(key) {
			return fmt.Errorf("env key %q looks like a secret — store secrets in mcp_servers.env (encrypted), not in project overrides", key)
		}
	}

	envJSON, err := json.Marshal(envOverrides)
	if err != nil {
		return fmt.Errorf("marshal env overrides: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO project_mcp_overrides (project_id, server_name, env_overrides)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (project_id, server_name) DO UPDATE SET env_overrides = $3, updated_at = NOW()`,
		projectID, serverName, envJSON,
	)
	return err
}

func (s *pgProjectStore) RemoveMCPOverride(ctx context.Context, projectID uuid.UUID, serverName string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM project_mcp_overrides WHERE project_id = $1 AND server_name = $2`,
		projectID, serverName,
	)
	return err
}

func (s *pgProjectStore) GetMCPOverrides(ctx context.Context, projectID uuid.UUID) ([]store.ProjectMCPOverride, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, server_name, env_overrides, enabled
		 FROM project_mcp_overrides WHERE project_id = $1 AND enabled = true`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var overrides []store.ProjectMCPOverride
	for rows.Next() {
		var o store.ProjectMCPOverride
		var envJSON []byte
		if err := rows.Scan(&o.ID, &o.ProjectID, &o.ServerName, &envJSON, &o.Enabled); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(envJSON, &o.EnvOverrides); err != nil {
			return nil, fmt.Errorf("unmarshal env overrides for %s: %w", o.ServerName, err)
		}
		overrides = append(overrides, o)
	}
	return overrides, rows.Err()
}

// GetMCPOverridesMap returns a nested map: {serverName: {envKey: envVal}}.
// Used at runtime to inject per-project env vars into MCP server connections.
func (s *pgProjectStore) GetMCPOverridesMap(ctx context.Context, projectID uuid.UUID) (map[string]map[string]string, error) {
	overrides, err := s.GetMCPOverrides(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if len(overrides) == 0 {
		return nil, nil
	}
	result := make(map[string]map[string]string, len(overrides))
	for _, o := range overrides {
		result[o.ServerName] = o.EnvOverrides
	}
	return result, nil
}
