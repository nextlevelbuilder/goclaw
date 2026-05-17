package tuyettruong

import (
	"context"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// VariantUpdateTool wraps PATCH /api/v1/admin/products/{slug}/variants/{sku}.
// Use for price/stock/attributes changes — the most common admin action.
type VariantUpdateTool struct {
	client *Client
}

func NewVariantUpdateTool(c *Client) *VariantUpdateTool { return &VariantUpdateTool{client: c} }

func (t *VariantUpdateTool) Name() string { return "tt_variant_update" }
func (t *VariantUpdateTool) Description() string {
	return "Update a single variant by (slug, sku). Common use: change price, stock, or active flag. Pass only the fields you want to change inside the 'patch' object."
}
func (t *VariantUpdateTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"slug": map[string]any{"type": "string"},
			"sku":  map[string]any{"type": "string"},
			"patch": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"price":      map[string]any{"type": "number"},
					"cost":       map[string]any{"type": "number"},
					"stock":      map[string]any{"type": "integer"},
					"attributes": map[string]any{"type": "object"},
					"active":     map[string]any{"type": "boolean"},
					"barcode":    map[string]any{"type": "string"},
					"weight":     map[string]any{"type": "number"},
				},
			},
		},
		"required": []string{"slug", "sku", "patch"},
	}
}

func (t *VariantUpdateTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	slug, _ := args["slug"].(string)
	sku, _ := args["sku"].(string)
	patch, _ := args["patch"].(map[string]any)
	if slug == "" || sku == "" || patch == nil {
		return errorResult(fmt.Errorf("slug, sku, and patch required"))
	}
	var out map[string]any
	path := fmt.Sprintf("/api/v1/admin/products/%s/variants/%s", slug, sku)
	if err := t.client.Do(ctx, RoleAdmin, "PATCH", path, patch, &out); err != nil {
		return errorResult(err)
	}
	return jsonResult(out)
}
