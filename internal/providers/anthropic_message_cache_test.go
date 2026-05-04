package providers

import (
	"encoding/json"
	"testing"
)

// TestApplyCacheControlEmptyMessages verifies the helper is a no-op on an
// empty message slice (e.g. tool-result-only first turn before any user msg).
func TestApplyCacheControlEmptyMessages(t *testing.T) {
	var messages []map[string]any
	applyCacheControlToLastMessage(messages)
	if len(messages) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(messages))
	}
}

// TestApplyCacheControlStringContent verifies that a plain string content
// is converted to a single text block carrying the cache_control marker.
// Anthropic accepts both shapes; converting lets us attach the breakpoint.
func TestApplyCacheControlStringContent(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "hello"},
	}
	applyCacheControlToLastMessage(messages)

	content, ok := messages[0]["content"].([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any, got %T", messages[0]["content"])
	}
	if len(content) != 1 {
		t.Fatalf("expected 1 block, got %d", len(content))
	}
	if content[0]["type"] != "text" || content[0]["text"] != "hello" {
		t.Errorf("block content mismatch: %+v", content[0])
	}
	if content[0]["cache_control"] == nil {
		t.Error("missing cache_control on converted text block")
	}
}

// TestApplyCacheControlBlockArrayContent verifies that an existing block
// array (multi-modal user, tool_result, assistant text+tool_use) gets
// cache_control on the last block only — earlier blocks stay untouched.
func TestApplyCacheControlBlockArrayContent(t *testing.T) {
	messages := []map[string]any{
		{
			"role": "user",
			"content": []map[string]any{
				{"type": "image", "source": map[string]any{"type": "base64"}},
				{"type": "text", "text": "describe this"},
			},
		},
	}
	applyCacheControlToLastMessage(messages)

	content := messages[0]["content"].([]map[string]any)
	if content[0]["cache_control"] != nil {
		t.Error("first block should not have cache_control")
	}
	if content[1]["cache_control"] == nil {
		t.Error("last block missing cache_control")
	}
}

// TestApplyCacheControlToolResultContent verifies tool_result messages
// (sent as user role with a tool_result block) get the breakpoint. Tool
// results are deterministic and stable across replays — safe to cache.
func TestApplyCacheControlToolResultContent(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "first turn"},
		{
			"role": "user",
			"content": []map[string]any{
				{
					"type":        "tool_result",
					"tool_use_id": "tool_123",
					"content":     "result data",
				},
			},
		},
	}
	applyCacheControlToLastMessage(messages)

	first := messages[0]["content"]
	if _, isString := first.(string); !isString {
		t.Errorf("first message should remain a plain string, got %T", first)
	}
	last := messages[1]["content"].([]map[string]any)
	if last[0]["cache_control"] == nil {
		t.Error("tool_result block missing cache_control")
	}
}

// TestApplyCacheControlRawAssistantBlocks verifies the json.RawMessage path
// used for assistant turns that preserve thinking signatures. The last
// raw block must round-trip through JSON with cache_control attached.
func TestApplyCacheControlRawAssistantBlocks(t *testing.T) {
	thinking, _ := json.Marshal(map[string]any{
		"type":      "thinking",
		"thinking":  "let me consider",
		"signature": "abc123",
	})
	text, _ := json.Marshal(map[string]any{
		"type": "text",
		"text": "here is the answer",
	})

	messages := []map[string]any{
		{
			"role":    "assistant",
			"content": []json.RawMessage{thinking, text},
		},
	}
	applyCacheControlToLastMessage(messages)

	raw := messages[0]["content"].([]json.RawMessage)

	var firstBlock map[string]any
	if err := json.Unmarshal(raw[0], &firstBlock); err != nil {
		t.Fatalf("first block unmarshal: %v", err)
	}
	if firstBlock["cache_control"] != nil {
		t.Error("first raw block should not have cache_control")
	}

	var lastBlock map[string]any
	if err := json.Unmarshal(raw[1], &lastBlock); err != nil {
		t.Fatalf("last block unmarshal: %v", err)
	}
	if lastBlock["cache_control"] == nil {
		t.Error("last raw block missing cache_control")
	}
	// Preserved fields stay intact.
	if lastBlock["text"] != "here is the answer" {
		t.Errorf("last block text mutated: %v", lastBlock["text"])
	}
}

// TestApplyCacheControlRawAssistantInvalidJSON verifies graceful handling
// when the last raw block is not valid JSON: helper silently skips rather
// than corrupting the request body.
func TestApplyCacheControlRawAssistantInvalidJSON(t *testing.T) {
	bad := json.RawMessage("not valid json")
	messages := []map[string]any{
		{
			"role":    "assistant",
			"content": []json.RawMessage{bad},
		},
	}
	applyCacheControlToLastMessage(messages)
	raw := messages[0]["content"].([]json.RawMessage)
	if string(raw[0]) != "not valid json" {
		t.Errorf("invalid block was mutated: %s", raw[0])
	}
}
