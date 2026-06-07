package tuyettruong

import (
	"context"
	"net/url"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// FxGetTool wraps GET /api/v1/admin/fx. Read-only: returns today's confirmed
// FX snapshot per currency plus a VCB-derived suggested rate (VCB Sell +
// buffer) that the supplier can confirm. No write happens here.
type FxGetTool struct {
	client *Client
}

func NewFxGetTool(c *Client) *FxGetTool { return &FxGetTool{client: c} }

func (t *FxGetTool) Name() string { return "tt_fx_get" }
func (t *FxGetTool) Description() string {
	return "Read today's foreign-exchange rates used to price imported goods (nhập hàng). Currency is per product origin: AUD=Úc, KRW=Hàn, JPY=Nhật, EUR=Đức, USD=Mỹ. Returns { bufferPct, updatedAt, rates: { <CCY>: { currency, rate, fresh, snapshot, vcbSell, suggested, bufferPct } } }. `snapshot` is the day's supplier-confirmed rate (authoritative if present); `vcbSell` is the live Vietcombank sell rate; `suggested` = vcbSell + buffer — offer this number to the supplier (tiếp viên) to confirm before recording it with tt_fx_set_snapshot. Pass `currencies` to limit which currencies are returned (e.g. ['AUD','KRW'])."
}

func (t *FxGetTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"currencies": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Currency codes to fetch, e.g. ['AUD','KRW']. Omit for all configured currencies.",
			},
		},
	}
}

func (t *FxGetTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	q := url.Values{}
	if raw, ok := args["currencies"].([]any); ok && len(raw) > 0 {
		codes := make([]string, 0, len(raw))
		for _, c := range raw {
			if s, ok := c.(string); ok && strings.TrimSpace(s) != "" {
				codes = append(codes, strings.TrimSpace(s))
			}
		}
		if len(codes) > 0 {
			q.Set("currencies", strings.Join(codes, ","))
		}
	}
	path := "/api/v1/admin/fx"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var out map[string]any
	if err := t.client.Do(ctx, RoleAdmin, "GET", path, nil, &out); err != nil {
		return errorResult(err)
	}
	return jsonResult(out)
}
