package tuyettruong

import (
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// RegisterAll wires every tuyettruong tool into the given registry. Safe to
// call at boot in cmd/gateway_setup.go right after the built-in tools are
// registered. Skips registration entirely if the client isn't configured —
// goclaw should still boot even when tuyettruong env vars are missing.
func RegisterAll(reg *tools.Registry) {
	client := NewClient()
	if client == nil {
		slog.Info("tuyettruong tools skipped (TUYETTRUONG_API_BASE not set)")
		return
	}

	// Admin tools — only usable from the admin-agent (its tool allow list
	// includes the tt_* names; sales-agent's tools_config excludes them).
	reg.RegisterWithMetadata(NewProductSearchTool(client, RoleAdmin), tools.ToolMetadata{ReadOnly: true})
	reg.RegisterWithMetadata(NewProductGetTool(client), tools.ToolMetadata{ReadOnly: true})
	reg.RegisterWithMetadata(NewProductCreateTool(client), tools.ToolMetadata{Mutating: true})
	reg.RegisterWithMetadata(NewProductUpdateTool(client), tools.ToolMetadata{Mutating: true})
	reg.RegisterWithMetadata(NewVariantUpdateTool(client), tools.ToolMetadata{Mutating: true})
	reg.RegisterWithMetadata(NewProductDeleteTool(client), tools.ToolMetadata{Mutating: true})
	reg.RegisterWithMetadata(NewOrderListTool(client), tools.ToolMetadata{ReadOnly: true})
	reg.RegisterWithMetadata(NewOrderUpdateStatusTool(client), tools.ToolMetadata{Mutating: true})

	slog.Info("tuyettruong admin tools registered",
		"base", client.baseURL,
		"tools", []string{
			"tt_product_search", "tt_product_get",
			"tt_product_create", "tt_product_update",
			"tt_variant_update", "tt_product_delete",
			"tt_order_list", "tt_order_update_status",
		})
}
