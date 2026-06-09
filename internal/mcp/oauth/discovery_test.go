package mcpoauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/security"
)

// mockASMeta is used to serve AS metadata in tests.
type mockASMeta struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint,omitempty"`
	ScopesSupported       []string `json:"scopes_supported,omitempty"`
}

// newDiscoveryServer returns a test server that serves OAuth discovery metadata
// at standard well-known paths. If protectedResource is non-empty, it responds
// to /.well-known/oauth-protected-resource with that AS URL. asStatus controls
// the HTTP status for AS metadata endpoints (200 or error).
func newDiscoveryServer(t *testing.T, protectedResourceAS string, asStatus int, authEndpoint, tokenEndpoint string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/.well-known/oauth-protected-resource") ||
			strings.Contains(r.URL.Path, "/.well-known/oauth-protected-resource/"):
			if protectedResourceAS == "" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resource":              "https://mcp.example.com/server",
				"authorization_servers": []string{protectedResourceAS},
			})

		case strings.HasSuffix(r.URL.Path, "/.well-known/oauth-authorization-server") ||
			strings.Contains(r.URL.Path, "/.well-known/oauth-authorization-server/"):
			if asStatus != http.StatusOK {
				http.Error(w, "not found", asStatus)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(mockASMeta{
				AuthorizationEndpoint: authEndpoint,
				TokenEndpoint:         tokenEndpoint,
			})

		case strings.HasSuffix(r.URL.Path, "/.well-known/openid-configuration") ||
			strings.Contains(r.URL.Path, "/.well-known/openid-configuration"):
			if asStatus != http.StatusOK {
				http.Error(w, "not found", asStatus)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(mockASMeta{
				AuthorizationEndpoint: authEndpoint,
				TokenEndpoint:         tokenEndpoint,
			})

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDiscoverRFC8414Success(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	srv := newDiscoveryServer(t, "", http.StatusOK, "https://auth.example.com/authorize", "https://auth.example.com/token")

	d := NewDiscoverer(security.NewSafeClient(5 * time.Second))
	result, err := d.Discover(context.Background(), srv.URL+"/server")
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	if result.AuthorizationEndpoint != "https://auth.example.com/authorize" {
		t.Errorf("AuthorizationEndpoint = %q", result.AuthorizationEndpoint)
	}
	if result.TokenEndpoint != "https://auth.example.com/token" {
		t.Errorf("TokenEndpoint = %q", result.TokenEndpoint)
	}
}

func TestDiscoverCachesResult(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	hitCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, ".well-known/oauth-authorization-server") ||
			strings.Contains(r.URL.Path, ".well-known/openid-configuration") {
			hitCount++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(mockASMeta{
				AuthorizationEndpoint: "https://auth.example.com/authorize",
				TokenEndpoint:         "https://auth.example.com/token",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	d := NewDiscoverer(security.NewSafeClient(5 * time.Second))
	url := srv.URL + "/mcp"

	// First call.
	if _, err := d.Discover(context.Background(), url); err != nil {
		t.Fatalf("first Discover() error: %v", err)
	}
	// Second call — should use cache.
	if _, err := d.Discover(context.Background(), url); err != nil {
		t.Fatalf("second Discover() error: %v", err)
	}

	if hitCount != 1 {
		t.Errorf("expected exactly 1 server hit (cached), got %d", hitCount)
	}
}

func TestDiscoverInvalidateCache(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	hitCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, ".well-known") {
			hitCount++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(mockASMeta{
				AuthorizationEndpoint: "https://auth.example.com/authorize",
				TokenEndpoint:         "https://auth.example.com/token",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	d := NewDiscoverer(security.NewSafeClient(5 * time.Second))
	url := srv.URL

	// Populate cache.
	if _, err := d.Discover(context.Background(), url); err != nil {
		t.Fatalf("Discover() error: %v", err)
	}

	// Invalidate and re-fetch.
	d.InvalidateCache(url)
	if _, err := d.Discover(context.Background(), url); err != nil {
		t.Fatalf("Discover() after invalidate error: %v", err)
	}

	if hitCount < 2 {
		t.Errorf("expected at least 2 server hits after invalidation, got %d", hitCount)
	}
}

func TestDiscoverRFC9728Priority(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	// AS server (separate from MCP server in full impl). Its advertised issuer must
	// share the AS origin (RFC 8414 §3.3), so it is set to asSrv.URL at request time.
	var asSrv *httptest.Server
	asSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, ".well-known/oauth-authorization-server") ||
			strings.Contains(r.URL.Path, ".well-known/openid-configuration") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(mockASMeta{
				Issuer:                asSrv.URL,
				AuthorizationEndpoint: asSrv.URL + "/authorize",
				TokenEndpoint:         asSrv.URL + "/token",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer asSrv.Close()

	// MCP server that returns RFC 9728 protected resource metadata pointing to asSrv.
	// Declare var first so the closure can reference mcpSrv.URL after assignment.
	var mcpSrv *httptest.Server
	mcpSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/.well-known/oauth-protected-resource") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"resource":              mcpSrv.URL,
				"authorization_servers": []string{asSrv.URL},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer mcpSrv.Close()

	d := NewDiscoverer(security.NewSafeClient(5 * time.Second))
	result, err := d.Discover(context.Background(), mcpSrv.URL)
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	// Should use AS from RFC 9728, not MCP server itself.
	if result.Issuer != asSrv.URL {
		t.Errorf("Issuer = %q, want %q (RFC 9728 AS)", result.Issuer, asSrv.URL)
	}
	// Endpoints must come from the AS server, not the MCP server (RFC 9728 priority).
	if result.AuthorizationEndpoint != asSrv.URL+"/authorize" {
		t.Errorf("AuthorizationEndpoint = %q, want AS server endpoint", result.AuthorizationEndpoint)
	}
}

// TestDiscoverIssuerMismatchRejected verifies that AS metadata advertising an
// issuer on a different origin than the one it was fetched from is rejected
// (RFC 8414 §3.3 — AS mix-up protection).
func TestDiscoverIssuerMismatchRejected(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, ".well-known/oauth-authorization-server") ||
			strings.Contains(r.URL.Path, ".well-known/openid-configuration") {
			w.Header().Set("Content-Type", "application/json")
			// Issuer points at a host the server does not control.
			_ = json.NewEncoder(w).Encode(mockASMeta{
				Issuer:                "https://evil.example.com",
				AuthorizationEndpoint: "https://evil.example.com/authorize",
				TokenEndpoint:         "https://evil.example.com/token",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	d := NewDiscoverer(security.NewSafeClient(5 * time.Second))
	_, err := d.Discover(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected discovery to reject issuer/origin mismatch, got nil error")
	}
}

func TestDiscoverFallbackOIDC(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	// Server that returns 404 for RFC 8414 path but serves OIDC.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "oauth-protected-resource"):
			http.NotFound(w, r)
		case strings.Contains(r.URL.Path, "oauth-authorization-server"):
			http.NotFound(w, r) // RFC 8414 not available
		case strings.Contains(r.URL.Path, "openid-configuration"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(mockASMeta{
				AuthorizationEndpoint: "https://oidc.example.com/authorize",
				TokenEndpoint:         "https://oidc.example.com/token",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	d := NewDiscoverer(security.NewSafeClient(5 * time.Second))
	result, err := d.Discover(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Discover() OIDC fallback error: %v", err)
	}
	if result.AuthorizationEndpoint != "https://oidc.example.com/authorize" {
		t.Errorf("AuthorizationEndpoint = %q, want OIDC fallback", result.AuthorizationEndpoint)
	}
}

func TestDiscoverAllEndpointsFail(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	// Server that returns 404 for all well-known paths.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	d := NewDiscoverer(security.NewSafeClient(5 * time.Second))
	_, err := d.Discover(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error when all discovery endpoints fail, got nil")
	}
}

func TestDiscoverMissingRequiredFields(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	// Server responds with JSON missing authorization_endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, ".well-known") {
			w.Header().Set("Content-Type", "application/json")
			// Missing authorization_endpoint and token_endpoint.
			_ = json.NewEncoder(w).Encode(map[string]string{"issuer": "https://example.com"})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	d := NewDiscoverer(security.NewSafeClient(5 * time.Second))
	_, err := d.Discover(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error when required fields missing, got nil")
	}
}

func TestDiscoverInvalidURL(t *testing.T) {
	d := NewDiscoverer(http.DefaultClient)
	_, err := d.Discover(nil, "://invalid-url") //nolint:staticcheck
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}

func TestDiscoverResponseTooLarge(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, ".well-known") {
			w.Header().Set("Content-Type", "application/json")
			// > 64KB response — will be truncated, causing JSON parse error.
			large := make([]byte, 65*1024)
			for i := range large {
				large[i] = 'x'
			}
			_, _ = w.Write([]byte(`{"authorization_endpoint":"`))
			_, _ = w.Write(large)
			_, _ = w.Write([]byte(`","token_endpoint":"https://t.example.com/token"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	d := NewDiscoverer(security.NewSafeClient(5 * time.Second))
	// Truncated response → JSON will be malformed → discovery continues but
	// this candidate fails. All candidates fail → error.
	_, err := d.Discover(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error for oversized response, got nil")
	}
}
