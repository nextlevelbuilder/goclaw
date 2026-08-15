package tools

import (
	"context"
)

// InProcessExtractor delegates to WebFetchTool.fetchRawContent for HTML→markdown
// extraction with full security checks (SSRF, domain policy on redirects).
// This is the fallback when external extractors (Defuddle) are unavailable.
type InProcessExtractor struct {
	tool *WebFetchTool
}

func (e *InProcessExtractor) Name() string { return "html-to-markdown" }

// Extract fetches the URL via the tool's fetchRawContent (full security checks)
// and returns the raw extracted markdown content.
func (e *InProcessExtractor) Extract(ctx context.Context, rawURL string) (string, error) {
	pol := e.tool.resolvePolicy(ctx)
	raw, err := e.tool.fetchRawContent(ctx, rawURL, "markdown", defaultFetchMaxChars, pol)
	if err != nil {
		return "", err
	}
	return raw.content, nil
}
