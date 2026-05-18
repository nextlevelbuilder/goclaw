package tuyettruong

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// ProductLookupExistingTool wraps GET /api/v1/admin/products/lookup-existing.
// Pre-flight dedup before drafting a new product from a customer image.
// Pass any combination of identifiers — the server checks in priority order:
// austL → parentSku → name fuzzy.
type ProductLookupExistingTool struct {
	client *Client
}

func NewProductLookupExistingTool(c *Client) *ProductLookupExistingTool {
	return &ProductLookupExistingTool{client: c}
}

func (t *ProductLookupExistingTool) Name() string { return "tt_product_lookup_existing" }
func (t *ProductLookupExistingTool) Description() string {
	return "Check if a product is already in the catalog before drafting a new one. Provide any of: austL (Australian AUST L number, strongest match), parentSku (KiotViet ID), name (Vietnamese or English name; ILIKE fuzzy match), brand (narrows name match). Returns { match: { slug, matchedBy } } on hit, { match: null } otherwise. Call this BEFORE tt_product_draft_from_extracted to avoid duplicates."
}

func (t *ProductLookupExistingTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"austL":     map[string]any{"type": "string", "description": "AU therapeutic-goods listing number (e.g. '369619'). Strongest dedup signal."},
			"parentSku": map[string]any{"type": "string", "description": "KiotViet parent SKU."},
			"name":      map[string]any{"type": "string", "description": "Product name; will be normalized + ILIKE'd."},
			"brand":     map[string]any{"type": "string", "description": "Optional brand filter on name match."},
		},
	}
}

func (t *ProductLookupExistingTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	q := url.Values{}
	for _, key := range []string{"austL", "parentSku", "name", "brand"} {
		if v, ok := args[key].(string); ok && strings.TrimSpace(v) != "" {
			q.Set(key, strings.TrimSpace(v))
		}
	}
	if len(q) == 0 {
		return errorResult(fmt.Errorf("at least one of austL, parentSku, name is required"))
	}
	var out map[string]any
	path := "/api/v1/admin/products/lookup-existing?" + q.Encode()
	if err := t.client.Do(ctx, RoleAdmin, "GET", path, nil, &out); err != nil {
		return errorResult(err)
	}
	return jsonResult(out)
}
