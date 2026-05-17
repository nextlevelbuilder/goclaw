package tuyettruong

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// All quote_* tools share the same session-keyed draft state. They are
// channel-agnostic — the LLM orchestrates the flow; tools only mutate state
// and re-fetch authoritative data when needed.

// --- quote_add_item ---

type QuoteAddItemTool struct{ client *Client }

func NewQuoteAddItemTool(c *Client) *QuoteAddItemTool { return &QuoteAddItemTool{client: c} }

func (t *QuoteAddItemTool) Name() string { return "quote_add_item" }
func (t *QuoteAddItemTool) Description() string {
	return "Add (or increase qty of) a product variant in the customer's current quote draft. Fetches the live price from the store API and snapshots it on the draft. If the SKU already exists, qty is added to existing."
}
func (t *QuoteAddItemTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"slug": map[string]any{"type": "string", "description": "Product slug"},
			"sku":  map[string]any{"type": "string", "description": "Variant SKU"},
			"qty":  map[string]any{"type": "integer", "description": "Quantity to add (default 1)"},
		},
		"required": []string{"slug", "sku"},
	}
}

func (t *QuoteAddItemTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	slug, _ := args["slug"].(string)
	sku, _ := args["sku"].(string)
	if slug == "" || sku == "" {
		return errorResult(fmt.Errorf("slug and sku required"))
	}
	qty := 1
	if v, ok := args["qty"].(float64); ok && v > 0 {
		qty = int(v)
	}

	// Fetch live product detail from public endpoint (no cost field).
	var product struct {
		Slug     string `json:"slug"`
		Name     string `json:"name"`
		Images   []string `json:"images"`
		Variants []struct {
			Sku        string            `json:"sku"`
			Attributes map[string]string `json:"attributes"`
			Price      string            `json:"price"`
			Stock      int               `json:"stock"`
			Active     bool              `json:"active"`
			Images     []string          `json:"images"`
		} `json:"variants"`
	}
	if err := t.client.Do(ctx, RoleSales, "GET", "/api/v1/store/products/"+slug, nil, &product); err != nil {
		return errorResult(err)
	}

	var matched *struct {
		Sku        string            `json:"sku"`
		Attributes map[string]string `json:"attributes"`
		Price      string            `json:"price"`
		Stock      int               `json:"stock"`
		Active     bool              `json:"active"`
		Images     []string          `json:"images"`
	}
	for i := range product.Variants {
		if product.Variants[i].Sku == sku {
			matched = &product.Variants[i]
			break
		}
	}
	if matched == nil {
		return errorResult(fmt.Errorf("variant %s not found in product %s", sku, slug))
	}
	if !matched.Active {
		return errorResult(fmt.Errorf("variant %s is inactive", sku))
	}

	price, err := parsePrice(matched.Price)
	if err != nil {
		return errorResult(err)
	}

	sessionKey := tools.ToolSessionKeyFromCtx(ctx)
	if sessionKey == "" {
		return errorResult(fmt.Errorf("no session context — cannot maintain quote draft"))
	}
	draft := drafts.loadOrInit(sessionKey)

	img := ""
	if len(matched.Images) > 0 {
		img = matched.Images[0]
	} else if len(product.Images) > 0 {
		img = product.Images[0]
	}

	idx := draft.findIndex(sku)
	if idx >= 0 {
		draft.Items[idx].Qty += qty
		draft.Items[idx].UnitPriceSnapshot = price // refresh snapshot
	} else {
		draft.Items = append(draft.Items, DraftItem{
			ProductSlug:       product.Slug,
			ProductName:       product.Name,
			VariantSku:        sku,
			VariantAttributes: matched.Attributes,
			UnitPriceSnapshot: price,
			Qty:               qty,
			ImageURL:          img,
		})
	}
	draft.UpdatedAt = time.Now()
	return jsonResult(map[string]any{
		"ok":       true,
		"item":     draft.Items[len(draft.Items)-1],
		"subtotal": draft.Subtotal(),
		"itemCount": len(draft.Items),
	})
}

// --- quote_remove_item ---

type QuoteRemoveItemTool struct{}

func NewQuoteRemoveItemTool() *QuoteRemoveItemTool { return &QuoteRemoveItemTool{} }

func (t *QuoteRemoveItemTool) Name() string { return "quote_remove_item" }
func (t *QuoteRemoveItemTool) Description() string {
	return "Remove a SKU from the current quote draft. No-op if SKU not in draft."
}
func (t *QuoteRemoveItemTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sku": map[string]any{"type": "string"},
		},
		"required": []string{"sku"},
	}
}
func (t *QuoteRemoveItemTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	sku, _ := args["sku"].(string)
	if sku == "" {
		return errorResult(fmt.Errorf("sku required"))
	}
	sessionKey := tools.ToolSessionKeyFromCtx(ctx)
	d := drafts.load(sessionKey)
	if d == nil {
		return jsonResult(map[string]any{"ok": true, "note": "no draft"})
	}
	i := d.findIndex(sku)
	if i < 0 {
		return jsonResult(map[string]any{"ok": true, "note": "sku not in draft"})
	}
	d.Items = append(d.Items[:i], d.Items[i+1:]...)
	d.UpdatedAt = time.Now()
	return jsonResult(map[string]any{"ok": true, "itemCount": len(d.Items), "subtotal": d.Subtotal()})
}

// --- quote_view ---

type QuoteViewTool struct{}

func NewQuoteViewTool() *QuoteViewTool { return &QuoteViewTool{} }

func (t *QuoteViewTool) Name() string { return "quote_view" }
func (t *QuoteViewTool) Description() string {
	return "Return the current quote draft as structured JSON (for the LLM to inspect or re-render). Use quote_finalize to produce the customer-facing text."
}
func (t *QuoteViewTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *QuoteViewTool) Execute(ctx context.Context, _ map[string]any) *tools.Result {
	d := drafts.load(tools.ToolSessionKeyFromCtx(ctx))
	if d == nil {
		return jsonResult(map[string]any{"empty": true})
	}
	return jsonResult(d)
}

// --- quote_set_customer ---

type QuoteSetCustomerTool struct{}

func NewQuoteSetCustomerTool() *QuoteSetCustomerTool { return &QuoteSetCustomerTool{} }

func (t *QuoteSetCustomerTool) Name() string { return "quote_set_customer" }
func (t *QuoteSetCustomerTool) Description() string {
	return "Attach customer details to the draft. All fields optional; call multiple times to fill in as you collect info from the chat."
}
func (t *QuoteSetCustomerTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":    map[string]any{"type": "string"},
			"phone":   map[string]any{"type": "string"},
			"email":   map[string]any{"type": "string"},
			"address": map[string]any{"type": "string"},
			"note":    map[string]any{"type": "string"},
		},
	}
}
func (t *QuoteSetCustomerTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	d := drafts.loadOrInit(tools.ToolSessionKeyFromCtx(ctx))
	if s, ok := args["name"].(string); ok && s != "" {
		d.Customer.Name = strings.TrimSpace(s)
	}
	if s, ok := args["phone"].(string); ok && s != "" {
		d.Customer.Phone = strings.TrimSpace(s)
	}
	if s, ok := args["email"].(string); ok && s != "" {
		d.Customer.Email = strings.TrimSpace(s)
	}
	if s, ok := args["address"].(string); ok && s != "" {
		d.Customer.Address = strings.TrimSpace(s)
	}
	if s, ok := args["note"].(string); ok {
		d.Customer.Note = strings.TrimSpace(s)
	}
	d.UpdatedAt = time.Now()
	return jsonResult(d.Customer)
}

// --- quote_finalize ---

type QuoteFinalizeTool struct{ client *Client }

func NewQuoteFinalizeTool(c *Client) *QuoteFinalizeTool { return &QuoteFinalizeTool{client: c} }

func (t *QuoteFinalizeTool) Name() string { return "quote_finalize" }
func (t *QuoteFinalizeTool) Description() string {
	return "Re-validate every line against current store prices, then render the customer-facing quote text. Returns both the text (send this to the customer) and a price_drift flag — if true, you MUST show the new quote and re-ask the customer to confirm before order_place."
}
func (t *QuoteFinalizeTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"quote_code":    map[string]any{"type": "string", "description": "Short code for this quote (sales bot generates, e.g. Q26051723). Used as CK content."},
			"shipping_fee":  map[string]any{"type": "number"},
			"bank_name":     map[string]any{"type": "string", "description": "Display name e.g. 'Vietcombank' or 'VCB'"},
			"bank_account":  map[string]any{"type": "string"},
			"bank_holder":   map[string]any{"type": "string"},
		},
		"required": []string{"quote_code"},
	}
}
func (t *QuoteFinalizeTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	sessionKey := tools.ToolSessionKeyFromCtx(ctx)
	d := drafts.load(sessionKey)
	if d == nil || len(d.Items) == 0 {
		return errorResult(fmt.Errorf("draft is empty — add items first via quote_add_item"))
	}

	// Re-fetch each product and compare snapshot vs current price.
	drift := false
	for i, item := range d.Items {
		var p struct {
			Variants []struct {
				Sku    string `json:"sku"`
				Price  string `json:"price"`
				Stock  int    `json:"stock"`
				Active bool   `json:"active"`
			} `json:"variants"`
		}
		if err := t.client.Do(ctx, RoleSales, "GET", "/api/v1/store/products/"+item.ProductSlug, nil, &p); err != nil {
			return errorResult(fmt.Errorf("re-fetch %s: %w", item.ProductSlug, err))
		}
		var newPrice float64 = -1
		for _, v := range p.Variants {
			if v.Sku == item.VariantSku {
				if !v.Active {
					return errorResult(fmt.Errorf("variant %s đã ngưng bán — vui lòng bỏ khỏi quote", item.VariantSku))
				}
				newPrice, _ = parsePrice(v.Price)
				break
			}
		}
		if newPrice < 0 {
			return errorResult(fmt.Errorf("variant %s không còn tồn tại", item.VariantSku))
		}
		if newPrice != item.UnitPriceSnapshot {
			drift = true
			d.Items[i].UnitPriceSnapshot = newPrice
		}
	}
	d.UpdatedAt = time.Now()

	quoteCode, _ := args["quote_code"].(string)
	if quoteCode == "" {
		return errorResult(fmt.Errorf("quote_code required"))
	}
	shipping := 0.0
	if v, ok := args["shipping_fee"].(float64); ok {
		shipping = v
	}
	bankName, _ := args["bank_name"].(string)
	bankAcc, _ := args["bank_account"].(string)
	bankHolder, _ := args["bank_holder"].(string)

	text := RenderQuote(RenderInput{
		QuoteCode:    quoteCode,
		Draft:        d,
		ShippingFee:  shipping,
		BankBankName: bankName,
		BankAccount:  bankAcc,
		BankHolder:   bankHolder,
	})
	return jsonResult(map[string]any{
		"text":         text,
		"price_drift":  drift,
		"subtotal":     d.Subtotal(),
		"shipping_fee": shipping,
		"total":        d.Subtotal() + shipping,
		"quote_code":   quoteCode,
	})
}

// --- quote_clear ---

type QuoteClearTool struct{}

func NewQuoteClearTool() *QuoteClearTool { return &QuoteClearTool{} }

func (t *QuoteClearTool) Name() string { return "quote_clear" }
func (t *QuoteClearTool) Description() string {
	return "Discard the current quote draft. Use when the customer says 'huỷ' / 'bỏ' / 'bắt đầu lại'."
}
func (t *QuoteClearTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *QuoteClearTool) Execute(ctx context.Context, _ map[string]any) *tools.Result {
	drafts.clear(tools.ToolSessionKeyFromCtx(ctx))
	return jsonResult(map[string]any{"ok": true})
}

func parsePrice(raw string) (float64, error) {
	// API returns numeric(14,0) as string. Strip whitespace, parse float.
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("empty price")
	}
	var f float64
	_, err := fmt.Sscanf(raw, "%f", &f)
	if err != nil {
		return 0, fmt.Errorf("parse price %q: %w", raw, err)
	}
	return f, nil
}
