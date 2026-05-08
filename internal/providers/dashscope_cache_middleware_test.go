package providers

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestWrapSystemForDashScopeCache_NoBoundary(t *testing.T) {
	msg := map[string]any{
		"role":    "system",
		"content": "You are a helpful assistant.",
	}
	out := wrapSystemForDashScopeCache(msg)
	blocks, ok := out["content"].([]map[string]any)
	if !ok {
		t.Fatalf("expected []map content, got %T", out["content"])
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0]["cache_control"] == nil {
		t.Error("block[0] missing cache_control")
	}
}

func TestWrapSystemForDashScopeCache_WithBoundary(t *testing.T) {
	msg := map[string]any{
		"role":    "system",
		"content": "Stable prefix\n" + CacheBoundaryMarker + "\nDynamic suffix",
	}
	out := wrapSystemForDashScopeCache(msg)
	blocks := out["content"].([]map[string]any)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0]["cache_control"] == nil {
		t.Error("stable block missing cache_control")
	}
	if blocks[1]["cache_control"] != nil {
		t.Error("dynamic block should not have cache_control")
	}
}

func TestWrapSystemForDashScopeCache_NonSystemUntouched(t *testing.T) {
	msg := map[string]any{
		"role":    "user",
		"content": "Hello",
	}
	out := wrapSystemForDashScopeCache(msg)
	if !reflect.DeepEqual(out, msg) {
		t.Error("user message should pass through unchanged")
	}
}

func TestWrapSystemForDashScopeCache_NonStringContentUntouched(t *testing.T) {
	blocks := []map[string]any{{"type": "text", "text": "x", "cache_control": map[string]any{"type": "ephemeral"}}}
	msg := map[string]any{"role": "system", "content": blocks}
	out := wrapSystemForDashScopeCache(msg)
	got, _ := json.Marshal(out["content"])
	want, _ := json.Marshal(blocks)
	if string(got) != string(want) {
		t.Errorf("idempotent fail: got %s want %s", got, want)
	}
}

func TestBuildRequestBody_DashScopeEndpoint_WrapsSystem(t *testing.T) {
	p := NewOpenAIProvider("test", "key", "https://coding-intl.dashscope.aliyuncs.com/v1", "qwen3.6-plus")
	req := ChatRequest{
		Messages: []Message{
			{Role: "system", Content: "You are an assistant."},
			{Role: "user", Content: "Hi"},
		},
	}
	body := p.buildRequestBody("qwen3.6-plus", req, false)
	msgs := body["messages"].([]map[string]any)
	sysContent := msgs[0]["content"]
	if _, ok := sysContent.([]map[string]any); !ok {
		t.Fatalf("expected DashScope system content as []block, got %T", sysContent)
	}
}

func TestBuildRequestBody_OpenAINative_DoesNotWrap(t *testing.T) {
	p := NewOpenAIProvider("test", "key", "https://api.openai.com/v1", "gpt-4o")
	req := ChatRequest{Messages: []Message{{Role: "system", Content: "..."}, {Role: "user", Content: "Hi"}}}
	body := p.buildRequestBody("gpt-4o", req, false)
	msgs := body["messages"].([]map[string]any)
	if _, ok := msgs[0]["content"].(string); !ok {
		t.Errorf("OpenAI native should keep string content, got %T", msgs[0]["content"])
	}
}

func TestApplyDashScopeToolPrefixCache_AddMarkerOnLast(t *testing.T) {
	tools := []map[string]any{
		{"type": "function", "function": map[string]any{"name": "tool_a"}},
		{"type": "function", "function": map[string]any{"name": "tool_b"}},
		{"type": "function", "function": map[string]any{"name": "tool_c"}},
	}
	out := applyDashScopeToolPrefixCache(tools, 1)
	if len(out) != 3 {
		t.Fatalf("len changed: got %d", len(out))
	}
	if out[0]["cache_control"] != nil || out[1]["cache_control"] != nil {
		t.Error("non-last tools should not have cache_control")
	}
	if out[2]["cache_control"] == nil {
		t.Error("last tool missing cache_control")
	}
}

func TestApplyDashScopeToolPrefixCache_EmptyArray(t *testing.T) {
	out := applyDashScopeToolPrefixCache([]map[string]any{}, 0)
	if len(out) != 0 {
		t.Errorf("expected empty, got %d", len(out))
	}
}

func TestApplyDashScopeToolPrefixCache_RespectsMarkerLimit(t *testing.T) {
	tools := []map[string]any{{"type": "function", "function": map[string]any{"name": "x"}}}
	out := applyDashScopeToolPrefixCache(tools, 4)
	if out[0]["cache_control"] != nil {
		t.Error("should skip tool marker when limit reached")
	}
}

func TestCountCacheControlMarkers(t *testing.T) {
	msg := map[string]any{
		"role": "system",
		"content": []map[string]any{
			{"type": "text", "text": "x", "cache_control": map[string]any{"type": "ephemeral"}},
			{"type": "text", "text": "y"},
		},
	}
	if got := countCacheControlMarkers(msg); got != 1 {
		t.Errorf("got %d, want 1", got)
	}
}

func TestBuildRequestBody_DashScopeWithTools_AppliesToolCache(t *testing.T) {
	p := NewOpenAIProvider("test", "key", "https://coding-intl.dashscope.aliyuncs.com/v1", "qwen3.6-plus")
	req := ChatRequest{
		Messages: []Message{{Role: "system", Content: "..."}, {Role: "user", Content: "Hi"}},
		Tools: []ToolDefinition{
			{Type: "function", Function: &ToolFunctionSchema{Name: "search", Description: "search docs", Parameters: map[string]any{}}},
			{Type: "function", Function: &ToolFunctionSchema{Name: "fetch", Description: "fetch url", Parameters: map[string]any{}}},
		},
	}
	body := p.buildRequestBody("qwen3.6-plus", req, false)
	tools := body["tools"].([]map[string]any)
	if tools[0]["cache_control"] != nil {
		t.Error("first tool should not have cache_control")
	}
	if tools[len(tools)-1]["cache_control"] == nil {
		t.Error("last tool should have cache_control")
	}
}

func TestBuildRequestBody_OpenAINativeWithTools_NoToolCache(t *testing.T) {
	p := NewOpenAIProvider("test", "key", "https://api.openai.com/v1", "gpt-4o")
	req := ChatRequest{
		Messages: []Message{{Role: "system", Content: "..."}, {Role: "user", Content: "Hi"}},
		Tools: []ToolDefinition{
			{Type: "function", Function: &ToolFunctionSchema{Name: "x", Description: "y", Parameters: map[string]any{}}},
		},
	}
	body := p.buildRequestBody("gpt-4o", req, false)
	tools, ok := body["tools"].([]map[string]any)
	if ok && len(tools) > 0 && tools[0]["cache_control"] != nil {
		t.Error("OpenAI native should not have tool cache_control")
	}
}

func TestBuildRequestBody_DashScopeWithEnvDisable_DoesNotWrap(t *testing.T) {
	t.Setenv("GOCLAW_DISABLE_DASHSCOPE_CACHE", "true")
	p := NewOpenAIProvider("test", "key", "https://coding-intl.dashscope.aliyuncs.com/v1", "qwen3.6-plus")
	req := ChatRequest{Messages: []Message{{Role: "system", Content: "..."}, {Role: "user", Content: "Hi"}}}
	body := p.buildRequestBody("qwen3.6-plus", req, false)
	msgs := body["messages"].([]map[string]any)
	if _, ok := msgs[0]["content"].(string); !ok {
		t.Errorf("env disable should keep string content, got %T", msgs[0]["content"])
	}
}
