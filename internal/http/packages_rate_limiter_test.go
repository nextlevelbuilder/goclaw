package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPerKeyRateLimiter_AllowThenBlock(t *testing.T) {
	rl := newPerKeyRateLimiter(60, 2) // 1 rps, burst 2

	// First two requests for key A succeed (burst).
	for i := 0; i < 2; i++ {
		if !rl.Allow("A") {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	// Third immediate request should be blocked.
	if rl.Allow("A") {
		t.Error("third immediate request should be rate-limited")
	}
	// Independent key is unaffected.
	if !rl.Allow("B") {
		t.Error("different key should not be rate-limited")
	}
}

func TestPerKeyRateLimiter_Disabled(t *testing.T) {
	rl := newPerKeyRateLimiter(0, 5)
	for i := 0; i < 100; i++ {
		if !rl.Allow("x") {
			t.Fatalf("disabled limiter should always allow (i=%d)", i)
		}
	}
}

func TestRateLimitKeyFromRequest(t *testing.T) {
	// Authenticated user: prefix with uid:.
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("X-GoClaw-User-Id", "alice")
	if got := rateLimitKeyFromRequest(r); got != "uid:alice" {
		t.Errorf("want uid:alice, got %s", got)
	}

	// Anonymous: fall back to IP.
	r2 := httptest.NewRequest(http.MethodGet, "/x", nil)
	r2.RemoteAddr = "203.0.113.5:54321"
	if got := rateLimitKeyFromRequest(r2); got != "ip:203.0.113.5" {
		t.Errorf("want ip:203.0.113.5, got %s", got)
	}
}

func TestEnforceGitHubReleasesLimit_Writes429(t *testing.T) {
	// Swap the package-level limiter for a tight one, restore after.
	prev := githubReleasesLimiter
	githubReleasesLimiter = newPerKeyRateLimiter(60, 1) // burst 1
	defer func() { githubReleasesLimiter = prev }()

	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("X-GoClaw-User-Id", "bob")

	w1 := httptest.NewRecorder()
	if !enforceGitHubReleasesLimit(w1, r) {
		t.Fatal("first call should pass")
	}
	w2 := httptest.NewRecorder()
	if enforceGitHubReleasesLimit(w2, r) {
		t.Fatal("second call should be throttled")
	}
	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("want 429, got %d", w2.Code)
	}
	if w2.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header missing")
	}
}
