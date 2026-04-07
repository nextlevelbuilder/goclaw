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
	ID           uuid.UUID       `json:"id" db:"id"`
	Name         string          `json:"name" db:"name"`
	DisplayName  string          `json:"display_name" db:"display_name"`
	ProviderType string          `json:"provider_type" db:"provider_type"`
	APIBase      string          `json:"api_base" db:"api_base"`
	APIKey       string          `json:"api_key" db:"api_key"`
	Enabled      bool            `json:"enabled" db:"enabled"`
	Settings     json.RawMessage `json:"settings" db:"settings"`
	CreatedAt    sqliteTime      `json:"created_at" db:"created_at"`
	UpdatedAt    sqliteTime      `json:"updated_at" db:"updated_at"`
	TenantID     uuid.UUID       `json:"tenant_id" db:"tenant_id"`
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
	ID        uuid.UUID       `json:"id" db:"id"`
	Name      string          `json:"name" db:"name"`
	Slug      string          `json:"slug" db:"slug"`
	Status    string          `json:"status" db:"status"`
	Settings  json.RawMessage `json:"settings" db:"settings"`
	CreatedAt sqliteTime      `json:"created_at" db:"created_at"`
	UpdatedAt sqliteTime      `json:"updated_at" db:"updated_at"`
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
	ID          uuid.UUID       `json:"id" db:"id"`
	TenantID    uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	UserID      string          `json:"user_id" db:"user_id"`
	DisplayName *string         `json:"display_name" db:"display_name"`
	Role        string          `json:"role" db:"role"`
	Metadata    json.RawMessage `json:"metadata" db:"metadata"`
	CreatedAt   sqliteTime      `json:"created_at" db:"created_at"`
	UpdatedAt   sqliteTime      `json:"updated_at" db:"updated_at"`
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
	ID          uuid.UUID       `json:"id" db:"id"`
	Name        string          `json:"name" db:"name"`
	DisplayName *string         `json:"display_name" db:"display_name"`
	Transport   string          `json:"transport" db:"transport"`
	Command     *string         `json:"command" db:"command"`
	Args        json.RawMessage `json:"args" db:"args"`
	URL         *string         `json:"url" db:"url"`
	Headers     json.RawMessage `json:"headers" db:"headers"`
	Env         json.RawMessage `json:"env" db:"env"`
	APIKey      *string         `json:"api_key" db:"api_key"`
	ToolPrefix  *string         `json:"tool_prefix" db:"tool_prefix"`
	TimeoutSec  int             `json:"timeout_sec" db:"timeout_sec"`
	Settings    json.RawMessage `json:"settings" db:"settings"`
	Enabled     bool            `json:"enabled" db:"enabled"`
	CreatedBy   string          `json:"created_by" db:"created_by"`
	CreatedAt   sqliteTime      `json:"created_at" db:"created_at"`
	UpdatedAt   sqliteTime      `json:"updated_at" db:"updated_at"`
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
