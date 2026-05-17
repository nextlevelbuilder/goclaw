package tuyettruong

import (
	"context"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// ProductUpdateTool wraps PATCH /api/v1/admin/products/{slug}. Any subset of
// product-level fields. For variant updates use VariantUpdateTool.
type ProductUpdateTool struct {
	client *Client
}

func NewProductUpdateTool(c *Client) *ProductUpdateTool { return &ProductUpdateTool{client: c} }

func (t *ProductUpdateTool) Name() string { return "tt_product_update" }
func (t *ProductUpdateTool) Description() string {
	return "Update product metadata (name, brand, description, etc.). Pass only the fields you want to change. To change a variant's price/stock, use tt_variant_update."
}
func (t *ProductUpdateTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"slug": map[string]any{"type": "string"},
			"patch": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":         map[string]any{"type": "string"},
					"brand":        map[string]any{"type": "string"},
					"categoryPath": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"description":  map[string]any{"type": "string"},
					"active":       map[string]any{"type": "boolean"},
				},
			},
		},
		"required": []string{"slug", "patch"},
	}
}

func (t *ProductUpdateTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	slug, _ := args["slug"].(string)
	patch, _ := args["patch"].(map[string]any)
	if slug == "" || patch == nil {
		return errorResult(fmt.Errorf("slug and patch required"))
	}
	var out map[string]any
	if err := t.client.Do(ctx, RoleAdmin, "PATCH", "/api/v1/admin/products/"+slug, patch, &out); err != nil {
		return errorResult(err)
	}
	return jsonResult(out)
}
