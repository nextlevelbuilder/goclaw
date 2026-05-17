package tuyettruong

import (
	"context"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// OrderCustomerClaimedPaidTool: customer says "đã CK". Moves order from
// pending → awaiting_confirmation and records customerConfirmedAt. Admin will
// verify the bank statement separately and either confirm (→ paid) or reject
// (→ back to pending). Permission "order.customer_claim" lets the sales bot's
// customer-role actor call this.
type OrderCustomerClaimedPaidTool struct{ client *Client }

func NewOrderCustomerClaimedPaidTool(c *Client) *OrderCustomerClaimedPaidTool {
	return &OrderCustomerClaimedPaidTool{client: c}
}

func (t *OrderCustomerClaimedPaidTool) Name() string { return "order_customer_claimed_paid" }
func (t *OrderCustomerClaimedPaidTool) Description() string {
	return "Call when the customer confirms they have completed the bank transfer. Moves the order to 'awaiting_confirmation' status. Admin will verify and finalize. Do not call this until the customer explicitly says 'đã CK' / 'đã chuyển' / similar."
}
func (t *OrderCustomerClaimedPaidTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"order_id": map[string]any{"type": "string"},
		},
		"required": []string{"order_id"},
	}
}
func (t *OrderCustomerClaimedPaidTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	orderID, _ := args["order_id"].(string)
	if orderID == "" {
		return errorResult(fmt.Errorf("order_id required"))
	}
	var out map[string]any
	path := fmt.Sprintf("/api/v1/admin/orders/%s/customer-claimed-paid", orderID)
	if err := t.client.Do(ctx, RoleSales, "POST", path, nil, &out); err != nil {
		return errorResult(err)
	}
	return jsonResult(out)
}
