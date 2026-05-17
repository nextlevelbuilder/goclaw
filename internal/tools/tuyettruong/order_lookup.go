package tuyettruong

import (
	"context"
	"fmt"
	"net/url"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// OrderLookupTool wraps GET /api/v1/store/order-lookup. Public endpoint.
// Customer-facing: "đơn em đâu rồi" → use shortCode + accessToken from the
// original order_place response, or just shortCode if anh's admin acts as
// a generous proxy.
type OrderLookupTool struct{ client *Client }

func NewOrderLookupTool(c *Client) *OrderLookupTool { return &OrderLookupTool{client: c} }

func (t *OrderLookupTool) Name() string { return "order_lookup" }
func (t *OrderLookupTool) Description() string {
	return "Look up an order by its shortCode + access token. Customer-facing — use to answer 'đơn em đâu rồi' type questions. The token comes from the order_place response."
}
func (t *OrderLookupTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"short_code":   map[string]any{"type": "string"},
			"access_token": map[string]any{"type": "string"},
		},
		"required": []string{"short_code", "access_token"},
	}
}
func (t *OrderLookupTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	code, _ := args["short_code"].(string)
	token, _ := args["access_token"].(string)
	if code == "" || token == "" {
		return errorResult(fmt.Errorf("short_code and access_token required"))
	}
	q := url.Values{}
	q.Set("code", code)
	q.Set("token", token)
	var out map[string]any
	if err := t.client.Do(ctx, RoleSales, "GET", "/api/v1/store/order-lookup?"+q.Encode(), nil, &out); err != nil {
		return errorResult(err)
	}
	return jsonResult(out)
}
