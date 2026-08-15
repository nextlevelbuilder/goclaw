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

// TestBuildRequestBody_ToolNameSurvivesMissingAssistant is the core regression
// test for the Gemini 400. Message.ToolName must be used directly, so a tool
// result still serializes a valid FunctionResponse.name even when the assistant
// tool_call it originated from is no longer in the request window (pruned,
// truncated, or collapsed). The reverse id→name index cannot help here.
func TestBuildRequestBody_ToolNameSurvivesMissingAssistant(t *testing.T) {
	p := NewOpenAIProvider("test-gemini", "key",
		"https://generativelanguage.googleapis.com/v1beta/openai", "gemini-3-flash-preview")

	req := ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "hi"},
			// No assistant tool_call in the window — only the carried name can save this.
			{Role: "tool", ToolCallID: "call_gone", ToolName: "mcp__srv__lookup", Content: "ok"},
		},
	}

	body := p.buildRequestBody("gemini-3-flash-preview", req, false)
	msgs := body["messages"].([]map[string]any)
	if len(msgs) != 2 {
		t.Fatalf("tool msg must be kept when ToolName is present, got %d msgs", len(msgs))
	}
	if got := msgs[1]["name"]; got != "mcp__srv__lookup" {
		t.Fatalf("msg[1] name = %v, want mcp__srv__lookup", got)
	}
}

// TestBuildRequestBody_ToolNameBeatsStaleIndex verifies precedence: the name
// carried on the message wins over the reverse index, so a rewritten/aliased
// tool_call ID cannot resurface a mismatched name.
func TestBuildRequestBody_ToolNameBeatsStaleIndex(t *testing.T) {
	p := NewOpenAIProvider("test-gemini", "key",
		"https://generativelanguage.googleapis.com/v1beta/openai", "gemini-3-flash-preview")

	req := ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", ToolCalls: []ToolCall{
				{
					ID: "call_1", Name: "indexed_name",
					Metadata: map[string]string{"thought_signature": "sig"},
				},
			}},
			{Role: "tool", ToolCallID: "call_1", ToolName: "carried_name", Content: "ok"},
		},
	}

	body := p.buildRequestBody("gemini-3-flash-preview", req, false)
	msgs := body["messages"].([]map[string]any)
	if got := msgs[2]["name"]; got != "carried_name" {
		t.Fatalf("carried ToolName must win; got %v", got)
	}
}

// TestBuildRequestBody_UnresolvableToolNameDropped verifies that a tool result
// with neither a carried name nor an index match is DROPPED for Gemini. Emitting
// it with an empty (or absent) name is what produced HTTP 400 "Name cannot be
// empty" — Gemini has no tool_call_id fallback to pair on.
func TestBuildRequestBody_UnresolvableToolNameDropped(t *testing.T) {
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

	for i, m := range msgs {
		if m["role"] == "tool" {
			t.Fatalf("msg[%d]: unresolvable tool result must be dropped, got %v", i, m)
		}
		if name, present := m["name"]; present && name == "" {
			t.Fatalf("msg[%d]: empty name must never reach Gemini", i)
		}
	}
}

// TestBuildRequestBody_UnresolvableToolNameKeptForNonGemini is the regression
// guard for every other OpenAI-compat host sharing this code path (OpenAI, Qwen,
// DeepSeek, Together, ...). They pair by tool_call_id and never need `name`, so
// the drop must not apply — silently losing tool results there would be a bug.
func TestBuildRequestBody_UnresolvableToolNameKeptForNonGemini(t *testing.T) {
	p := NewOpenAIProvider("together", "key",
		"https://api.together.xyz/v1", "meta-llama/Llama-3-70b")

	req := ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "hi"},
			{Role: "tool", ToolCallID: "orphan_id", Content: "stale"},
		},
	}

	body := p.buildRequestBody("meta-llama/Llama-3-70b", req, false)
	msgs := body["messages"].([]map[string]any)
	if len(msgs) != 2 {
		t.Fatalf("non-Gemini must keep tool msg, got %d msgs", len(msgs))
	}
	if msgs[1]["role"] != "tool" {
		t.Fatalf("msg[1] role = %v, want tool", msgs[1]["role"])
	}
	if _, present := msgs[1]["name"]; present {
		t.Fatalf("non-Gemini must not emit name, got %v", msgs[1]["name"])
	}
}
