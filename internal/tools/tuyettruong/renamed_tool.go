package tuyettruong

import (
	"context"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

type renamedTool struct {
	tools.Tool
	name string
}

func newRenamedTool(name string, tool tools.Tool) tools.Tool {
	return renamedTool{Tool: tool, name: name}
}

func (t renamedTool) Name() string {
	return t.name
}

func (t renamedTool) Description() string {
	desc := t.Tool.Description()
	replacements := map[string]string{
		"tt_whoami":                         "shop_whoami",
		"tt_product_search":                 "shop_product_search",
		"tt_product_get":                    "shop_product_get",
		"tt_product_create":                 "shop_product_create",
		"tt_product_update":                 "shop_product_update",
		"tt_variant_update":                 "shop_variant_update",
		"tt_product_delete":                 "shop_product_delete",
		"tt_product_lookup_existing":        "shop_product_lookup_existing",
		"tt_product_draft_from_extracted":   "shop_product_draft_from_extracted",
		"tt_order_list":                     "shop_order_list",
		"tt_order_update_status":            "shop_order_update_status",
		"sales_product_search":              "shop_catalog_search",
		"quote_add_item":                    "shop_quote_add_item",
		"quote_remove_item":                 "shop_quote_remove_item",
		"quote_view":                        "shop_quote_view",
		"quote_set_customer":                "shop_quote_set_customer",
		"quote_finalize":                    "shop_quote_finalize",
		"quote_clear":                       "shop_quote_clear",
		"order_place":                       "shop_order_place",
		"order_customer_claimed_paid":       "shop_order_customer_claimed_paid",
		"order_lookup":                      "shop_order_lookup",
		"notify_admin":                      "shop_notify_admin",
		"tt_fx_get":                         "shop_fx_get",
		"tt_fx_set_snapshot":                "shop_fx_set_snapshot",
		"tt_suppliers_list":                 "shop_suppliers_list",
		"tt_nhaphang_price":                 "shop_nhaphang_price",
		"tt_nhaphang_commit":                "shop_nhaphang_commit",
		"tuyettruong catalog":               "shop catalog",
		"tuyettruong store":                 "shop store",
		"tuyettruong API":                   "shop API",
	}
	for oldName, newName := range replacements {
		desc = strings.ReplaceAll(desc, oldName, newName)
	}
	return desc
}

func (t renamedTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	return t.Tool.Execute(ctx, args)
}
