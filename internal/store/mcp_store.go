package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// MCPServerData represents an MCP server in the database.
type MCPServerData struct {
	BaseModel
	Name        string  `json:"name"`
	DisplayName string  `json:"display_name,omitempty"`
	Transport   string  `json:"transport"`               // "stdio", "sse", "streamable-http"
	Command     string  `json:"command,omitempty"`        // stdio
	Args        json.RawMessage `json:"args,omitempty"`    // JSONB
	URL         string  `json:"url,omitempty"`            // sse/http
	Headers     json.RawMessage `json:"headers,omitempty"`  // JSONB
	Env         json.RawMessage `json:"env,omitempty"`     // JSONB (stdio)
	APIKey      string  `json:"api_key,omitempty"`        // encrypted
	ToolPrefix  string  `json:"tool_prefix,omitempty"`
	TimeoutSec  int     `json:"timeout_sec"`
	Settings    json.RawMessage `json:"settings,omitempty"` // JSONB
	Enabled     bool    `json:"enabled"`
	CreatedBy   string  `json:"created_by"`
}

// MCPAgentGrant represents an MCP server grant to an agent.
type MCPAgentGrant struct {
	ID              uuid.UUID `json:"id"`
	ServerID        uuid.UUID `json:"server_id"`
	AgentID         uuid.UUID `json:"agent_id"`
	Enabled         bool      `json:"enabled"`
	ToolAllow       json.RawMessage `json:"tool_allow,omitempty"`       // JSONB
	ToolDeny        json.RawMessage `json:"tool_deny,omitempty"`        // JSONB
	ConfigOverrides json.RawMessage `json:"config_overrides,omitempty"` // JSONB
	GrantedBy       string    `json:"granted_by"`
	CreatedAt       time.Time `json:"created_at"`
}

// MCPUserGrant represents an MCP server grant to a user.
type MCPUserGrant struct {
	ID        uuid.UUID `json:"id"`
	ServerID  uuid.UUID `json:"server_id"`
	UserID    string    `json:"user_id"`
	Enabled   bool      `json:"enabled"`
	ToolAllow json.RawMessage `json:"tool_allow,omitempty"` // JSONB
	ToolDeny  json.RawMessage `json:"tool_deny,omitempty"`  // JSONB
	GrantedBy string          `json:"granted_by"`
	CreatedAt time.Time       `json:"created_at"`
}

// MCPAccessRequest represents a request for MCP server access.
type MCPAccessRequest struct {
	ID          uuid.UUID  `json:"id"`
	ServerID    uuid.UUID  `json:"server_id"`
	AgentID     *uuid.UUID `json:"agent_id,omitempty"`
	UserID      string     `json:"user_id,omitempty"`
	Scope       string     `json:"scope"`                    // "agent" or "user"
	Status      string     `json:"status"`                   // "pending", "approved", "rejected"
	Reason      string     `json:"reason,omitempty"`
	ToolAllow   json.RawMessage `json:"tool_allow,omitempty"` // JSONB
	RequestedBy string    `json:"requested_by"`
	ReviewedBy  string     `json:"reviewed_by,omitempty"`
	ReviewedAt  *time.Time `json:"reviewed_at,omitempty"`
	ReviewNote  string     `json:"review_note,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// MCPAccessInfo combines server data with grant-level tool filters for runtime resolution.
type MCPAccessInfo struct {
	Server    MCPServerData `json:"server"`
	ToolAllow []string      `json:"tool_allow,omitempty"` // effective allow list (nil = all)
	ToolDeny  []string      `json:"tool_deny,omitempty"`  // effective deny list
}

// MCPServerStore manages MCP server configs and access grants (managed mode only).
type MCPServerStore interface {
	// Server CRUD
	CreateServer(ctx context.Context, s *MCPServerData) error
	GetServer(ctx context.Context, id uuid.UUID) (*MCPServerData, error)
	GetServerByName(ctx context.Context, name string) (*MCPServerData, error)
	ListServers(ctx context.Context) ([]MCPServerData, error)
	UpdateServer(ctx context.Context, id uuid.UUID, updates map[string]any) error
	DeleteServer(ctx context.Context, id uuid.UUID) error

	// Agent grants
	GrantToAgent(ctx context.Context, g *MCPAgentGrant) error
	RevokeFromAgent(ctx context.Context, serverID, agentID uuid.UUID) error
	ListAgentGrants(ctx context.Context, agentID uuid.UUID) ([]MCPAgentGrant, error)

	// User grants
	GrantToUser(ctx context.Context, g *MCPUserGrant) error
	RevokeFromUser(ctx context.Context, serverID uuid.UUID, userID string) error

	// Resolution: all accessible MCP servers + tool filters for agent+user
	ListAccessible(ctx context.Context, agentID uuid.UUID, userID string) ([]MCPAccessInfo, error)

	// Access requests
	CreateRequest(ctx context.Context, req *MCPAccessRequest) error
	ListPendingRequests(ctx context.Context) ([]MCPAccessRequest, error)
	ReviewRequest(ctx context.Context, requestID uuid.UUID, approved bool, reviewedBy, note string) error
}
