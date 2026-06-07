package tuyettruong

import (
	"context"
	"fmt"
	"net/url"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// ResellerPriceQuoteTool computes the retail VND price from a wholesale price
// by applying the markup rule resolved by tuyettruong's pricing engine. Wraps
// GET /api/v1/reseller/markup — see endpoint for resolution priority.
//
// Used by reseller-agent after a supplier-query returns a wholesale_vnd. The
// agent should NOT compute markup itself — pricing rules live server-side so
// admin can tune them without redeploying the agent.
type ResellerPriceQuoteTool struct {
	client *Client
}

func NewResellerPriceQuoteTool(c *Client) *ResellerPriceQuoteTool {
	return &ResellerPriceQuoteTool{client: c}
}

func (t *ResellerPriceQuoteTool) Name() string { return "reseller_price_quote" }
func (t *ResellerPriceQuoteTool) Description() string {
	return "Tính giá lẻ VND từ giá sỉ + markup rule (resolved server-side). Trả về retail_vnd, markup_pct, formatted text. Gọi sau khi supplier báo wholesale_vnd hoặc khi cần báo giá lẻ cho khách. KHÔNG tự cộng tay — markup rule có thể đổi theo category/supplier."
}
func (t *ResellerPriceQuoteTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"wholesale_vnd": map[string]any{
				"type":        "number",
				"description": "Wholesale price in VND (no decimals)",
			},
			"category_slug": map[string]any{
				"type":        "string",
				"description": "Optional product category slug for per-category markup override",
			},
			"supplier_id": map[string]any{
				"type":        "string",
				"description": "Optional supplier ID for per-supplier markup override",
			},
		},
		"required": []string{"wholesale_vnd"},
	}
}

func (t *ResellerPriceQuoteTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	wholesale, _ := args["wholesale_vnd"].(float64)
	if wholesale < 0 {
		return errorResult(fmt.Errorf("wholesale_vnd must be non-negative"))
	}
	categorySlug, _ := args["category_slug"].(string)
	supplierID, _ := args["supplier_id"].(string)

	q := url.Values{}
	q.Set("wholesale_vnd", fmt.Sprintf("%d", int64(wholesale)))
	if categorySlug != "" {
		q.Set("category_slug", categorySlug)
	}
	if supplierID != "" {
		q.Set("supplier_id", supplierID)
	}

	var out map[string]any
	path := "/api/v1/reseller/markup?" + q.Encode()
	if err := t.client.Do(ctx, RoleAdmin, "GET", path, nil, &out); err != nil {
		return errorResult(err)
	}
	return jsonResult(out)
}
