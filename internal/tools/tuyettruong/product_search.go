package tuyettruong

import (
	"context"
	"fmt"
	"net/url"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// ProductSearchTool wraps GET /api/v1/store/products-search. Read-only,
// returns minimal {slug, name, brand}. Use ProductGetTool for full detail.
type ProductSearchTool struct {
	client *Client
	role   BotRole
}

func NewProductSearchTool(c *Client, role BotRole) *ProductSearchTool {
	return &ProductSearchTool{client: c, role: role}
}

func (t *ProductSearchTool) Name() string { return "tt_product_search" }
func (t *ProductSearchTool) Description() string {
	return "Search products in the tuyettruong catalog by keyword. Returns up to 20 matches with slug/name/brand. Use this before tt_product_get when you only have a fuzzy name."
}
func (t *ProductSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"q":     map[string]any{"type": "string", "description": "Search keyword (Vietnamese OK)"},
			"limit": map[string]any{"type": "integer", "description": "Max results (1-50, default 20)"},
		},
		"required": []string{"q"},
	}
}

func (t *ProductSearchTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	q, _ := args["q"].(string)
	if q == "" {
		return errorResult(fmt.Errorf("q required"))
	}
	limit := 20
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
		if limit > 50 {
			limit = 50
		}
	}
	path := fmt.Sprintf("/api/v1/store/products-search?q=%s&limit=%d", url.QueryEscape(q), limit)
	var out map[string]any
	if err := t.client.Do(ctx, t.role, "GET", path, nil, &out); err != nil {
		return errorResult(err)
	}
	return jsonResult(out)
}
