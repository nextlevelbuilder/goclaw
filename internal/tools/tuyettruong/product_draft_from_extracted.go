package tuyettruong

import (
	"context"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// ProductDraftFromExtractedTool wraps POST /api/v1/admin/products/draft-from-extracted.
// This is the vision-pipeline entry point: the agent (multimodal LLM) extracts
// structured fields from a customer-sent product photo, then calls this tool
// to land a DRAFT product (active=false) for admin review. The server-side
// validation is zod-strict on the AUST L format (\d{4,10}) and image URLs.
//
// Always call tt_product_lookup_existing first when austLNumber is present.
type ProductDraftFromExtractedTool struct {
	client *Client
}

func NewProductDraftFromExtractedTool(c *Client) *ProductDraftFromExtractedTool {
	return &ProductDraftFromExtractedTool{client: c}
}

func (t *ProductDraftFromExtractedTool) Name() string { return "tt_product_draft_from_extracted" }
func (t *ProductDraftFromExtractedTool) Description() string {
	return "Create a DRAFT product (active=false) from fields you extracted out of a customer-sent product photo. Always lands inactive for admin review — never published automatically. Provide whatever you can read off the box: name (required), brand, austLNumber (AU therapeutic listing, dedup key), packSize, ingredients, ageRange, manufacturerUrl, description, images (R2 URLs of the customer photo or manufacturer images). Server dedupes on austLNumber and returns mode='matched' with the existing slug if already in catalog. Call tt_product_lookup_existing FIRST to avoid wasted writes."
}

func (t *ProductDraftFromExtractedTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":            map[string]any{"type": "string", "description": "Product name as printed on the box (e.g. 'HAPPi Baby Lactoferrin Powder')."},
			"brand":           map[string]any{"type": "string", "description": "Brand name."},
			"categoryPath":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Vietnamese category path, e.g. ['Sức khoẻ', 'Trẻ em']."},
			"description":     map[string]any{"type": "string", "description": "Free-text description. packSize/ageRange/ingredients/manufacturerUrl will be appended below this."},
			"images":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "R2 URLs (already uploaded via the presign endpoint). Customer photo + manufacturer images."},
			"parentSku":       map[string]any{"type": "string"},
			"austLNumber":     map[string]any{"type": "string", "description": "AUST L / AUST R number (digits only, 4-10 chars). Strongest dedup key."},
			"packSize":        map[string]any{"type": "string", "description": "e.g. '28 x 1g sachets'."},
			"ingredients":     map[string]any{"type": "string", "description": "Active ingredients/dosage."},
			"ageRange":        map[string]any{"type": "string", "description": "e.g. '1 to 36 months'."},
			"manufacturerUrl": map[string]any{"type": "string", "description": "Official product page (if found via search / printed on packaging)."},
			"sourceNote":      map[string]any{"type": "string", "description": "Audit string, e.g. 'telegram:user12345 image 2026-05-18'."},
		},
		"required": []string{"name"},
	}
}

func (t *ProductDraftFromExtractedTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	if name, _ := args["name"].(string); name == "" {
		return errorResult(fmt.Errorf("name required"))
	}
	var out map[string]any
	if err := t.client.Do(ctx, RoleAdmin, "POST", "/api/v1/admin/products/draft-from-extracted", args, &out); err != nil {
		return errorResult(err)
	}
	return jsonResult(out)
}
