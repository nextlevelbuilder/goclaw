package tuyettruong

import (
	"context"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// ProductCreateTool wraps POST /api/v1/admin/products. Payload is passed
// through verbatim — schema validation happens on the Next.js side
// (ProductCreateSchema). Admin-only.
type ProductCreateTool struct {
	client *Client
}

func NewProductCreateTool(c *Client) *ProductCreateTool { return &ProductCreateTool{client: c} }

func (t *ProductCreateTool) Name() string { return "tt_product_create" }
func (t *ProductCreateTool) Description() string {
	return "Create a new product with at least 1 variant. Fields: name (required), brand, categoryPath (array), description, images (array of URLs), variants (array of {sku, attributes, price, stock, images, active, weight}). Variants must have unique SKUs across the whole store."
}
func (t *ProductCreateTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":         map[string]any{"type": "string"},
			"brand":        map[string]any{"type": "string"},
			"categoryPath": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"description":  map[string]any{"type": "string"},
			"images":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"active":       map[string]any{"type": "boolean", "description": "default true"},
			"parentSku":    map[string]any{"type": "string", "description": "Optional KiotViet parent SKU"},
			"variants": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"sku":        map[string]any{"type": "string"},
						"attributes": map[string]any{"type": "object", "description": "e.g. {COLOR, SIZE, DUNG_TICH}"},
						"price":      map[string]any{"type": "number"},
						"cost":       map[string]any{"type": "number", "description": "Admin-only cost"},
						"stock":      map[string]any{"type": "integer"},
						"images":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"active":     map[string]any{"type": "boolean"},
						"weight":     map[string]any{"type": "number"},
					},
					"required": []string{"sku", "price", "stock"},
				},
			},
		},
		"required": []string{"name", "variants"},
	}
}

func (t *ProductCreateTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	if _, ok := args["name"]; !ok {
		return errorResult(fmt.Errorf("name required"))
	}
	var out map[string]any
	if err := t.client.Do(ctx, RoleAdmin, "POST", "/api/v1/admin/products", args, &out); err != nil {
		return errorResult(err)
	}
	return jsonResult(out)
}
