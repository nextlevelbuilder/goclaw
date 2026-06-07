package tuyettruong

import (
	"context"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// SupplierUpsertTool wraps POST /api/v1/admin/suppliers. Suppliers (tiếp viên /
// hand-carriers) are onboarded conversationally — when the admin tells the
// agent about one in group chat ("chị Mai làm Úc + Hàn, công xách 200k/kg"),
// the agent records their sourcing currencies + carry fee here. Carry fee is
// stable per supplier; the daily FX rate changes separately (tt_fx_set_snapshot).
type SupplierUpsertTool struct {
	client *Client
}

func NewSupplierUpsertTool(c *Client) *SupplierUpsertTool { return &SupplierUpsertTool{client: c} }

func (t *SupplierUpsertTool) Name() string { return "tt_supplier_upsert" }
func (t *SupplierUpsertTool) Description() string {
	return "Create or update a tiếp viên (hand-carrier / supplier) by slug. Use when the admin describes a supplier in chat, e.g. 'chị Mai làm hàng Úc với Hàn, công xách 200k/kg'. Records the currencies they can source (AUD=Úc, KRW=Hàn, JPY=Nhật, EUR=Đức, USD=Mỹ), their carry fee in VND per kg, and optionally links their chat contact. Carry fee is stable per supplier; the FX rate changes daily (use tt_fx_set_snapshot for that). Params: displayName (required), slug?, currencies?, carryFeePerKg? (VND/kg), defaultMarkupPct?, internalNote?, contact? {channel, channelUserId, label?, preferred?}. Returns { ok, supplierId, slug }."
}

func (t *SupplierUpsertTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"displayName":      map[string]any{"type": "string", "description": "Supplier/tiếp viên name, e.g. 'Chị Mai'."},
			"slug":             map[string]any{"type": "string", "description": "Stable id slug; derived from displayName if omitted."},
			"currencies":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Currency codes this supplier can source: AUD,KRW,JPY,EUR,USD."},
			"carryFeePerKg":    map[string]any{"type": "number", "description": "Carry fee in VND per kg (đồng/kg). Stable per supplier."},
			"defaultMarkupPct": map[string]any{"type": "number", "description": "Optional default retail markup % for this supplier."},
			"internalNote":     map[string]any{"type": "string", "description": "Team-only note."},
			"contact": map[string]any{
				"type":        "object",
				"description": "Optional chat contact to link to this supplier.",
				"properties": map[string]any{
					"channel":       map[string]any{"type": "string", "description": "'telegram' | 'zalo' ..."},
					"channelUserId": map[string]any{"type": "string", "description": "Their user id on that channel."},
					"label":         map[string]any{"type": "string"},
					"preferred":     map[string]any{"type": "boolean"},
				},
			},
		},
		"required": []string{"displayName"},
	}
}

func (t *SupplierUpsertTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	if name, _ := args["displayName"].(string); name == "" {
		return errorResult(fmt.Errorf("displayName required"))
	}
	var out map[string]any
	if err := t.client.Do(ctx, RoleAdmin, "POST", "/api/v1/admin/suppliers", args, &out); err != nil {
		return errorResult(err)
	}
	return jsonResult(out)
}
