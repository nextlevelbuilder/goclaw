package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestReadImage_BothPathAndUrl_Error(t *testing.T) {
	tool := NewReadImageTool(nil)

	res := tool.Execute(context.Background(), map[string]any{
		"prompt": "describe this",
		"path":   "workspace/image.png",
		"url":    "https://example.com/image.png",
	})

	if !res.IsError {
		t.Fatalf("expected error when both path and url are provided")
	}

	if !strings.Contains(res.ForLLM, "Both 'path' and 'url' parameters cannot be specified") {
		t.Errorf("unexpected error message: %s", res.ForLLM)
	}
}

func TestReadImage_PrivateURL_Error(t *testing.T) {
	tool := NewReadImageTool(nil)

	res := tool.Execute(context.Background(), map[string]any{
		"prompt": "describe this",
		"url":    "http://127.0.0.1/image.png",
	})

	if !res.IsError {
		t.Fatalf("expected error for private image URL")
	}
	if !strings.Contains(res.ForLLM, "Invalid image URL") {
		t.Errorf("unexpected error message: %s", res.ForLLM)
	}
}

func TestReadImage_AnthropicURL_Error(t *testing.T) {
	tool := NewReadImageTool(nil)

	params := map[string]any{
		"prompt": "describe this",
		"images": []providers.ImageContent{
			{
				URL: "https://93.184.216.34/image.png",
			},
		},
	}

	_, _, err := tool.callProvider(context.Background(), nil, "anthropic", "claude-3-sonnet", params)
	if err == nil {
		t.Fatalf("expected error for anthropic provider with image URL")
	}

	if !strings.Contains(err.Error(), "does not support analyzing images directly from a URL") {
		t.Errorf("unexpected error message: %v", err)
	}

	// Should also error for claude-cli
	_, _, err = tool.callProvider(context.Background(), nil, "claude-cli", "claude-3-sonnet", params)
	if err == nil {
		t.Fatalf("expected error for claude-cli provider with image URL")
	}
}
