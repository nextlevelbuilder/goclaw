package tuyettruong

import (
	"context"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// ResellerSupplierQueryTool fires an inquiry to a supplier and records it for
// async resume. Wraps POST /api/v1/reseller/supplier-inquiries. The endpoint
// only PERSISTS the inquiry — actual outbound message dispatch (sending to
// supplier's Zalo/Telegram) is the agent's responsibility via the existing
// `message` tool against the supplier's channel.
//
// Returns immediately with inquiry_id. The on_supplier_reply hook will wake
// the customer session when the supplier replies (Phase 6 — webhook TBD).
//
// State machine: pending → replied | timeout | cancelled.
type ResellerSupplierQueryTool struct {
	client *Client
}

func NewResellerSupplierQueryTool(c *Client) *ResellerSupplierQueryTool {
	return &ResellerSupplierQueryTool{client: c}
}

func (t *ResellerSupplierQueryTool) Name() string { return "reseller_supplier_query" }
func (t *ResellerSupplierQueryTool) Description() string {
	return "Gửi inquiry sang supplier (Zalo/Telegram của supplier). Async — KHÔNG chờ reply trong call này. Trả inquiry_id để track. Khi supplier reply, customer session sẽ tự wake. Gọi khi: sản phẩm chưa có trong catalog HOẶC cần check stock/price real-time. Đặt timeout_minutes phù hợp (default 30)."
}
func (t *ResellerSupplierQueryTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"supplier_slug": map[string]any{
				"type":        "string",
				"description": "Supplier slug từ DB (vd 'trang', 'kim-anh-luxury')",
			},
			"product_query": map[string]any{
				"type":        "string",
				"description": "Câu hỏi gửi supplier (vd 'iconic swan blue còn hàng không, giá sỉ bao nhiêu')",
			},
			"customer_session_key": map[string]any{
				"type":        "string",
				"description": "Goclaw session key của khách hiện tại (để wake khi supplier reply)",
			},
			"customer_channel": map[string]any{
				"type":        "string",
				"description": "Kênh khách: zalo | pancake (IG) | telegram",
			},
			"attached_image_url": map[string]any{
				"type":        "string",
				"description": "Optional: URL ảnh để forward sang supplier",
			},
			"timeout_minutes": map[string]any{
				"type":        "integer",
				"description": "Phút trước khi auto-escalate (default 30, max 360)",
			},
		},
		"required": []string{"supplier_slug", "product_query", "customer_session_key", "customer_channel"},
	}
}

func (t *ResellerSupplierQueryTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	supplierSlug, _ := args["supplier_slug"].(string)
	productQuery, _ := args["product_query"].(string)
	sessionKey, _ := args["customer_session_key"].(string)
	customerChannel, _ := args["customer_channel"].(string)
	imageURL, _ := args["attached_image_url"].(string)
	timeoutFloat, _ := args["timeout_minutes"].(float64)

	if supplierSlug == "" || productQuery == "" || sessionKey == "" || customerChannel == "" {
		return errorResult(fmt.Errorf("supplier_slug, product_query, customer_session_key, customer_channel required"))
	}

	body := map[string]any{
		"supplier_slug":        supplierSlug,
		"product_query":        productQuery,
		"customer_session_key": sessionKey,
		"customer_channel":     customerChannel,
	}
	if imageURL != "" {
		body["attached_image_url"] = imageURL
	}
	if timeoutFloat > 0 {
		body["timeout_minutes"] = int(timeoutFloat)
	}

	var out map[string]any
	if err := t.client.Do(ctx, RoleAdmin, "POST", "/api/v1/reseller/supplier-inquiries", body, &out); err != nil {
		return errorResult(err)
	}
	return jsonResult(out)
}
