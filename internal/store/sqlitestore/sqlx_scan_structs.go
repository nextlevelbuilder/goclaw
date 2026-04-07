//go:build sqlite || sqliteonly

package sqlitestore

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// providerRow is a scan struct for llm_providers rows.
// Uses sqliteTime for created_at/updated_at to handle SQLite text timestamps.
type providerRow struct {
	ID           uuid.UUID       `json:"id"`
	Name         string          `json:"name"`
	DisplayName  string          `json:"display_name"`
	ProviderType string          `json:"provider_type"`
	APIBase      string          `json:"api_base"`
	APIKey       string          `json:"api_key"`
	Enabled      bool            `json:"enabled"`
	Settings     json.RawMessage `json:"settings"`
	CreatedAt    sqliteTime      `json:"created_at"`
	UpdatedAt    sqliteTime      `json:"updated_at"`
	TenantID     uuid.UUID       `json:"tenant_id"`
}

func (r *providerRow) toLLMProviderData() store.LLMProviderData {
	return store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: r.ID, CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time},
		TenantID:     r.TenantID,
		Name:         r.Name,
		DisplayName:  r.DisplayName,
		ProviderType: r.ProviderType,
		APIBase:      r.APIBase,
		APIKey:       r.APIKey,
		Enabled:      r.Enabled,
		Settings:     r.Settings,
	}
}

// tenantRow is a scan struct for tenants rows.
type tenantRow struct {
	ID        uuid.UUID       `json:"id"`
	Name      string          `json:"name"`
	Slug      string          `json:"slug"`
	Status    string          `json:"status"`
	Settings  json.RawMessage `json:"settings"`
	CreatedAt sqliteTime      `json:"created_at"`
	UpdatedAt sqliteTime      `json:"updated_at"`
}

func (r *tenantRow) toTenantData() store.TenantData {
	return store.TenantData{
		ID:        r.ID,
		Name:      r.Name,
		Slug:      r.Slug,
		Status:    r.Status,
		Settings:  r.Settings,
		CreatedAt: r.CreatedAt.Time,
		UpdatedAt: r.UpdatedAt.Time,
	}
}

// tenantUserRow is a scan struct for tenant_users rows.
type tenantUserRow struct {
	ID          uuid.UUID       `json:"id"`
	TenantID    uuid.UUID       `json:"tenant_id"`
	UserID      string          `json:"user_id"`
	DisplayName *string         `json:"display_name"`
	Role        string          `json:"role"`
	Metadata    json.RawMessage `json:"metadata"`
	CreatedAt   sqliteTime      `json:"created_at"`
	UpdatedAt   sqliteTime      `json:"updated_at"`
}

func (r *tenantUserRow) toTenantUserData() store.TenantUserData {
	return store.TenantUserData{
		ID:          r.ID,
		TenantID:    r.TenantID,
		UserID:      r.UserID,
		DisplayName: r.DisplayName,
		Role:        r.Role,
		Metadata:    r.Metadata,
		CreatedAt:   r.CreatedAt.Time,
		UpdatedAt:   r.UpdatedAt.Time,
	}
}

// mcpServerRow is a scan struct for mcp_servers rows.
// Pointer fields handle nullable columns that sqlx maps to empty string otherwise.
type mcpServerRow struct {
	ID          uuid.UUID       `json:"id"`
	Name        string          `json:"name"`
	DisplayName *string         `json:"display_name"`
	Transport   string          `json:"transport"`
	Command     *string         `json:"command"`
	Args        json.RawMessage `json:"args"`
	URL         *string         `json:"url"`
	Headers     json.RawMessage `json:"headers"`
	Env         json.RawMessage `json:"env"`
	APIKey      *string         `json:"api_key"`
	ToolPrefix  *string         `json:"tool_prefix"`
	TimeoutSec  int             `json:"timeout_sec"`
	Settings    json.RawMessage `json:"settings"`
	Enabled     bool            `json:"enabled"`
	CreatedBy   string          `json:"created_by"`
	CreatedAt   sqliteTime      `json:"created_at"`
	UpdatedAt   sqliteTime      `json:"updated_at"`
}

func (r *mcpServerRow) toMCPServerData() store.MCPServerData {
	return store.MCPServerData{
		BaseModel:   store.BaseModel{ID: r.ID, CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time},
		Name:        r.Name,
		DisplayName: derefStr(r.DisplayName),
		Transport:   r.Transport,
		Command:     derefStr(r.Command),
		Args:        r.Args,
		URL:         derefStr(r.URL),
		Headers:     r.Headers,
		Env:         r.Env,
		APIKey:      derefStr(r.APIKey),
		ToolPrefix:  derefStr(r.ToolPrefix),
		TimeoutSec:  r.TimeoutSec,
		Settings:    r.Settings,
		Enabled:     r.Enabled,
		CreatedBy:   r.CreatedBy,
	}
}
