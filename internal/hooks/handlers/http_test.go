package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/hooks/handlers"
	"github.com/nextlevelbuilder/goclaw/internal/security"
)

// makeHTTPCfg builds a minimal HookConfig with given URL.
func makeHTTPCfg(url string) hooks.HookConfig {
	return hooks.HookConfig{
		HandlerType: hooks.HandlerHTTP,
		Scope:       hooks.ScopeAgent,
		Config:      map[string]any{"url": url},
		Enabled:     true,
	}
}

func TestHTTP_200Allow(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// empty body → allow
	}))
	defer srv.Close()

	h := &handlers.HTTPHandler{Client: srv.Client()}
	dec, err := h.Execute(context.Background(), makeHTTPCfg(srv.URL), hooks.Event{HookEvent: hooks.EventPreToolUse})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec != hooks.DecisionAllow {
		t.Errorf("decision=%q, want allow", dec)
	}
}

func TestHTTP_200BlockDecision(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"decision":"block"}`))
	}))
	defer srv.Close()

	h := &handlers.HTTPHandler{Client: srv.Client()}
	dec, err := h.Execute(context.Background(), makeHTTPCfg(srv.URL), hooks.Event{HookEvent: hooks.EventPreToolUse})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec != hooks.DecisionBlock {
		t.Errorf("decision=%q, want block", dec)
	}
}

func TestHTTP_200ContinueFalse(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"continue":false}`))
	}))
	defer srv.Close()

	h := &handlers.HTTPHandler{Client: srv.Client()}
	dec, err := h.Execute(context.Background(), makeHTTPCfg(srv.URL), hooks.Event{HookEvent: hooks.EventPreToolUse})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec != hooks.DecisionBlock {
		t.Errorf("decision=%q, want block (continue:false)", dec)
	}
}

func TestHTTP_5xxRetriesOnce(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// Second call: allow
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	h := &handlers.HTTPHandler{Client: srv.Client()}
	dec, err := h.Execute(context.Background(), makeHTTPCfg(srv.URL), hooks.Event{HookEvent: hooks.EventPreToolUse})
	if err != nil {
		t.Fatalf("unexpected error after retry: %v", err)
	}
	if dec != hooks.DecisionAllow {
		t.Errorf("decision=%q, want allow", dec)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("server called %d times, want 2 (original + one retry)", got)
	}
}

func TestHTTP_4xxReturnsError(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	// The Execute method retries once on any doRequest error (including 4xx).
	// Primary assertion: the final decision is DecisionError regardless of retry.
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	h := &handlers.HTTPHandler{Client: srv.Client()}
	dec, err := h.Execute(context.Background(), makeHTTPCfg(srv.URL), hooks.Event{HookEvent: hooks.EventPreToolUse})
	if err == nil {
		t.Fatal("expected error on persistent 400")
	}
	if dec != hooks.DecisionError {
		t.Errorf("decision=%q, want error", dec)
	}
	// Execute retries once on any non-nil doRequest error, so server sees 2 calls.
	if got := calls.Load(); got != 2 {
		t.Errorf("server called %d times, want 2 (original + one retry)", got)
	}
}

func TestHTTP_MissingURL(t *testing.T) {
	h := &handlers.HTTPHandler{Client: http.DefaultClient}
	cfg := hooks.HookConfig{
		HandlerType: hooks.HandlerHTTP,
		Scope:       hooks.ScopeAgent,
		Config:      map[string]any{}, // no "url"
		Enabled:     true,
	}
	dec, err := h.Execute(context.Background(), cfg, hooks.Event{HookEvent: hooks.EventPreToolUse})
	if err == nil {
		t.Fatal("expected error for missing URL")
	}
	if dec != hooks.DecisionError {
		t.Errorf("decision=%q, want error", dec)
	}
}

func TestHTTP_NonJSON2xx_TreatedAsAllow(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer srv.Close()

	h := &handlers.HTTPHandler{Client: srv.Client()}
	dec, err := h.Execute(context.Background(), makeHTTPCfg(srv.URL), hooks.Event{HookEvent: hooks.EventPreToolUse})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec != hooks.DecisionAllow {
		t.Errorf("decision=%q, want allow (non-JSON 2xx)", dec)
	}
}

func TestHTTP_EncryptedAuthHeader_Decrypted(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	// 32-byte raw key (hex-encoded = 64 chars accepted by DeriveKey).
	const key = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"

	encrypted, err := crypto.Encrypt("Bearer secret-token", key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if gotAuth == "Bearer secret-token" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	h := &handlers.HTTPHandler{
		EncryptKey: key,
		Client:     srv.Client(),
	}
	cfg := hooks.HookConfig{
		HandlerType: hooks.HandlerHTTP,
		Scope:       hooks.ScopeAgent,
		Config: map[string]any{
			"url": srv.URL,
			"headers": map[string]any{
				"Authorization": encrypted,
			},
		},
		Enabled: true,
	}
	dec, err := h.Execute(context.Background(), cfg, hooks.Event{HookEvent: hooks.EventPreToolUse})
	if err != nil {
		t.Fatalf("unexpected error: %v (got Authorization: %q)", err, gotAuth)
	}
	if dec != hooks.DecisionAllow {
		t.Errorf("decision=%q, want allow; server got Authorization=%q", dec, gotAuth)
	}
}

func TestHTTP_ResponseBodyCappedAt1MiB(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write 2 MiB of 'x' — handler must cap at 1 MiB and not panic.
		chunk := make([]byte, 64*1024)
		for i := range chunk {
			chunk[i] = 'x'
		}
		for range 32 { // 32 × 64 KiB = 2 MiB
			w.Write(chunk)
		}
	}))
	defer srv.Close()

	h := &handlers.HTTPHandler{Client: srv.Client()}
	// 2 MiB body is non-JSON → treated as allow, no panic.
	dec, err := h.Execute(context.Background(), makeHTTPCfg(srv.URL), hooks.Event{HookEvent: hooks.EventPreToolUse})
	if err != nil {
		t.Fatalf("unexpected error on oversized body: %v", err)
	}
	// Non-JSON large body → allow.
	if dec != hooks.DecisionAllow {
		t.Errorf("decision=%q, want allow for oversized non-JSON body", dec)
	}
}
