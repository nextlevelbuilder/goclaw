package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
)

// ErrDocAccessDenied is returned when the bot's Lark app lacks permission to
// read the target document or when the document is missing. Callers should
// treat this as a soft failure and inject a "[access denied]" marker into the
// agent input rather than crashing the pipeline.
//
// The sentinel is used for both "forbidden" and "not found" conditions on
// purpose — from the bot's perspective they are indistinguishable outcomes
// ("we got no content to show the agent") and the UX is identical.
var ErrDocAccessDenied = errors.New("feishu: lark doc access denied or missing")

// larkDocAccessErrorCodes enumerates the Lark API error codes that map to
// ErrDocAccessDenied. The list is narrow on purpose: we only elevate codes
// where we are confident the fetch will never succeed without operator
// intervention (permission grant, doc re-creation). All codes verified
// against the official Lark docx v1 raw_content endpoint docs at
// open.feishu.cn/document/server-docs/docs/docs/docx-v1/document/raw_content.
var larkDocAccessErrorCodes = map[int]struct{}{
	// docx v1 endpoint-specific codes.
	1770032: {}, // HTTP 403 — insufficient permissions for docx API
	1770002: {}, // HTTP 404 — document not found or deleted
	1770003: {}, // resource deleted
	// Legacy generic "no permission" code — kept as belt-and-suspenders in
	// case the endpoint surfaces it on some tenant configurations.
	99991672: {},
}

// GetDocRawContent fetches the raw text body of a Lark docx document via
// GET /open-apis/docx/v1/documents/{document_id}/raw_content.
//
// Returns the document content on success. On permission denied or not found,
// returns ErrDocAccessDenied so the caller can fall back to a soft failure
// marker. All other Lark error codes surface as a generic error so operators
// see them in logs and can investigate.
func (c *LarkClient) GetDocRawContent(ctx context.Context, docID string) (string, error) {
	if docID == "" {
		return "", fmt.Errorf("get doc raw content: empty doc id")
	}
	path := fmt.Sprintf("/open-apis/docx/v1/documents/%s/raw_content", url.PathEscape(docID))
	resp, err := c.doJSON(ctx, "GET", path, nil)
	if err != nil {
		return "", fmt.Errorf("get doc raw content: %w", err)
	}
	if resp.Code != 0 {
		if _, soft := larkDocAccessErrorCodes[resp.Code]; soft {
			return "", fmt.Errorf("%w: code=%d msg=%s", ErrDocAccessDenied, resp.Code, resp.Msg)
		}
		return "", fmt.Errorf("get doc raw content: code=%d msg=%s", resp.Code, resp.Msg)
	}
	var data struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return "", fmt.Errorf("unmarshal doc content: %w", err)
	}
	return data.Content, nil
}
