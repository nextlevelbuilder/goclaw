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
	Name        string          `json:"name" db:"name"`
	DisplayName string          `json:"display_name,omitempty" db:"display_name"`
	Transport   string          `json:"transport" db:"transport"`
	Command     string          `json:"command,omitempty" db:"command"`
	Args        json.RawMessage `json:"args,omitempty" db:"args"`
	URL         string          `json:"url,omitempty" db:"url"`
	Headers     json.RawMessage `json:"headers,omitempty" db:"headers"`
	Env         json.RawMessage `json:"env,omitempty" db:"env"`
	APIKey      string          `json:"api_key,omitempty" db:"api_key"`
	ToolPrefix  string          `json:"tool_prefix,omitempty" db:"tool_prefix"`
	TimeoutSec  int             `json:"timeout_sec" db:"timeout_sec"`
	Settings    json.RawMessage `json:"settings,omitempty" db:"settings"`
	Enabled     bool            `json:"enabled" db:"enabled"`
	// RequireUserCredentials marks servers that mint credentials per-user at
	// message time (e.g. Bitrix24 channel auto-onboard) instead of sharing a
	// single admin api_key across every caller. Promoted from
	// settings.require_user_credentials (JSONB) to a top-level column so
	// channel factories can filter mcp_servers directly.
	RequireUserCredentials bool   `json:"require_user_credentials" db:"require_user_credentials"`
	CreatedBy              string `json:"created_by" db:"created_by"`
}

// MCPAgentGrant represents an MCP server grant to an agent.
type MCPAgentGrant struct {
	ID              uuid.UUID       `json:"id" db:"id"`
	ServerID        uuid.UUID       `json:"server_id" db:"server_id"`
	AgentID         uuid.UUID       `json:"agent_id" db:"agent_id"`
	Enabled         bool            `json:"enabled" db:"enabled"`
	ToolAllow       json.RawMessage `json:"tool_allow,omitempty" db:"tool_allow"`             // JSONB
	ToolDeny        json.RawMessage `json:"tool_deny,omitempty" db:"tool_deny"`               // JSONB
	ConfigOverrides json.RawMessage `json:"config_overrides,omitempty" db:"config_overrides"` // JSONB
	GrantedBy       string          `json:"granted_by" db:"granted_by"`
	CreatedAt       time.Time       `json:"created_at" db:"created_at"`
}

// MCPUserGrant represents an MCP server grant to a user.
type MCPUserGrant struct {
	ID        uuid.UUID       `json:"id" db:"id"`
	ServerID  uuid.UUID       `json:"server_id" db:"server_id"`
	UserID    string          `json:"user_id" db:"user_id"`
	Enabled   bool            `json:"enabled" db:"enabled"`
	ToolAllow json.RawMessage `json:"tool_allow,omitempty" db:"tool_allow"` // JSONB
	ToolDeny  json.RawMessage `json:"tool_deny,omitempty" db:"tool_deny"`   // JSONB
	GrantedBy string          `json:"granted_by" db:"granted_by"`
	CreatedAt time.Time       `json:"created_at" db:"created_at"`
}

// MCPAccessRequest represents a request for MCP server access.
type MCPAccessRequest struct {
	ID          uuid.UUID       `json:"id" db:"id"`
	ServerID    uuid.UUID       `json:"server_id" db:"server_id"`
	AgentID     *uuid.UUID      `json:"agent_id,omitempty" db:"agent_id"`
	UserID      string          `json:"user_id,omitempty" db:"user_id"`
	Scope       string          `json:"scope" db:"scope"`   // "agent" or "user"
	Status      string          `json:"status" db:"status"` // "pending", "approved", "rejected"
	Reason      string          `json:"reason,omitempty" db:"reason"`
	ToolAllow   json.RawMessage `json:"tool_allow,omitempty" db:"tool_allow"` // JSONB
	RequestedBy string          `json:"requested_by" db:"requested_by"`
	ReviewedBy  string          `json:"reviewed_by,omitempty" db:"reviewed_by"`
	ReviewedAt  *time.Time      `json:"reviewed_at,omitempty" db:"reviewed_at"`
	ReviewNote  string          `json:"review_note,omitempty" db:"review_note"`
	CreatedAt   time.Time       `json:"created_at" db:"created_at"`
}

// MCPAccessInfo combines server data with grant-level tool filters for runtime resolution.
type MCPAccessInfo struct {
	Server    MCPServerData `json:"server" db:"-"`
	ToolAllow []string      `json:"tool_allow,omitempty" db:"-"` // effective allow list (nil = all)
	ToolDeny  []string      `json:"tool_deny,omitempty" db:"-"`  // effective deny list
}

// MCPUserCredentials holds per-user credential overrides for an MCP server.
type MCPUserCredentials struct {
	APIKey  string            `json:"api_key,omitempty" db:"-"` // decrypted
	Headers map[string]string `json:"headers,omitempty" db:"-"` // decrypted
	Env     map[string]string `json:"env,omitempty" db:"-"`     // decrypted
}

// MCPOAuthToken holds OAuth tokens for an MCP server, either global (UserID="")
// or per-user (UserID set). Sensitive fields are decrypted before delivery.
type MCPOAuthToken struct {
	ID              uuid.UUID  `db:"id"`
	ServerID        uuid.UUID  `db:"server_id"`
	TenantID        uuid.UUID  `db:"tenant_id"`
	UserID          string     `db:"user_id"` // empty = global
	AccessToken     string     `db:"access_token"`
	RefreshToken    string     `db:"refresh_token"`
	TokenType       string     `db:"token_type"`
	Scopes          string     `db:"scopes"`
	ExpiresAt       *time.Time `db:"expires_at"`
	IssuedAt        *time.Time `db:"issued_at"`
	DCRClientID     string     `db:"dcr_client_id"`
	DCRClientSecret string     `db:"dcr_client_secret"` // decrypted
	DCRIssuer       string     `db:"dcr_issuer"`
	TokenEndpoint   string     `db:"token_endpoint"`
	ResourceURI     string     `db:"resource_uri"`
	CreatedAt       time.Time  `db:"created_at"`
	UpdatedAt       time.Time  `db:"updated_at"`
}

// MCPOAuthTokenStore manages OAuth tokens for MCP servers.
type MCPOAuthTokenStore interface {
	// GetOAuthToken returns the global (tenant-level) OAuth token for a server.
	GetOAuthToken(ctx context.Context, serverID, tenantID uuid.UUID) (*MCPOAuthToken, error)
	// GetUserOAuthToken returns the per-user OAuth token for a server.
	GetUserOAuthToken(ctx context.Context, serverID, tenantID uuid.UUID, userID string) (*MCPOAuthToken, error)
	// UpsertOAuthToken inserts or replaces an OAuth token record.
	UpsertOAuthToken(ctx context.Context, token *MCPOAuthToken) error
	// DeleteOAuthToken deletes the global OAuth token for a server.
	DeleteOAuthToken(ctx context.Context, serverID, tenantID uuid.UUID) error
	// DeleteUserOAuthToken deletes the per-user OAuth token for a server.
	DeleteUserOAuthToken(ctx context.Context, serverID, tenantID uuid.UUID, userID string) error
	// DeleteServerOAuthTokens deletes ALL OAuth tokens for a server (global +
	// every per-user row). Used when the server URL or OAuth config changes and
	// the previously minted tokens are no longer valid for the new resource/AS.
	DeleteServerOAuthTokens(ctx context.Context, serverID, tenantID uuid.UUID) error
}

// MCPServerStore manages MCP server configs and access grants.
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
	ListServerGrants(ctx context.Context, serverID uuid.UUID) ([]MCPAgentGrant, error)

	// User grants
	GrantToUser(ctx context.Context, g *MCPUserGrant) error
	RevokeFromUser(ctx context.Context, serverID uuid.UUID, userID string) error

	// Counts: agent grant counts per server (for listing UI)
	CountAgentGrantsByServer(ctx context.Context) (map[uuid.UUID]int, error)

	// Resolution: all accessible MCP servers + tool filters for agent+user
	ListAccessible(ctx context.Context, agentID uuid.UUID, userID string) ([]MCPAccessInfo, error)

	// Access requests
	CreateRequest(ctx context.Context, req *MCPAccessRequest) error
	ListPendingRequests(ctx context.Context) ([]MCPAccessRequest, error)
	ReviewRequest(ctx context.Context, requestID uuid.UUID, approved bool, reviewedBy, note string) error

	// Per-user credentials
	GetUserCredentials(ctx context.Context, serverID uuid.UUID, userID string) (*MCPUserCredentials, error)
	SetUserCredentials(ctx context.Context, serverID uuid.UUID, userID string, creds MCPUserCredentials) error
	DeleteUserCredentials(ctx context.Context, serverID uuid.UUID, userID string) error

	// CacheToolDescriptions stores a map of tool name → cached tool info
	// (description + real parameter schema, when available) into the
	// server's settings JSONB under the "tool_cache" key.
	CacheToolDescriptions(ctx context.Context, serverID uuid.UUID, toolInfo map[string]CachedToolInfo) error
}

// CachedToolInfo is the cached, per-tool information stored under an MCP
// server's settings "tool_cache" key. It is used to render prompt previews
// without a live connection to the MCP server.
type CachedToolInfo struct {
	// Description is the tool's human-readable description.
	Description string `json:"description"`
	// Parameters is the tool's full JSON Schema for its input parameters,
	// captured from the live MCP connection at cache-write time. It may be
	// nil for cache entries written before this field existed, or for
	// servers whose tools genuinely expose no schema.
	Parameters json.RawMessage `json:"parameters,omitempty"`
}
