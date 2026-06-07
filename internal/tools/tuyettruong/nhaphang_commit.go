package tuyettruong

import (
	"context"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// NhapHangCommitTool wraps POST /api/v1/admin/nhap-hang/commit. Mutating:
// for NEW items (no existingSlug) it creates an INACTIVE draft product;
// for EXISTING items (existingSlug set) it ADDS STOCK only and NEVER changes
// the price. The retail price in each item MUST already be confirmed by
// admin/supplier — this tool never auto-publishes. Customers never see cost
// or FX.
type NhapHangCommitTool struct {
	client *Client
}

func NewNhapHangCommitTool(c *Client) *NhapHangCommitTool { return &NhapHangCommitTool{client: c} }

func (t *NhapHangCommitTool) Name() string { return "tt_nhaphang_commit" }
func (t *NhapHangCommitTool) Description() string {
	return "Commit priced import items to the catalog. New items (no existingSlug) are created as INACTIVE drafts for admin review — never published automatically. Existing items (existingSlug set, from tt_nhaphang_price's existingMatch) only get STOCK ADDED; their retail price is NOT changed. The retailVnd you pass MUST have been confirmed by admin/supplier first (use tt_nhaphang_price to compute, then confirm). Currency is per origin: AUD=Úc, KRW=Hàn, JPY=Nhật, EUR=Đức, USD=Mỹ. Provide `items` (array of { name, costAmount, costCurrency, fxRate, costVnd, retailVnd, qty, weightKg?, supplierId?, existingSlug? }). Returns { results: [{ name, status: 'created'|'stock_added'|'error', slug?, adminUrl?, costDelta?, error? }] }. Cost/FX are internal only — never shown to customers."
}

func (t *NhapHangCommitTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"items": map[string]any{
				"type":        "array",
				"description": "Items to commit, each priced + confirmed.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":         map[string]any{"type": "string", "description": "Product name."},
						"costAmount":   map[string]any{"type": "number", "description": "Per-unit cost in the bill currency (the PAID price)."},
						"costCurrency": map[string]any{"type": "string", "description": "Bill currency: AUD|KRW|JPY|EUR|USD."},
						"fxRate":       map[string]any{"type": "number", "description": "Confirmed FX rate used (VND per 1 unit)."},
						"costVnd":      map[string]any{"type": "number", "description": "Landed cost per unit in VND (from tt_nhaphang_price)."},
						"retailVnd":    map[string]any{"type": "number", "description": "Confirmed retail price per unit in VND. New items only — ignored for existing items (price unchanged)."},
						"qty":          map[string]any{"type": "integer", "description": "Quantity to add to stock."},
						"weightKg":     map[string]any{"type": "number", "description": "Per-unit weight in kg (optional)."},
						"supplierId":   map[string]any{"type": "string", "description": "Supplier id from tt_suppliers_list (optional)."},
						"existingSlug": map[string]any{"type": "string", "description": "Set to existingMatch.slug to ADD STOCK to an existing product (price untouched). Omit to create a new INACTIVE draft."},
						"images":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "NEW items only: product/manufacturer image URLs you found via web search (bills carry no photos). Ignored for existing items."},
						"categoryPath": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "NEW items only: suggested Vietnamese category path, e.g. ['Sức khoẻ','Trẻ em'] or ['Thực phẩm','Hạt dinh dưỡng']. Ignored for existing items."},
					},
					"required": []string{"name", "costAmount", "costCurrency", "fxRate", "costVnd", "retailVnd", "qty"},
				},
			},
		},
		"required": []string{"items"},
	}
}

func (t *NhapHangCommitTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	items, ok := args["items"].([]any)
	if !ok || len(items) == 0 {
		return errorResult(fmt.Errorf("items required (array of { name, costAmount, costCurrency, fxRate, costVnd, retailVnd, qty, weightKg?, supplierId?, existingSlug? })"))
	}
	var out map[string]any
	if err := t.client.Do(ctx, RoleAdmin, "POST", "/api/v1/admin/nhap-hang/commit", args, &out); err != nil {
		return errorResult(err)
	}
	return jsonResult(out)
}
