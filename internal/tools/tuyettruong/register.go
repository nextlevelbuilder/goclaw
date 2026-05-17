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

	// Sales tools — usable from sales-agent. Quote tools mutate in-process
	// draft state; order_place writes to the store; the rest are read or notify.
	reg.RegisterWithMetadata(NewQuoteAddItemTool(client), tools.ToolMetadata{Mutating: true})
	reg.RegisterWithMetadata(NewQuoteRemoveItemTool(), tools.ToolMetadata{Mutating: true})
	reg.RegisterWithMetadata(NewQuoteViewTool(), tools.ToolMetadata{ReadOnly: true})
	reg.RegisterWithMetadata(NewQuoteSetCustomerTool(), tools.ToolMetadata{Mutating: true})
	reg.RegisterWithMetadata(NewQuoteFinalizeTool(client), tools.ToolMetadata{ReadOnly: true})
	reg.RegisterWithMetadata(NewQuoteClearTool(), tools.ToolMetadata{Mutating: true})
	reg.RegisterWithMetadata(NewOrderPlaceTool(client), tools.ToolMetadata{Mutating: true})
	reg.RegisterWithMetadata(NewOrderCustomerClaimedPaidTool(client), tools.ToolMetadata{Mutating: true})
	reg.RegisterWithMetadata(NewOrderLookupTool(client), tools.ToolMetadata{ReadOnly: true})
	reg.RegisterWithMetadata(NewNotifyAdminTool(), tools.ToolMetadata{Mutating: false})

	// Sales bot also needs product_search read-only — register a sales-role
	// variant under a distinct alias so the agent allow lists can differ.
	reg.RegisterWithMetadata(NewProductSearchTool(client, RoleSales).WithName("sales_product_search"), tools.ToolMetadata{ReadOnly: true})

	slog.Info("tuyettruong tools registered",
		"base", client.baseURL,
		"admin_tools", []string{
			"tt_product_search", "tt_product_get",
			"tt_product_create", "tt_product_update",
			"tt_variant_update", "tt_product_delete",
			"tt_order_list", "tt_order_update_status",
		},
		"sales_tools", []string{
			"sales_product_search",
			"quote_add_item", "quote_remove_item", "quote_view",
			"quote_set_customer", "quote_finalize", "quote_clear",
			"order_place", "order_customer_claimed_paid",
			"order_lookup", "notify_admin",
		})
}
