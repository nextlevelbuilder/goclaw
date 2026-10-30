package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiniMaxFlashSerializedRequests(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, effort := range []string{"", "low", "medium", "high", "xhigh", "max", "off", "none", "invalid"} {
			t.Run(effort+map[bool]string{false: "/chat", true: "/stream"}[stream], func(t *testing.T) {
				var captured map[string]any
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/v1/chat/completions" {
						t.Errorf("path = %q", r.URL.Path)
					}
					if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
						t.Error(err)
					}
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
					} else {
						_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
					}
				}))
				t.Cleanup(server.Close)
				p := NewOpenAIProvider("minimax", "test-token", server.URL+"/v1", "MiniMax-M3.1-Flash")
				p.WithProviderType("minimax_native")
				p.WithMiddlewares(func(body map[string]any, _ MiddlewareConfig) map[string]any {
					body["service_tier"] = "priority"
					return body
				})
				req := ChatRequest{
					Messages: []Message{
						{Role: "assistant", Thinking: "preserved reasoning", ToolCalls: []ToolCall{
							{ID: "call_1", Name: "lookup", Arguments: map[string]any{"query": "example"}},
						}},
						{Role: "tool", ToolCallID: "call_1", ToolName: "lookup", Content: "result"},
						{Role: "user", Content: "continue"},
					},
					Options: map[string]any{OptThinkingLevel: effort},
				}
				var err error
				if stream {
					_, err = p.ChatStream(context.Background(), req, func(StreamChunk) {})
				} else {
					_, err = p.Chat(context.Background(), req)
				}
				if err != nil {
					t.Fatal(err)
				}
				if captured["model"] != "MiniMax-M3.1-Flash" || captured["stream"] != stream {
					t.Fatalf("model/stream = %#v", captured)
				}
				if _, exists := captured["service_tier"]; exists {
					t.Fatal("service_tier leaked after middleware")
				}
				if _, exists := captured["thinking"]; exists {
					t.Fatal("Flash must use adaptive reasoning, not a thinking switch")
				}
				switch effort {
				case "low", "medium", "high", "xhigh", "max":
					if captured[OptReasoningEffort] != effort {
						t.Fatalf("reasoning_effort = %v, want %q", captured[OptReasoningEffort], effort)
					}
				default:
					if _, exists := captured[OptReasoningEffort]; exists {
						t.Fatalf("unexpected reasoning_effort = %v", captured[OptReasoningEffort])
					}
				}
				messages := captured["messages"].([]any)
				if messages[0].(map[string]any)["reasoning_content"] != "preserved reasoning" {
					t.Fatal("assistant reasoning was not replayed")
				}
				if messages[1].(map[string]any)["tool_call_id"] != "call_1" {
					t.Fatal("tool follow-up lost its call ID")
				}
			})
		}
	}
}

func TestMiniMaxSavedModelsAndEndpointIsolation(t *testing.T) {
	p := NewOpenAIProvider("minimax", "key", "https://api.minimax.io/v1", "MiniMax-M3.1-Flash")
	decision := ResolveReasoningDecision(p, p.DefaultModel(), "max", "downgrade", "reasoning")
	if decision.RequestEffort() != "max" {
		t.Fatalf("max effort was lost before serialization: %#v", decision)
	}
	for _, model := range []string{"MiniMax-M3", "MiniMax-M2.7", "my-custom-model"} {
		p := NewOpenAIProvider("minimax", "key", "https://api.minimax.io/v1", model)
		body := p.buildRequestBody(p.DefaultModel(), ChatRequest{Options: map[string]any{OptThinkingLevel: "max"}}, false)
		if body["model"] != model || p.DefaultModel() != model {
			t.Fatalf("saved model %q was changed", model)
		}
		if _, exists := body[OptReasoningEffort]; exists {
			t.Fatalf("Flash effort applied to saved model %q", model)
		}
	}
	for _, base := range []string{"https://openrouter.ai/api/v1", "https://api.minimax.io.example.com/v1", "https://example.com/minimax"} {
		p := NewOpenAIProvider("minimax", "key", base, "MiniMax-M3.1-Flash")
		if p.isMiniMaxEndpoint() {
			t.Fatalf("unrelated endpoint matched: %s", base)
		}
		body := p.buildRequestBody(p.DefaultModel(), ChatRequest{Options: map[string]any{OptThinkingLevel: "max"}}, false)
		if _, exists := body[OptReasoningEffort]; exists {
			t.Fatalf("native effort leaked to %s", base)
		}
	}
	for _, base := range []string{"https://api.minimax.io/v1", "https://api.minimaxi.com/v1"} {
		p := NewOpenAIProvider("custom-name", "key", base, "MiniMax-M3.1-Flash")
		if !p.isMiniMaxEndpoint() {
			t.Fatalf("native endpoint not matched: %s", base)
		}
	}
}
