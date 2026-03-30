package store

import (
	"context"

	"github.com/google/uuid"
)

// Project represents a workspace bound to a group chat (channel_type + chat_id).
// When a message arrives on a chat bound to a project, per-project MCP environment
// overrides are injected into MCP server connections for the duration of that run.
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

// ProjectMCPOverride holds per-project environment variable overrides for an MCP server.
// When a project is active, these env vars are merged into the server's base env
// (project overrides take precedence, base keys are never removed).
type ProjectMCPOverride struct {
	ID           uuid.UUID         `json:"id"`
	ProjectID    uuid.UUID         `json:"project_id"`
	ServerName   string            `json:"server_name"`
	EnvOverrides map[string]string `json:"env_overrides"`
	Enabled      bool              `json:"enabled"`
}

// ProjectStore manages projects and their MCP environment overrides.
type ProjectStore interface {
	CreateProject(ctx context.Context, p *Project) error
	GetProject(ctx context.Context, id uuid.UUID) (*Project, error)
	GetProjectBySlug(ctx context.Context, slug string) (*Project, error)
	GetProjectByChatID(ctx context.Context, channelType, chatID string) (*Project, error)
	ListProjects(ctx context.Context) ([]Project, error)
	UpdateProject(ctx context.Context, id uuid.UUID, updates map[string]any) error
	DeleteProject(ctx context.Context, id uuid.UUID) error

	// MCP environment overrides
	SetMCPOverride(ctx context.Context, projectID uuid.UUID, serverName string, envOverrides map[string]string) error
	RemoveMCPOverride(ctx context.Context, projectID uuid.UUID, serverName string) error
	GetMCPOverrides(ctx context.Context, projectID uuid.UUID) ([]ProjectMCPOverride, error)
	// GetMCPOverridesMap returns {serverName: {envKey: envVal}} for runtime injection.
	GetMCPOverridesMap(ctx context.Context, projectID uuid.UUID) (map[string]map[string]string, error)
}

// --- Context propagation for project scope ---

type projectIDKey struct{}
type projectOverridesKey struct{}

// WithProjectID injects a project ID into the context.
func WithProjectID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, projectIDKey{}, id)
}

// ProjectIDFromContext extracts the project ID from context. Returns "" if unset.
func ProjectIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(projectIDKey{}).(string); ok {
		return v
	}
	return ""
}

// WithProjectOverrides injects per-server MCP env overrides into the context.
func WithProjectOverrides(ctx context.Context, overrides map[string]map[string]string) context.Context {
	return context.WithValue(ctx, projectOverridesKey{}, overrides)
}

// ProjectOverridesFromContext extracts per-server MCP env overrides. Returns nil if unset.
func ProjectOverridesFromContext(ctx context.Context) map[string]map[string]string {
	if v, ok := ctx.Value(projectOverridesKey{}).(map[string]map[string]string); ok {
		return v
	}
	return nil
}
