package tuyettruong

import (
	"context"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// NhapHangPriceTool wraps POST /api/v1/admin/nhap-hang/price. Read-only
// (computes, never writes): given the bill line items + currency, it returns
// the landed cost = foreign×rate + kg×carryFee per item and a SUGGESTED
// retail (cost + markup, rounded). The suggested retail is a proposal only —
// it MUST be confirmed by admin/supplier before tt_nhaphang_commit. It also
// flags existing catalog matches so the agent knows whether to add stock or
// create a new draft.
type NhapHangPriceTool struct {
	client *Client
}

func NewNhapHangPriceTool(c *Client) *NhapHangPriceTool { return &NhapHangPriceTool{client: c} }

func (t *NhapHangPriceTool) Name() string { return "tt_nhaphang_price" }
func (t *NhapHangPriceTool) Description() string {
	return "Compute landed cost + a SUGGESTED retail price for goods read off a supplier bill (e.g. a Chemist Warehouse AU receipt). Read-only: it calculates, it does NOT write to the catalog. Currency is per bill origin: AUD=Úc, KRW=Hàn, JPY=Nhật, EUR=Đức, USD=Mỹ. Formula: landed cost = unitPrice×fxRate + weightKg×carryFeePerKg; suggested retail = landed cost × (1 + markup), rounded. Provide `lines` (array of { name, unitPrice (the PAID price, ignore the 'Why Pay' RRP), whyPay?, qty? }), `currency`, and optionally supplierId, carryFeePerKg, fxRateOverride, markupPctOverride, roundTo. Returns { result: { currency, fxRate, fxFresh, vcbSell, suggestedRate, bufferPct, markupPct }, items: [{ name, unitPrice, qty, whyPay, weightKg, weightEstimated, costVnd, fxComponent, carryComponent, markupPct, retailSuggestedVnd, existingMatch: { slug, name, active } | null }] }. existingMatch != null → add stock later (keep price); null → create a new inactive draft. ALWAYS have admin/supplier confirm the suggested retail before committing."
}

func (t *NhapHangPriceTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"lines": map[string]any{
				"type":        "array",
				"description": "Bill line items. Merge duplicate lines (sum qty) before sending.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":      map[string]any{"type": "string", "description": "Product name as printed on the bill."},
						"unitPrice": map[string]any{"type": "number", "description": "The PAID price per unit in the bill currency (NOT the 'Why Pay' / RRP)."},
						"whyPay":    map[string]any{"type": "number", "description": "Optional 'Why Pay' / RRP shown on the bill, for reference only."},
						"qty":       map[string]any{"type": "integer", "description": "Quantity for this line (default 1)."},
					},
					"required": []string{"name", "unitPrice"},
				},
			},
			"currency":          map[string]any{"type": "string", "description": "Bill origin currency: AUD|KRW|JPY|EUR|USD."},
			"supplierId":        map[string]any{"type": "string", "description": "Supplier id from tt_suppliers_list (sets default carry fee + markup)."},
			"carryFeePerKg":     map[string]any{"type": "number", "description": "Override carry/ship fee per kg in VND."},
			"fxRateOverride":    map[string]any{"type": "number", "description": "Override the FX rate (VND per 1 unit). Else uses today's confirmed snapshot."},
			"markupPctOverride": map[string]any{"type": "number", "description": "Override retail markup percent (e.g. 30 for +30%)."},
			"roundTo":           map[string]any{"type": "number", "description": "Round suggested retail to this VND step (e.g. 1000)."},
		},
		"required": []string{"lines", "currency"},
	}
}

func (t *NhapHangPriceTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	lines, ok := args["lines"].([]any)
	if !ok || len(lines) == 0 {
		return errorResult(fmt.Errorf("lines required (array of { name, unitPrice, whyPay?, qty? })"))
	}
	if currency, _ := args["currency"].(string); strings.TrimSpace(currency) == "" {
		return errorResult(fmt.Errorf("currency required (AUD|KRW|JPY|EUR|USD)"))
	}
	var out map[string]any
	if err := t.client.Do(ctx, RoleAdmin, "POST", "/api/v1/admin/nhap-hang/price", args, &out); err != nil {
		return errorResult(err)
	}
	return jsonResult(out)
}
