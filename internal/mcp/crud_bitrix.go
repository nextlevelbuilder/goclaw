package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// bitrixPortalView is the credential-masked API view of a Bitrix24 portal.
type bitrixPortalView struct {
	Name      string `json:"name"`
	Domain    string `json:"domain"`
	Installed bool   `json:"installed"`
}

// registerBitrixCRUDTools registers the goclaw_bitrix_portals_* MCP tools
// backed by store.BitrixPortalStore. Requires a tenant-scoped context
// (store.WithTenantID) or master scope, per store.BitrixPortalStore's own
// contract — this server does not additionally gate access.
func registerBitrixCRUDTools(srv *mcpserver.MCPServer, portals store.BitrixPortalStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_bitrix_portals_list",
		mcpgo.WithDescription("List Bitrix24 portals for the caller's tenant (credentials masked)."),
		mcpgo.WithString("tenant_id", mcpgo.Required(), mcpgo.Description("Tenant UUID.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleBitrixPortalsList(portals))

	srv.AddTool(mcpgo.NewTool("goclaw_bitrix_portals_create",
		mcpgo.WithDescription("Provision a new Bitrix24 portal."),
		mcpgo.WithString("tenant_id", mcpgo.Required(), mcpgo.Description("Tenant UUID.")),
		mcpgo.WithString("name", mcpgo.Required(), mcpgo.Description("Portal name (unique per tenant).")),
		mcpgo.WithString("domain", mcpgo.Required(), mcpgo.Description("Bitrix24 portal domain.")),
		mcpgo.WithString("client_id", mcpgo.Required(), mcpgo.Description("Bitrix24 OAuth app client ID.")),
		mcpgo.WithString("client_secret", mcpgo.Required(), mcpgo.Description("Bitrix24 OAuth app client secret.")),
	), handleBitrixPortalsCreate(portals))

	srv.AddTool(mcpgo.NewTool("goclaw_bitrix_portals_delete",
		mcpgo.WithDescription("Delete a Bitrix24 portal."),
		mcpgo.WithString("tenant_id", mcpgo.Required(), mcpgo.Description("Tenant UUID.")),
		mcpgo.WithString("name", mcpgo.Required(), mcpgo.Description("Portal name.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleBitrixPortalsDelete(portals))
}

func handleBitrixPortalsList(portals store.BitrixPortalStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		tenantID, err := parseTenantID(req)
		if err != nil {
			return toolError("bitrix_portals.list", err)
		}
		list, err := portals.ListByTenant(ctx, tenantID)
		if err != nil {
			return toolError("bitrix_portals.list", err)
		}
		views := make([]bitrixPortalView, 0, len(list))
		for _, p := range list {
			installed := false
			if len(p.State) > 0 {
				var state store.BitrixPortalState
				if err := json.Unmarshal(p.State, &state); err == nil {
					installed = state.AccessToken != ""
				}
			}
			views = append(views, bitrixPortalView{Name: p.Name, Domain: p.Domain, Installed: installed})
		}
		return jsonToolResult(map[string]any{"portals": views})
	}
}

func handleBitrixPortalsCreate(portals store.BitrixPortalStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		tenantID, err := parseTenantID(req)
		if err != nil {
			return toolError("bitrix_portals.create", err)
		}
		name, err := req.RequireString("name")
		if err != nil {
			return toolError("bitrix_portals.create", err)
		}
		domain, err := req.RequireString("domain")
		if err != nil {
			return toolError("bitrix_portals.create", err)
		}
		clientID, err := req.RequireString("client_id")
		if err != nil {
			return toolError("bitrix_portals.create", err)
		}
		clientSecret, err := req.RequireString("client_secret")
		if err != nil {
			return toolError("bitrix_portals.create", err)
		}

		creds, err := json.Marshal(store.BitrixPortalCredentials{ClientID: clientID, ClientSecret: clientSecret})
		if err != nil {
			return toolError("bitrix_portals.create", fmt.Errorf("marshal credentials: %w", err))
		}
		portal := &store.BitrixPortalData{
			BaseModel:   store.BaseModel{ID: store.GenNewID()},
			TenantID:    tenantID,
			Name:        name,
			Domain:      domain,
			Credentials: creds,
		}
		if err := portals.Create(ctx, portal); err != nil {
			return toolError("bitrix_portals.create", err)
		}
		return jsonToolResult(map[string]string{"name": name, "domain": domain})
	}
}

func handleBitrixPortalsDelete(portals store.BitrixPortalStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		tenantID, err := parseTenantID(req)
		if err != nil {
			return toolError("bitrix_portals.delete", err)
		}
		name, err := req.RequireString("name")
		if err != nil {
			return toolError("bitrix_portals.delete", err)
		}
		if err := portals.Delete(ctx, tenantID, name); err != nil {
			return toolError("bitrix_portals.delete", err)
		}
		return jsonToolResult(map[string]string{"status": "deleted"})
	}
}

// parseTenantID reads and parses the required "tenant_id" argument.
func parseTenantID(req mcpgo.CallToolRequest) (uuid.UUID, error) {
	tenantIDStr, err := req.RequireString("tenant_id")
	if err != nil {
		return uuid.Nil, err
	}
	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid tenant_id: %w", err)
	}
	return tenantID, nil
}
