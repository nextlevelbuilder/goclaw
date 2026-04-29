package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/mymmrac/telego"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// handleExecApprovalCallback handles inline keyboard button presses for exec approval.
// Callback data format: "ea:{approvalID}:{decision}" where decision is "allow", "always", or "deny".
func (c *Channel) handleExecApprovalCallback(ctx context.Context, query *telego.CallbackQuery) {
	if c.execApprovalMgr == nil {
		c.bot.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID,
			Text:            "Approval system not available",
		})
		return
	}

	data := strings.TrimPrefix(query.Data, "ea:")
	parts := strings.SplitN(data, ":", 2)
	if len(parts) != 2 {
		c.bot.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID,
			Text:            "Invalid approval data",
		})
		return
	}
	approvalID, decision := parts[0], parts[1]

	var d tools.ApprovalDecision
	switch decision {
	case "allow":
		d = tools.ApprovalAllowOnce
	case "always":
		d = tools.ApprovalAllowAlways
	case "deny":
		d = tools.ApprovalDeny
	default:
		c.bot.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
			CallbackQueryID: query.ID,
			Text:            "Invalid decision",
		})
		return
	}

	err := c.execApprovalMgr.Resolve(approvalID, d)
	text := "Approved"
	if d == tools.ApprovalAllowAlways {
		text = "Approved (always allow)"
	}
	if d == tools.ApprovalDeny {
		text = "Denied"
	}
	if err != nil {
		text = fmt.Sprintf("Error: %s", err.Error())
	}

	c.bot.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
		CallbackQueryID: query.ID,
		Text:            text,
	})
}
