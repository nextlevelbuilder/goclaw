package providers

import (
	"testing"
)

// TestBuildRequestBody_ToolMessageIncludesName verifies that role="tool" wire
// messages carry the originating tool's `name` field. Google Gemini's
// OpenAI-compat shim maps this to native `FunctionResponse.name`; an empty name
// trips HTTP 400 ("Name cannot be empty"). Trace: 019d8f33-2de1-7ab2-9a32-9df92cd610dd.
func TestBuildRequestBody_ToolMessageIncludesName(t *testing.T) {
	p := NewOpenAIProvider("test-gemini", "key",
		"https://generativelanguage.googleapis.com/v1beta/openai", "gemini-3-flash-preview")

	req := ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "", ToolCalls: []ToolCall{
				{
					ID: "call_1", Name: "write_file",
					Arguments: map[string]any{"path": "USER.md", "content": "x"},
					// thought_signature present → not collapsed by Gemini sig-missing collapser.
					Metadata: map[string]string{"thought_signature": "sig-abc"},
				},
			}},
			{Role: "tool", ToolCallID: "call_1", Content: "ok"},
		},
	}

	body := p.buildRequestBody("gemini-3-flash-preview", req, false)
	msgs, ok := body["messages"].([]map[string]any)
	if !ok {
		t.Fatalf("messages not []map[string]any: %T", body["messages"])
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	toolMsg := msgs[2]
	if toolMsg["role"] != "tool" {
		t.Fatalf("msg[2] role = %v, want tool", toolMsg["role"])
	}
	if toolMsg["tool_call_id"] != "call_1" {
		t.Fatalf("msg[2] tool_call_id = %v, want call_1", toolMsg["tool_call_id"])
	}
	if got := toolMsg["name"]; got != "write_file" {
		t.Fatalf("msg[2] name = %v, want write_file (Gemini 400 fix)", got)
	}
}

// TestBuildRequestBody_ToolMessageNameLookupUsesRawID ensures the lookup map is
// keyed by the *raw* ToolCallID (pre-wire-truncation) so long IDs still resolve.
func TestBuildRequestBody_ToolMessageNameLookupUsesRawID(t *testing.T) {
	longID := "call_0123456789abcdef0123456789abcdef0123456789abcdef" // > maxToolCallIDLen
	p := NewOpenAIProvider("test-gemini", "key",
		"https://generativelanguage.googleapis.com/v1beta/openai", "gemini-3-flash-preview")

	req := ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", ToolCalls: []ToolCall{
				{
					ID: longID, Name: "read_file",
					Arguments: map[string]any{"path": "x"},
					Metadata:  map[string]string{"thought_signature": "sig-xyz"},
				},
			}},
			{Role: "tool", ToolCallID: longID, Content: "data"},
		},
	}

	body := p.buildRequestBody("gemini-3-flash-preview", req, false)
	msgs := body["messages"].([]map[string]any)
	toolMsg := msgs[2]

	if got := toolMsg["name"]; got != "read_file" {
		t.Fatalf("name lookup must use raw ID; got %v want read_file", got)
	}
	wireID := p.wireToolCallID(longID)
	if got := toolMsg["tool_call_id"]; got != wireID {
		t.Fatalf("tool_call_id should be wire-translated; got %v want %v", got, wireID)
	}
}

// TestBuildRequestBody_ToolMessageWithoutMatchingCallOmitsName verifies that a
// stray tool message (no preceding tool_call with matching ID) does NOT emit an
// empty name field — better to drop the field than send "" which Gemini rejects
// just the same. Logged via slog.Warn for observability.
func TestBuildRequestBody_ToolMessageWithoutMatchingCallOmitsName(t *testing.T) {
	p := NewOpenAIProvider("test-gemini", "key",
		"https://generativelanguage.googleapis.com/v1beta/openai", "gemini-3-flash-preview")

	req := ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "hi"},
			{Role: "tool", ToolCallID: "orphan_id", Content: "stale"},
		},
	}

	body := p.buildRequestBody("gemini-3-flash-preview", req, false)
	msgs := body["messages"].([]map[string]any)
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 msgs after collapse, got %d", len(msgs))
	}
	last := msgs[len(msgs)-1]
	if last["role"] == "tool" {
		if _, present := last["name"]; present {
			t.Fatalf("orphan tool msg should omit name field, got %v", last["name"])
		}
	}
}
