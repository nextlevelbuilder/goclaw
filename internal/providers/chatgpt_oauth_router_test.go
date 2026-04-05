package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type routeEligibilityTokenSource struct {
	token       string
	eligibility RouteEligibility
}

func (s *routeEligibilityTokenSource) Token() (string, error) {
	return s.token, nil
}

func (s *routeEligibilityTokenSource) RouteEligibility(context.Context) RouteEligibility {
	return s.eligibility
}

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

func TestChatGPTOAuthRouterSkipsBlockedProviderBeforeFirstPick(t *testing.T) {
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
	providerA := NewCodexProvider("acct-a", &routeEligibilityTokenSource{
		token:       "token-a",
		eligibility: RouteEligibility{Class: RouteEligibilityBlocked, Reason: "reauth"},
	}, serverA.URL, "gpt-5.4")
	providerB := NewCodexProvider("acct-b", &routeEligibilityTokenSource{
		token:       "token-b",
		eligibility: RouteEligibility{Class: RouteEligibilityHealthy},
	}, serverB.URL, "gpt-5.4")
	providerA.retryConfig.Attempts = 1
	providerB.retryConfig.Attempts = 1
	registry.RegisterForTenant(tenantID, providerA)
	registry.RegisterForTenant(tenantID, providerB)

	router := NewChatGPTOAuthRouter(tenantID, registry, "acct-a", "manual", []string{"acct-b"})
	if _, err := router.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "route around blocked"}},
	}); err != nil {
		t.Fatalf("chat failed: %v", err)
	}

	if hitsA != 0 {
		t.Fatalf("hitsA = %d, want 0", hitsA)
	}
	if hitsB != 1 {
		t.Fatalf("hitsB = %d, want 1", hitsB)
	}
}

func TestChatGPTOAuthRouterRoundRobinPrefersHealthyBeforeUnknown(t *testing.T) {
	var hitsA, hitsB, hitsC int
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

	serverC := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsC++
		writeSSEDone(w)
	}))
	defer serverC.Close()

	tenantID := uuid.New()
	registry := NewRegistry(nil)
	providerA := NewCodexProvider("acct-a", &routeEligibilityTokenSource{
		token:       "token-a",
		eligibility: RouteEligibility{Class: RouteEligibilityHealthy},
	}, serverA.URL, "gpt-5.4")
	providerB := NewCodexProvider("acct-b", &routeEligibilityTokenSource{
		token:       "token-b",
		eligibility: RouteEligibility{Class: RouteEligibilityUnknown, Reason: "retry_later"},
	}, serverB.URL, "gpt-5.4")
	providerC := NewCodexProvider("acct-c", &routeEligibilityTokenSource{
		token:       "token-c",
		eligibility: RouteEligibility{Class: RouteEligibilityHealthy},
	}, serverC.URL, "gpt-5.4")
	providerA.retryConfig.Attempts = 1
	providerB.retryConfig.Attempts = 1
	providerC.retryConfig.Attempts = 1
	registry.RegisterForTenant(tenantID, providerA)
	registry.RegisterForTenant(tenantID, providerB)
	registry.RegisterForTenant(tenantID, providerC)

	router := NewChatGPTOAuthRouter(tenantID, registry, "acct-a", "round_robin", []string{"acct-b", "acct-c"})
	for i := range 2 {
		if _, err := router.Chat(context.Background(), ChatRequest{
			Messages: []Message{{Role: "user", Content: "prefer healthy"}},
		}); err != nil {
			t.Fatalf("chat %d failed: %v", i, err)
		}
	}

	if hitsA != 1 {
		t.Fatalf("hitsA = %d, want 1", hitsA)
	}
	if hitsB != 0 {
		t.Fatalf("hitsB = %d, want 0", hitsB)
	}
	if hitsC != 1 {
		t.Fatalf("hitsC = %d, want 1", hitsC)
	}
}

func TestChatGPTOAuthRouterReportsWhenAllProvidersBlocked(t *testing.T) {
	tenantID := uuid.New()
	registry := NewRegistry(nil)
	registry.RegisterForTenant(tenantID, NewCodexProvider("acct-a", &routeEligibilityTokenSource{
		token:       "token-a",
		eligibility: RouteEligibility{Class: RouteEligibilityBlocked, Reason: "reauth"},
	}, "http://127.0.0.1", "gpt-5.4"))
	registry.RegisterForTenant(tenantID, NewCodexProvider("acct-b", &routeEligibilityTokenSource{
		token:       "token-b",
		eligibility: RouteEligibility{Class: RouteEligibilityBlocked, Reason: "exhausted"},
	}, "http://127.0.0.1", "gpt-5.4"))

	router := NewChatGPTOAuthRouter(tenantID, registry, "acct-a", "round_robin", []string{"acct-b"})
	_, err := router.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "all blocked"}},
	})
	if err == nil {
		t.Fatal("router.Chat() error = nil, want blocked error")
	}
	if !strings.Contains(err.Error(), "reauth") || !strings.Contains(err.Error(), "exhausted") {
		t.Fatalf("router.Chat() error = %q, want both block reasons", err.Error())
	}
}

func TestChatGPTOAuthRouterPriorityOrderKeepsPrimaryAheadOfHealthyFallbacks(t *testing.T) {
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
	providerA := NewCodexProvider("acct-a", &routeEligibilityTokenSource{
		token:       "token-a",
		eligibility: RouteEligibility{Class: RouteEligibilityUnknown, Reason: "retry_later"},
	}, serverA.URL, "gpt-5.4")
	providerB := NewCodexProvider("acct-b", &routeEligibilityTokenSource{
		token:       "token-b",
		eligibility: RouteEligibility{Class: RouteEligibilityHealthy},
	}, serverB.URL, "gpt-5.4")
	providerA.retryConfig.Attempts = 1
	providerB.retryConfig.Attempts = 1
	registry.RegisterForTenant(tenantID, providerA)
	registry.RegisterForTenant(tenantID, providerB)

	router := NewChatGPTOAuthRouter(tenantID, registry, "acct-a", "priority_order", []string{"acct-b"})
	if _, err := router.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "priority first"}},
	}); err != nil {
		t.Fatalf("chat failed: %v", err)
	}

	if hitsA != 1 {
		t.Fatalf("hitsA = %d, want 1", hitsA)
	}
	if hitsB != 0 {
		t.Fatalf("hitsB = %d, want 0", hitsB)
	}
}
