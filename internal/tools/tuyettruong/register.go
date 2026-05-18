package tuyettruong

import (
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// readOnly / mutating build a ToolMetadata with the right capability set.
// Goclaw uses a slice of ToolCapability constants (not bool fields).
func readOnly() tools.ToolMetadata {
	return tools.ToolMetadata{
		Capabilities: []tools.ToolCapability{tools.CapReadOnly},
		Group:        "tuyettruong",
	}
}

func mutating() tools.ToolMetadata {
	return tools.ToolMetadata{
		Capabilities: []tools.ToolCapability{tools.CapMutating},
		Group:        "tuyettruong",
	}
}

// RegisterAll wires every tuyettruong tool into the given registry. Safe to
// call at boot in cmd/gateway_setup.go right after the built-in tools are
// registered. Skips registration entirely if the client isn't configured —
// goclaw should still boot even when tuyettruong env vars are missing.
func RegisterAll(reg *tools.Registry) {
	client := NewClient()
	if client == nil {
		slog.Info("tuyettruong tools skipped — missing env",
			"required", MissingEnv())
		return
	}

	// Admin tools — only usable from the admin-agent (its tool allow list
	// includes the tt_* names; sales-agent's tools_config excludes them).
	reg.RegisterWithMetadata(NewProductSearchTool(client, RoleAdmin), readOnly())
	reg.RegisterWithMetadata(NewProductGetTool(client), readOnly())
	reg.RegisterWithMetadata(NewProductCreateTool(client), mutating())
	reg.RegisterWithMetadata(NewProductUpdateTool(client), mutating())
	reg.RegisterWithMetadata(NewVariantUpdateTool(client), mutating())
	reg.RegisterWithMetadata(NewProductDeleteTool(client), mutating())
	reg.RegisterWithMetadata(NewOrderListTool(client), readOnly())
	reg.RegisterWithMetadata(NewOrderUpdateStatusTool(client), mutating())

	// Sales tools — usable from sales-agent. Quote tools mutate in-process
	// draft state; order_place writes to the store; the rest read or notify.
	reg.RegisterWithMetadata(NewQuoteAddItemTool(client), mutating())
	reg.RegisterWithMetadata(NewQuoteRemoveItemTool(), mutating())
	reg.RegisterWithMetadata(NewQuoteViewTool(), readOnly())
	reg.RegisterWithMetadata(NewQuoteSetCustomerTool(), mutating())
	reg.RegisterWithMetadata(NewQuoteFinalizeTool(client), readOnly())
	reg.RegisterWithMetadata(NewQuoteClearTool(), mutating())
	reg.RegisterWithMetadata(NewOrderPlaceTool(client), mutating())
	reg.RegisterWithMetadata(NewOrderCustomerClaimedPaidTool(client), mutating())
	reg.RegisterWithMetadata(NewOrderLookupTool(client), readOnly())
	reg.RegisterWithMetadata(NewNotifyAdminTool(), readOnly())

	// Sales bot also needs product_search read-only — register a sales-role
	// variant under a distinct alias so the agent allow lists can differ.
	reg.RegisterWithMetadata(NewProductSearchTool(client, RoleSales).WithName("sales_product_search"), readOnly())

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
