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

var shopToolNames = []string{
	"shop_product_search",
	"shop_product_get",
	"shop_product_create",
	"shop_product_update",
	"shop_variant_update",
	"shop_product_delete",
	"shop_product_lookup_existing",
	"shop_product_draft_from_extracted",
	"shop_order_list",
	"shop_order_update_status",
	"shop_whoami",
	"shop_catalog_search",
	"shop_quote_add_item",
	"shop_quote_remove_item",
	"shop_quote_view",
	"shop_quote_set_customer",
	"shop_quote_finalize",
	"shop_quote_clear",
	"shop_order_place",
	"shop_order_customer_claimed_paid",
	"shop_order_lookup",
	"shop_notify_admin",
	// Reseller-agent tools (Phase 2 + 3 — middleman flow).
	"shop_reseller_price_quote",
	"shop_reseller_supplier_query",
	// Purchase / import-goods (nhập hàng) tools.
	"shop_fx_get",
	"shop_fx_set_snapshot",
	"shop_suppliers_list",
	"shop_supplier_upsert",
	"shop_nhaphang_price",
	"shop_nhaphang_commit",
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

	// Shop tools use generic shop_* names so agents are not tied to one brand.
	// The package still holds the current Tuyet Truong adapter until the client
	// is promoted to a tenant/shop-config resolver.
	reg.RegisterWithMetadata(newRenamedTool("shop_product_search", NewProductSearchTool(client, RoleAdmin)), readOnly())
	reg.RegisterWithMetadata(newRenamedTool("shop_product_get", NewProductGetTool(client)), readOnly())
	reg.RegisterWithMetadata(newRenamedTool("shop_product_create", NewProductCreateTool(client)), mutating())
	reg.RegisterWithMetadata(newRenamedTool("shop_product_update", NewProductUpdateTool(client)), mutating())
	reg.RegisterWithMetadata(newRenamedTool("shop_variant_update", NewVariantUpdateTool(client)), mutating())
	reg.RegisterWithMetadata(newRenamedTool("shop_product_delete", NewProductDeleteTool(client)), mutating())
	reg.RegisterWithMetadata(newRenamedTool("shop_product_lookup_existing", NewProductLookupExistingTool(client)), readOnly())
	reg.RegisterWithMetadata(newRenamedTool("shop_product_draft_from_extracted", NewProductDraftFromExtractedTool(client)), mutating())
	reg.RegisterWithMetadata(newRenamedTool("shop_order_list", NewOrderListTool(client)), readOnly())
	reg.RegisterWithMetadata(newRenamedTool("shop_order_update_status", NewOrderUpdateStatusTool(client)), mutating())

	// Identity resolution — used by either mode at conversation start to know
	// whether the incoming chat sender is admin/staff/customer/unknown.
	reg.RegisterWithMetadata(newRenamedTool("shop_whoami", NewWhoAmITool(client)), readOnly())

	// Sales tools — usable from sales-agent. Quote tools mutate in-process
	// draft state; order_place writes to the store; the rest read or notify.
	reg.RegisterWithMetadata(newRenamedTool("shop_quote_add_item", NewQuoteAddItemTool(client)), mutating())
	reg.RegisterWithMetadata(newRenamedTool("shop_quote_remove_item", NewQuoteRemoveItemTool()), mutating())
	reg.RegisterWithMetadata(newRenamedTool("shop_quote_view", NewQuoteViewTool()), readOnly())
	reg.RegisterWithMetadata(newRenamedTool("shop_quote_set_customer", NewQuoteSetCustomerTool()), mutating())
	reg.RegisterWithMetadata(newRenamedTool("shop_quote_finalize", NewQuoteFinalizeTool(client)), readOnly())
	reg.RegisterWithMetadata(newRenamedTool("shop_quote_clear", NewQuoteClearTool()), mutating())
	reg.RegisterWithMetadata(newRenamedTool("shop_order_place", NewOrderPlaceTool(client)), mutating())
	reg.RegisterWithMetadata(newRenamedTool("shop_order_customer_claimed_paid", NewOrderCustomerClaimedPaidTool(client)), mutating())
	reg.RegisterWithMetadata(newRenamedTool("shop_order_lookup", NewOrderLookupTool(client)), readOnly())
	reg.RegisterWithMetadata(newRenamedTool("shop_notify_admin", NewNotifyAdminTool()), readOnly())

	// Sales bot also needs product_search read-only — register a sales-role
	// variant under a distinct alias so the agent allow lists can differ.
	reg.RegisterWithMetadata(newRenamedTool("shop_catalog_search", NewProductSearchTool(client, RoleSales)), readOnly())

	// Reseller-agent tools (middleman: catalog → supplier → markup → handoff).
	// price_quote is read-only; supplier_query writes an inquiry row server-side.
	reg.RegisterWithMetadata(newRenamedTool("shop_reseller_price_quote", NewResellerPriceQuoteTool(client)), readOnly())
	reg.RegisterWithMetadata(newRenamedTool("shop_reseller_supplier_query", NewResellerSupplierQueryTool(client)), mutating())

	// Purchase / import-goods (nhập hàng) tools. fx_get / suppliers_list /
	// nhaphang_price only read or compute; fx_set_snapshot records the day's
	// confirmed rate; nhaphang_commit creates inactive drafts / adds stock.
	reg.RegisterWithMetadata(newRenamedTool("shop_fx_get", NewFxGetTool(client)), readOnly())
	reg.RegisterWithMetadata(newRenamedTool("shop_fx_set_snapshot", NewFxSetSnapshotTool(client)), mutating())
	reg.RegisterWithMetadata(newRenamedTool("shop_suppliers_list", NewSuppliersListTool(client)), readOnly())
	reg.RegisterWithMetadata(newRenamedTool("shop_supplier_upsert", NewSupplierUpsertTool(client)), mutating())
	reg.RegisterWithMetadata(newRenamedTool("shop_nhaphang_price", NewNhapHangPriceTool(client)), readOnly())
	reg.RegisterWithMetadata(newRenamedTool("shop_nhaphang_commit", NewNhapHangCommitTool(client)), mutating())

	reg.RegisterToolGroup("shop", shopToolNames)

	slog.Info("shop tools registered",
		"base", client.baseURL,
		"admin_tools", []string{
			"shop_product_search", "shop_product_get",
			"shop_product_create", "shop_product_update",
			"shop_variant_update", "shop_product_delete",
			"shop_product_lookup_existing", "shop_product_draft_from_extracted",
			"shop_order_list", "shop_order_update_status",
		},
		"shared_tools", []string{"shop_whoami"},
		"sales_tools", []string{
			"shop_catalog_search",
			"shop_quote_add_item", "shop_quote_remove_item", "shop_quote_view",
			"shop_quote_set_customer", "shop_quote_finalize", "shop_quote_clear",
			"shop_order_place", "shop_order_customer_claimed_paid",
			"shop_order_lookup", "shop_notify_admin",
		},
		"reseller_tools", []string{
			"shop_reseller_price_quote",
			"shop_reseller_supplier_query",
		},
		"purchase_tools", []string{
			"shop_fx_get", "shop_fx_set_snapshot",
			"shop_suppliers_list", "shop_supplier_upsert",
			"shop_nhaphang_price", "shop_nhaphang_commit",
		})
}
