package tuyettruong

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// OrderPlaceTool wraps POST /api/v1/store/orders. Reads the draft from
// session state, builds the order payload, and submits. Tuyettruong returns
// shortCode + accessToken which the sales bot relays to the customer along
// with CK instructions.
//
// Idempotency: payload hash is computed and stashed; if order_place is called
// twice in the same session with identical draft, we short-circuit returning
// the prior result. This protects against LLM-driven duplicate submits.
type OrderPlaceTool struct{ client *Client }

func NewOrderPlaceTool(c *Client) *OrderPlaceTool { return &OrderPlaceTool{client: c} }

func (t *OrderPlaceTool) Name() string { return "order_place" }
func (t *OrderPlaceTool) Description() string {
	return "Submit the current draft as a real order. The draft must have at least 1 item AND customer.name/phone/address filled. On success, clears the draft and returns the order id + shortCode + access token. Customer should then transfer money with shortCode as CK content."
}
func (t *OrderPlaceTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

// orderResults caches the result per session+payload-hash for idempotency.
// Lives next to drafts in-memory — drops on goclaw restart.
type orderRecord struct{ payloadHash string; response map[string]any }

var (
	orderResults = map[string]orderRecord{}
)

func (t *OrderPlaceTool) Execute(ctx context.Context, _ map[string]any) *tools.Result {
	sessionKey := tools.ToolSessionKeyFromCtx(ctx)
	d := drafts.load(sessionKey)
	if d == nil || len(d.Items) == 0 {
		return errorResult(fmt.Errorf("draft is empty"))
	}
	if d.Customer.Name == "" || d.Customer.Phone == "" || d.Customer.Address == "" {
		return errorResult(fmt.Errorf("customer name/phone/address required — use quote_set_customer first"))
	}

	items := make([]map[string]any, 0, len(d.Items))
	for _, it := range d.Items {
		items = append(items, map[string]any{
			"productId":         it.ProductSlug, // tuyettruong accepts slug as productId for create
			"productSlug":       it.ProductSlug,
			"productName":       it.ProductName,
			"variantSku":        it.VariantSku,
			"variantAttributes": it.VariantAttributes,
			"unitPrice":         it.UnitPriceSnapshot,
			"qty":               it.Qty,
			"imageUrl":          it.ImageURL,
		})
	}
	body := map[string]any{
		"items":    items,
		"customer": map[string]any{
			"name":    d.Customer.Name,
			"phone":   d.Customer.Phone,
			"email":   firstNonEmpty(d.Customer.Email, ""),
			"address": d.Customer.Address,
			"note":    d.Customer.Note,
		},
	}

	// Idempotency check
	payloadJSON, _ := json.Marshal(body)
	hash := sha256.Sum256(payloadJSON)
	hashStr := hex.EncodeToString(hash[:])
	if prior, ok := orderResults[sessionKey]; ok && prior.payloadHash == hashStr {
		return jsonResult(map[string]any{
			"idempotent_replay": true,
			"order":             prior.response,
		})
	}

	var resp map[string]any
	if err := t.client.Do(ctx, RoleSales, "POST", "/api/v1/store/orders", body, &resp); err != nil {
		return errorResult(err)
	}
	orderResults[sessionKey] = orderRecord{payloadHash: hashStr, response: resp}
	drafts.clear(sessionKey)
	return jsonResult(map[string]any{"order": resp})
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
