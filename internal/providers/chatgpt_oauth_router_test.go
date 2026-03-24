package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestChatGPTOAuthRouterRoundRobin(t *testing.T) {
	var hitsA, hitsB int
	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsA++
		writeSSEDone(w)
	}))
	defer serverA.Close()

	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsB++
		writeSSEDone(w)
	}))
	defer serverB.Close()

	tenantID := uuid.New()
	registry := NewRegistry(nil)
	providerA := NewCodexProvider("acct-a", &staticTokenSource{token: "token-a"}, serverA.URL, "gpt-5.4")
	providerB := NewCodexProvider("acct-b", &staticTokenSource{token: "token-b"}, serverB.URL, "gpt-5.4")
	providerA.retryConfig.Attempts = 1
	providerB.retryConfig.Attempts = 1
	registry.RegisterForTenant(tenantID, providerA)
	registry.RegisterForTenant(tenantID, providerB)

	router := NewChatGPTOAuthRouter(tenantID, registry, "acct-a", "round_robin", []string{"acct-b"})

	if _, err := router.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "first"}},
	}); err != nil {
		t.Fatalf("first chat failed: %v", err)
	}
	if _, err := router.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "second"}},
	}); err != nil {
		t.Fatalf("second chat failed: %v", err)
	}

	if hitsA != 1 {
		t.Fatalf("hitsA = %d, want 1", hitsA)
	}
	if hitsB != 1 {
		t.Fatalf("hitsB = %d, want 1", hitsB)
	}
}

func TestChatGPTOAuthRouterFailoverOnRetryableError(t *testing.T) {
	var hitsA, hitsB int
	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsA++
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer serverA.Close()

	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsB++
		writeSSEDone(w)
	}))
	defer serverB.Close()

	tenantID := uuid.New()
	registry := NewRegistry(nil)
	providerA := NewCodexProvider("acct-a", &staticTokenSource{token: "token-a"}, serverA.URL, "gpt-5.4")
	providerB := NewCodexProvider("acct-b", &staticTokenSource{token: "token-b"}, serverB.URL, "gpt-5.4")
	providerA.retryConfig.Attempts = 1
	providerB.retryConfig.Attempts = 1
	registry.RegisterForTenant(tenantID, providerA)
	registry.RegisterForTenant(tenantID, providerB)

	router := NewChatGPTOAuthRouter(tenantID, registry, "acct-a", "round_robin", []string{"acct-b"})

	if _, err := router.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "fail over"}},
	}); err != nil {
		t.Fatalf("chat failed: %v", err)
	}

	if hitsA != 1 {
		t.Fatalf("hitsA = %d, want 1", hitsA)
	}
	if hitsB != 1 {
		t.Fatalf("hitsB = %d, want 1", hitsB)
	}
}
