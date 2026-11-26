package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newSSEServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
}

func TestChatStream_MidStreamErrorIsReturned(t *testing.T) {
	const partial = `data: {"choices":[{"delta":{"content":"Hel"}}]}` + "\n\n"
	cases := map[string]string{
		"error only":   `{"error":{"message":"upstream overloaded","code":502}}`,
		"with choices": `{"error":{"message":"upstream overloaded","code":"server_error"},"choices":[{"delta":{"content":""},"finish_reason":"error"}]}`,
		"string error": `{"error":"upstream overloaded"}`,
	}
	for name, event := range cases {
		t.Run(name, func(t *testing.T) {
			server := newSSEServer(partial + "data: " + event + "\n\ndata: [DONE]\n\n")
			defer server.Close()

			_, err := newTestOpenAIProvider(server.URL).ChatStream(context.Background(),
				ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}, nil)
			if err == nil || !strings.Contains(err.Error(), "upstream overloaded") {
				t.Fatalf("err = %v, want stream error", err)
			}
		})
	}
}

func TestChatStream_NullErrorFieldIgnored(t *testing.T) {
	server := newSSEServer(`data: {"error":null,"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n")
	defer server.Close()

	resp, err := newTestOpenAIProvider(server.URL).ChatStream(context.Background(),
		ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}, nil)
	if err != nil || resp.Content != "ok" {
		t.Fatalf("resp = %+v, err = %v", resp, err)
	}
}

func TestCodexChatStream_ErrorEventIsReturned(t *testing.T) {
	cases := map[string]string{
		"flat":   `{"type":"error","code":"server_error","message":"upstream overloaded"}`,
		"nested": `{"type":"error","error":{"type":"server_error","message":"upstream overloaded"}}`,
	}
	for name, event := range cases {
		t.Run(name, func(t *testing.T) {
			server := newSSEServer(`data: {"type":"response.output_text.delta","delta":"Hel"}` + "\n\ndata: " + event + "\n\n")
			defer server.Close()

			p := NewCodexProvider("test", &staticTokenSource{token: "test"}, server.URL, "gpt-4o")
			_, err := p.ChatStream(context.Background(),
				ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}, nil)
			if err == nil || !strings.Contains(err.Error(), "upstream overloaded") {
				t.Fatalf("err = %v, want stream error", err)
			}
		})
	}
}
