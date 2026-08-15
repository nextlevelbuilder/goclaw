package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// minimalServer builds a Server with only the fields needed for HTTP-level tests.
// No agent.Router, no store — these are set to nil so BuildMux can be called only for
// the specific routes we are testing.
func minimalServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{}
	// Build a no-op EventPublisher stub so NewServer doesn't panic.
	s := &Server{
		cfg:     cfg,
		clients: make(map[string]*Client),
	}
	s.rateLimiter = NewRateLimiter(0, 5)
	s.upgrader.CheckOrigin = s.checkOrigin
	return s
}

// ---- handleHealth ----

func TestHandleHealth_Returns200WithProtocolVersion(t *testing.T) {
	s := minimalServer(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	s.handleHealth(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	body := w.Body.String()
	if body == "" {
		t.Error("expected non-empty body")
	}
	// Must contain protocol version field.
	if !containsSubstr(body, "protocol") {
		t.Errorf("body %q missing 'protocol' key", body)
	}
	if !containsSubstr(body, "ok") {
		t.Errorf("body %q missing 'ok' status", body)
	}
}

func TestHandleHealth_ContentTypeJSON(t *testing.T) {
	s := minimalServer(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	s.handleHealth(w, req)
	ct := w.Result().Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

// ---- tokenAuthMiddleware ----

func TestTokenAuthMiddleware_ValidToken_PassesThrough(t *testing.T) {
	token := "super-secret"
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := tokenAuthMiddleware(token, next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
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

func TestTokenAuthMiddleware_WrongToken_Returns401(t *testing.T) {
	token := "correct-token"
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := tokenAuthMiddleware(token, next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestTokenAuthMiddleware_MissingAuthHeader_Returns401(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := tokenAuthMiddleware("some-token", next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestTokenAuthMiddleware_NonBearerScheme_Returns401(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := tokenAuthMiddleware("token123", next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz") // Base64, not Bearer
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// ---- clientIP ----

func TestClientIP_XRealIPHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Real-IP", "1.2.3.4")
	req.RemoteAddr = "127.0.0.1:9999"
	if got := clientIP(req); got != "1.2.3.4" {
		t.Errorf("clientIP = %q, want 1.2.3.4", got)
	}
}

func TestClientIP_XForwardedForHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "5.6.7.8, 9.10.11.12")
	req.RemoteAddr = "127.0.0.1:9999"
	if got := clientIP(req); got != "5.6.7.8" {
		t.Errorf("clientIP = %q, want 5.6.7.8 (first in chain)", got)
	}
}

func TestClientIP_FallbackRemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	if got := clientIP(req); got != "192.168.1.100" {
		t.Errorf("clientIP = %q, want 192.168.1.100", got)
	}
}

// ---- checkOrigin ----

func TestCheckOrigin_NoAllowedOriginsConfigured_AllowsAll(t *testing.T) {
	s := minimalServer(t)
	s.cfg.Gateway.AllowedOrigins = nil

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	if !s.checkOrigin(req) {
		t.Error("should allow any origin when AllowedOrigins is empty")
	}
}

func TestCheckOrigin_EmptyOriginHeader_AlwaysAllowed(t *testing.T) {
	s := minimalServer(t)
	s.cfg.Gateway.AllowedOrigins = []string{"https://app.example.com"}

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	// No Origin header → non-browser client
	if !s.checkOrigin(req) {
		t.Error("empty Origin header should always be allowed (CLI / SDK clients)")
	}
}

func TestCheckOrigin_MatchingOrigin_Allowed(t *testing.T) {
	s := minimalServer(t)
	s.cfg.Gateway.AllowedOrigins = []string{"https://app.example.com", "https://dashboard.example.com"}

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Origin", "https://dashboard.example.com")
	if !s.checkOrigin(req) {
		t.Error("matching origin should be allowed")
	}
}

func TestCheckOrigin_WildcardAllowsAll(t *testing.T) {
	s := minimalServer(t)
	s.cfg.Gateway.AllowedOrigins = []string{"*"}

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Origin", "https://any-origin.example.com")
	if !s.checkOrigin(req) {
		t.Error("wildcard origin should allow all")
	}
}

func TestCheckOrigin_UnknownOrigin_Rejected(t *testing.T) {
	s := minimalServer(t)
	s.cfg.Gateway.AllowedOrigins = []string{"https://allowed.example.com"}

	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	if s.checkOrigin(req) {
		t.Error("unknown origin should be rejected")
	}
}

// ---- desktopCORS ----

func TestDesktopCORS_SetsHeaders(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := desktopCORS(next)

	req := httptest.NewRequest(http.MethodGet, "/v1/agents", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("CORS origin header missing")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestDesktopCORS_OptionsRequest_Returns204(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should not be called for OPTIONS
		t.Error("next handler called for OPTIONS — should short-circuit")
	})
	handler := desktopCORS(next)

	req := httptest.NewRequest(http.MethodOptions, "/v1/agents", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("OPTIONS status = %d, want 204", w.Code)
	}
}

// ---- isOwnerID ----

func TestIsOwnerID_MatchesConfiguredOwner(t *testing.T) {
	if !isOwnerID("alice", []string{"alice", "bob"}) {
		t.Error("alice should be recognized as owner")
	}
}

func TestIsOwnerID_EmptyUserID_NotOwner(t *testing.T) {
	if isOwnerID("", []string{"alice"}) {
		t.Error("empty user ID should never be owner")
	}
}

func TestIsOwnerID_EmptyOwnerList_OnlySystemIsOwner(t *testing.T) {
	if !isOwnerID("system", nil) {
		t.Error("'system' should be default owner when no owner IDs configured")
	}
	if isOwnerID("admin", nil) {
		t.Error("non-system user should not be owner when no owner IDs configured")
	}
}

func TestIsOwnerID_UnknownUser_NotOwner(t *testing.T) {
	if isOwnerID("charlie", []string{"alice", "bob"}) {
		t.Error("charlie is not in owner list")
	}
}

// ---- helpers ----

func containsSubstr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
