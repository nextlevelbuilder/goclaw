package tuyettruong

import (
	"context"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// OrderUpdateStatusTool wraps POST /api/v1/admin/orders/{id}/status. The most
// common admin action: confirming payment received (→ paid), starting shipping
// (→ shipping), marking delivered (→ done), or cancelling. Cancellation
// requires explicit confirm_token to prevent fat-finger.
type OrderUpdateStatusTool struct {
	client *Client
}

func NewOrderUpdateStatusTool(c *Client) *OrderUpdateStatusTool {
	return &OrderUpdateStatusTool{client: c}
}

func (t *OrderUpdateStatusTool) Name() string { return "tt_order_update_status" }
func (t *OrderUpdateStatusTool) Description() string {
	return "Change an order's status. Valid transitions: pending|awaiting_confirmation → paid|cancelled; paid → shipping|cancelled; shipping → done|cancelled. For status='cancelled', you MUST get user confirmation and pass confirm_token='HUY-<order_id>'."
}
func (t *OrderUpdateStatusTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"order_id":      map[string]any{"type": "string"},
			"status":        map[string]any{"type": "string", "enum": []string{"pending", "awaiting_confirmation", "paid", "shipping", "done", "cancelled"}},
			"confirm_token": map[string]any{"type": "string", "description": "Required when status=cancelled. Must equal 'HUY-<order_id>'."},
		},
		"required": []string{"order_id", "status"},
	}
}

func (t *OrderUpdateStatusTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	orderID, _ := args["order_id"].(string)
	status, _ := args["status"].(string)
	if orderID == "" || status == "" {
		return errorResult(fmt.Errorf("order_id and status required"))
	}
	if status == "cancelled" {
		token, _ := args["confirm_token"].(string)
		expected := "HUY-" + orderID
		if !strings.EqualFold(strings.TrimSpace(token), expected) {
			return errorResult(fmt.Errorf("cancelling requires confirm_token=%q (got %q)", expected, token))
		}
	}
	var out map[string]any
	path := fmt.Sprintf("/api/v1/admin/orders/%s/status", orderID)
	if err := t.client.Do(ctx, RoleAdmin, "POST", path, map[string]any{"status": status}, &out); err != nil {
		return errorResult(err)
	}
	return jsonResult(out)
}
