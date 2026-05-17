package tuyettruong

import (
	"context"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// ProductGetTool wraps GET /api/v1/admin/products/{slug}. Full product +
// variants (price, stock, attributes). Admin-only because it exposes cost.
type ProductGetTool struct {
	client *Client
}

func NewProductGetTool(c *Client) *ProductGetTool { return &ProductGetTool{client: c} }

func (t *ProductGetTool) Name() string { return "tt_product_get" }
func (t *ProductGetTool) Description() string {
	return "Get full product detail by slug (includes all variants with price, stock, attributes, images). Always call tt_product_search first if you don't have the exact slug."
}
func (t *ProductGetTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"slug": map[string]any{"type": "string", "description": "Product slug, e.g. 'ao-tuyet-trang'"},
		},
		"required": []string{"slug"},
	}
}

func (t *ProductGetTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	slug, _ := args["slug"].(string)
	if slug == "" {
		return errorResult(fmt.Errorf("slug required"))
	}
	var out map[string]any
	if err := t.client.Do(ctx, RoleAdmin, "GET", "/api/v1/admin/products/"+slug, nil, &out); err != nil {
		return errorResult(err)
	}
	return jsonResult(out)
}
