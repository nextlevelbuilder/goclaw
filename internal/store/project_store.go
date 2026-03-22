package store

import (
	"context"

	"github.com/google/uuid"
)

// Project represents a workspace bound to a group chat.
type Project struct {
	BaseModel
	Name        string     `json:"name"`
	Slug        string     `json:"slug"`
	ChannelType *string    `json:"channel_type,omitempty"`
	ChatID      *string    `json:"chat_id,omitempty"`
	TeamID      *uuid.UUID `json:"team_id,omitempty"`
	Description *string    `json:"description,omitempty"`
	Status      string     `json:"status"`
	CreatedBy   string     `json:"created_by"`
}

// ProjectMCPOverride holds per-project env overrides for an MCP server.
type ProjectMCPOverride struct {
	ID           uuid.UUID         `json:"id"`
	ProjectID    uuid.UUID         `json:"project_id"`
	ServerName   string            `json:"server_name"`
	EnvOverrides map[string]string `json:"env_overrides"`
	Enabled      bool              `json:"enabled"`
}

// ProjectStore manages project entities and their MCP overrides.
type ProjectStore interface {
	CreateProject(ctx context.Context, p *Project) error
	GetProject(ctx context.Context, id uuid.UUID) (*Project, error)
	GetProjectBySlug(ctx context.Context, slug string) (*Project, error)
	GetProjectByChatID(ctx context.Context, channelType, chatID string) (*Project, error)
	ListProjects(ctx context.Context) ([]Project, error)
	UpdateProject(ctx context.Context, id uuid.UUID, updates map[string]any) error
	DeleteProject(ctx context.Context, id uuid.UUID) error

	// MCP overrides
	SetMCPOverride(ctx context.Context, projectID uuid.UUID, serverName string, envOverrides map[string]string) error
	RemoveMCPOverride(ctx context.Context, projectID uuid.UUID, serverName string) error
	GetMCPOverrides(ctx context.Context, projectID uuid.UUID) ([]ProjectMCPOverride, error)
	// GetMCPOverridesMap returns {serverName: {envKey: envVal}} for runtime injection.
	GetMCPOverridesMap(ctx context.Context, projectID uuid.UUID) (map[string]map[string]string, error)
}
