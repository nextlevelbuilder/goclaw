package tuyettruong

import (
	"context"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// ProductDeleteTool wraps DELETE /api/v1/admin/products/{slug}. Hard delete
// (cascade to variants). Requires explicit confirm_token to avoid LLM-driven
// fat-finger destruction — the LLM must surface the confirmation phrase to
// the user, get their typed reply back, then call the tool with that exact
// token.
type ProductDeleteTool struct {
	client *Client
}

func NewProductDeleteTool(c *Client) *ProductDeleteTool { return &ProductDeleteTool{client: c} }

func (t *ProductDeleteTool) Name() string { return "tt_product_delete" }
func (t *ProductDeleteTool) Description() string {
	return "DESTRUCTIVE. Permanently delete a product and all its variants. You MUST first ask the user to confirm by typing exactly 'XOA-<slug>' (e.g. XOA-ao-tuyet-trang), then call this tool with confirm_token set to what the user typed. The tool will reject if confirm_token doesn't match 'XOA-<slug>'."
}
func (t *ProductDeleteTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"slug":          map[string]any{"type": "string"},
			"confirm_token": map[string]any{"type": "string", "description": "Must equal 'XOA-<slug>'"},
		},
		"required": []string{"slug", "confirm_token"},
	}
}

func (t *ProductDeleteTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	slug, _ := args["slug"].(string)
	token, _ := args["confirm_token"].(string)
	if slug == "" {
		return errorResult(fmt.Errorf("slug required"))
	}
	expected := "XOA-" + slug
	if !strings.EqualFold(strings.TrimSpace(token), expected) {
		return errorResult(fmt.Errorf("confirm_token must equal %q (user typed %q)", expected, token))
	}
	var out map[string]any
	if err := t.client.Do(ctx, RoleAdmin, "DELETE", "/api/v1/admin/products/"+slug, nil, &out); err != nil {
		return errorResult(err)
	}
	return jsonResult(out)
}
