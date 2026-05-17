package tuyettruong

import (
	"context"
	"fmt"
	"net/url"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// OrderListTool wraps GET /api/v1/admin/orders. Filterable by status.
type OrderListTool struct {
	client *Client
}

func NewOrderListTool(c *Client) *OrderListTool { return &OrderListTool{client: c} }

func (t *OrderListTool) Name() string { return "tt_order_list" }
func (t *OrderListTool) Description() string {
	return "List orders, newest first. Optional status filter: pending|awaiting_confirmation|paid|shipping|done|cancelled. Pending/awaiting_confirmation are the actionable ones for admin."
}
func (t *OrderListTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]any{
				"type":        "string",
				"description": "Filter by status (optional)",
				"enum":        []string{"pending", "awaiting_confirmation", "paid", "shipping", "done", "cancelled"},
			},
			"limit": map[string]any{"type": "integer", "description": "Max results (1-100, default 50)"},
		},
	}
}

func (t *OrderListTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	status, _ := args["status"].(string)
	limit := 50
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
		if limit > 100 {
			limit = 100
		}
	}
	q := url.Values{}
	q.Set("limit", fmt.Sprintf("%d", limit))
	if status != "" {
		q.Set("status", status)
	}
	var out map[string]any
	if err := t.client.Do(ctx, RoleAdmin, "GET", "/api/v1/admin/orders?"+q.Encode(), nil, &out); err != nil {
		return errorResult(err)
	}
	return jsonResult(out)
}
