package tuyettruong

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// SuppliersListTool wraps GET /api/v1/admin/suppliers. Read-only: lists the
// configured suppliers (tiếp viên / forwarders) with their currencies, carry
// fee per kg and default markup. Use it to pick a supplierId for
// tt_nhaphang_price / tt_nhaphang_commit and to know the default carry fee.
type SuppliersListTool struct {
	client *Client
}

func NewSuppliersListTool(c *Client) *SuppliersListTool { return &SuppliersListTool{client: c} }

func (t *SuppliersListTool) Name() string { return "tt_suppliers_list" }
func (t *SuppliersListTool) Description() string {
	return "List configured suppliers (tiếp viên / forwarders) for the import-goods flow. Returns { items: [{ id, slug, displayName, currencies, carryFeePerKg, defaultMarkupPct }] }. `currencies` are the origins this supplier handles (AUD=Úc, KRW=Hàn, JPY=Nhật, EUR=Đức, USD=Mỹ); `carryFeePerKg` is the per-kg ship/carry fee in VND; `defaultMarkupPct` is the shop's default retail markup. Use the returned `id` as supplierId when calling tt_nhaphang_price and tt_nhaphang_commit."
}

func (t *SuppliersListTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *SuppliersListTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	var out map[string]any
	if err := t.client.Do(ctx, RoleAdmin, "GET", "/api/v1/admin/suppliers", nil, &out); err != nil {
		return errorResult(err)
	}
	return jsonResult(out)
}
