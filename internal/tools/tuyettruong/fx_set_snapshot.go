package tuyettruong

import (
	"context"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// FxSetSnapshotTool wraps POST /api/v1/admin/fx with action=set_snapshot.
// Records the day's confirmed FX rate for a currency. A supplier-confirmed
// rate is authoritative and is what tt_nhaphang_price will use to compute
// landed cost. Always offer tt_fx_get's `suggested` value to the supplier
// (tiếp viên) and only record what they confirm.
type FxSetSnapshotTool struct {
	client *Client
}

func NewFxSetSnapshotTool(c *Client) *FxSetSnapshotTool { return &FxSetSnapshotTool{client: c} }

func (t *FxSetSnapshotTool) Name() string { return "tt_fx_set_snapshot" }
func (t *FxSetSnapshotTool) Description() string {
	return "Record today's confirmed exchange rate for one currency so import pricing uses it. Currency is per product origin: AUD=Úc, KRW=Hàn, JPY=Nhật, EUR=Đức, USD=Mỹ. A supplier-confirmed snapshot is authoritative — call this AFTER the supplier (tiếp viên) confirms the rate (offer them tt_fx_get's `suggested` = VCB Sell + buffer). Params: currency (required, e.g. 'AUD'), rate (required, VND per 1 unit of currency, e.g. 19700), source (default 'supplier'; one of vcb|supplier|manual), by (optional audit string). Returns { ok, config }."
}

func (t *FxSetSnapshotTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"currency": map[string]any{"type": "string", "description": "Currency code, e.g. 'AUD', 'KRW'."},
			"rate":     map[string]any{"type": "number", "description": "VND per 1 unit of the currency, e.g. 19700 for 1 AUD."},
			"source":   map[string]any{"type": "string", "description": "Where the rate came from: vcb|supplier|manual. Default 'supplier' (supplier-confirmed, authoritative).", "enum": []string{"vcb", "supplier", "manual"}},
			"by":       map[string]any{"type": "string", "description": "Audit string, e.g. 'telegram:tiepvien Hoa 2026-05-30'."},
		},
		"required": []string{"currency", "rate"},
	}
}

func (t *FxSetSnapshotTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	currency, _ := args["currency"].(string)
	if strings.TrimSpace(currency) == "" {
		return errorResult(fmt.Errorf("currency required"))
	}
	rate, ok := args["rate"].(float64)
	if !ok || rate <= 0 {
		return errorResult(fmt.Errorf("rate required (VND per 1 unit, > 0)"))
	}
	source, _ := args["source"].(string)
	if source == "" {
		source = "supplier"
	}
	body := map[string]any{
		"action":   "set_snapshot",
		"currency": strings.TrimSpace(currency),
		"rate":     rate,
		"source":   source,
	}
	if by, _ := args["by"].(string); strings.TrimSpace(by) != "" {
		body["by"] = strings.TrimSpace(by)
	}
	var out map[string]any
	if err := t.client.Do(ctx, RoleAdmin, "POST", "/api/v1/admin/fx", body, &out); err != nil {
		return errorResult(err)
	}
	return jsonResult(out)
}
