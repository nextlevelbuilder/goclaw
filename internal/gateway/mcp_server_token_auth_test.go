package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---- mcpServerTokenAuthMiddleware ----
//
// Gates the CRUD MCP server (/api/mcp/) with its own bearer token, independent
// from tokenAuthMiddleware's general gateway token (see server.go). Mirrors
// TestTokenAuthMiddleware_* above; kept separate since the two middlewares
// are intentionally distinct code paths (different security logging) even
// though their pass/fail logic is currently identical.

func TestMCPServerTokenAuthMiddleware_ValidToken_PassesThrough(t *testing.T) {
	token := "mcp-secret"
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := mcpServerTokenAuthMiddleware(token, next)

	req := httptest.NewRequest(http.MethodPost, "/api/mcp/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("next handler should have been called with valid token")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestMCPServerTokenAuthMiddleware_WrongToken_Returns401(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := mcpServerTokenAuthMiddleware("correct-token", next)

	req := httptest.NewRequest(http.MethodPost, "/api/mcp/", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
	// Note: http.Error() (used internally) always overwrites Content-Type to
	// text/plain regardless of what the handler set beforehand — this is a
	// stdlib quirk, not a bug in mcpServerTokenAuthMiddleware, so we only
	// assert on the body content here rather than the header.
	if body := w.Body.String(); body == "" {
		t.Error("expected a non-empty error body")
	}
}

func TestMCPServerTokenAuthMiddleware_MissingAuthHeader_Returns401(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := mcpServerTokenAuthMiddleware("some-token", next)

	req := httptest.NewRequest(http.MethodPost, "/api/mcp/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestMCPServerTokenAuthMiddleware_NonBearerScheme_Returns401(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := mcpServerTokenAuthMiddleware("token123", next)

	req := httptest.NewRequest(http.MethodPost, "/api/mcp/", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// TestMCPServerTokenAuthMiddleware_DistinctFromGatewayToken proves the two
// bearer tokens are independent: a request bearing the *general* gateway
// token must NOT satisfy the MCP server's own token gate.
func TestMCPServerTokenAuthMiddleware_DistinctFromGatewayToken(t *testing.T) {
	gatewayToken := "gateway-token"
	mcpToken := "mcp-token"
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := mcpServerTokenAuthMiddleware(mcpToken, next)

	req := httptest.NewRequest(http.MethodPost, "/api/mcp/", nil)
	req.Header.Set("Authorization", "Bearer "+gatewayToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (gateway token must not unlock the MCP server)", w.Code)
	}
}
