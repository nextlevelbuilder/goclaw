package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testTokenSource struct {
	token string
}

func (s *testTokenSource) Token() (string, error) {
	return s.token, nil
}

func TestVertexProviderChat_UsesBearerToken(t *testing.T) {
	var gotAuth string
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{"content": "ok"},
				},
			},
		})
	}))
	defer server.Close()

	ts := &testTokenSource{token: "vertex-access-token"}
	p := NewVertexProvider("vertex", server.URL, "google/gemini-2.5-flash", ts)
	resp, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("unexpected content: %q", resp.Content)
	}
	if gotAuth != "Bearer vertex-access-token" {
		t.Fatalf("unexpected auth header: %q", gotAuth)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("unexpected path: %q", gotPath)
	}
}
