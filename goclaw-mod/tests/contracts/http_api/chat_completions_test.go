//go:build integration

package http_api

import "testing"

// CONTRACT: /v1/chat/completions response MUST match OpenAI format.
func TestContract_HTTP_ChatCompletions(t *testing.T) {
	baseURL, token := getTestServer(t)
	client := newHTTPClient(baseURL, token)

	resp := client.post(t, "/v1/chat/completions", map[string]any{
		"model": "test",
		"messages": []map[string]any{
			{"role": "user", "content": "Say hello in one word"},
		},
	})

	// Required OpenAI-compatible fields
	assertField(t, resp, "id", "string")
	assertField(t, resp, "object", "string")
	assertField(t, resp, "created", "number")
	assertField(t, resp, "model", "string")
	assertField(t, resp, "choices", "array")

	// Verify choices structure
	choices, ok := resp["choices"].([]any)
	if !ok || len(choices) == 0 {
		t.Error("CONTRACT VIOLATION: choices must be non-empty array")
		return
	}

	choice, ok := choices[0].(map[string]any)
	if !ok {
		t.Error("CONTRACT VIOLATION: choices[0] is not an object")
		return
	}

	assertField(t, choice, "index", "number")
	assertField(t, choice, "message", "object")
	assertField(t, choice, "finish_reason", "string")

	message, ok := choice["message"].(map[string]any)
	if !ok {
		t.Error("CONTRACT VIOLATION: choice.message is not an object")
		return
	}

	assertField(t, message, "role", "string")
	assertField(t, message, "content", "string")
}

// CONTRACT: /v1/chat/completions MUST include usage when available.
func TestContract_HTTP_ChatCompletions_Usage(t *testing.T) {
	baseURL, token := getTestServer(t)
	client := newHTTPClient(baseURL, token)

	resp := client.post(t, "/v1/chat/completions", map[string]any{
		"model": "test",
		"messages": []map[string]any{
			{"role": "user", "content": "Hi"},
		},
	})

	// Usage is optional but if present must have correct structure
	if usage, ok := resp["usage"].(map[string]any); ok {
		assertField(t, usage, "prompt_tokens", "number")
		assertField(t, usage, "completion_tokens", "number")
		assertField(t, usage, "total_tokens", "number")
	}
}
