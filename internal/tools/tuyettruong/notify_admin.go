package tuyettruong

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// NotifyAdminTool emits a notification meant for the admin operator. In v1
// this is a structured log entry — the admin Telegram bot polls
// `/api/v1/admin/orders?status=awaiting_confirmation` periodically (or admin
// asks "what's new") to see actionable orders. A future push-based notify
// (sales bot → admin bot via goclaw message bus) is tracked as P5 polish.
//
// The tool still returns success so the sales bot's flow doesn't break — it
// just logs locally for now. The customer-facing UX is unaffected.
type NotifyAdminTool struct{}

func NewNotifyAdminTool() *NotifyAdminTool { return &NotifyAdminTool{} }

func (t *NotifyAdminTool) Name() string { return "notify_admin" }
func (t *NotifyAdminTool) Description() string {
	return "Notify the shop admin about a noteworthy event (new order, customer claim, escalation). In v1 this writes a structured log; the admin will see actionable orders by asking the admin bot or opening the admin UI. Always call this after order_place and order_customer_claimed_paid so we have a clean audit trail."
}
func (t *NotifyAdminTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"event":    map[string]any{"type": "string", "description": "Short event name: order_placed | customer_claimed_paid | escalation | other"},
			"order_id": map[string]any{"type": "string", "description": "Optional related order id"},
			"summary":  map[string]any{"type": "string", "description": "1-2 sentence summary for the admin to see"},
		},
		"required": []string{"event", "summary"},
	}
}
func (t *NotifyAdminTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	event, _ := args["event"].(string)
	orderID, _ := args["order_id"].(string)
	summary, _ := args["summary"].(string)
	if event == "" || summary == "" {
		return errorResult(fmt.Errorf("event and summary required"))
	}
	slog.Info("tuyettruong.notify_admin",
		"event", event,
		"order_id", orderID,
		"summary", summary,
		"session_key", tools.ToolSessionKeyFromCtx(ctx),
	)
	return jsonResult(map[string]any{"ok": true, "delivery": "logged_v1"})
}
