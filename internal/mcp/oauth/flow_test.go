package mcpoauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/security"
)

func TestPKCEVerifierMatchesChallenge(t *testing.T) {
	verifier, challenge, err := generatePKCE()
	if err != nil {
		t.Fatalf("generatePKCE() error: %v", err)
	}
	if verifier == "" || challenge == "" {
		t.Fatal("expected non-empty verifier and challenge")
	}
	h := sha256.Sum256([]byte(verifier))
	want := base64.RawURLEncoding.EncodeToString(h[:])
	if challenge != want {
		t.Errorf("challenge = %q, want %q", challenge, want)
	}
}

func TestGenerateStateLength(t *testing.T) {
	state, err := generateState()
	if err != nil {
		t.Fatalf("generateState() error: %v", err)
	}
	// 16 bytes base64url-encoded → at least 21 chars (no padding).
	if len(state) < 20 {
		t.Errorf("state too short: %q (len %d)", state, len(state))
	}
}

func TestStartFlowReturnsPKCEAuthURL(t *testing.T) {
	fm := NewFlowManager(http.DefaultClient)
	disc := &DiscoveryResult{
		AuthorizationEndpoint: "https://auth.example.com/authorize",
		TokenEndpoint:         "https://auth.example.com/token",
	}
	authURL, state, err := fm.StartFlow(context.Background(), StartFlowParams{
		ServerID:        uuid.New(),
		TenantID:        uuid.New(),
		DiscoveryResult: disc,
		ClientID:        "test-client",
		RedirectURI:     "https://goclaw.example.com/v1/mcp/oauth/callback",
		GrantType:       "pkce",
	})
	if err != nil {
		t.Fatalf("StartFlow() error: %v", err)
	}
	if authURL == "" {
		t.Error("expected non-empty authURL")
	}
	if state == "" || len(state) < 10 {
		t.Errorf("expected valid state token, got %q", state)
	}
	if !strings.Contains(authURL, "code_challenge=") {
		t.Errorf("authURL missing code_challenge: %s", authURL)
	}
	if !strings.Contains(authURL, "code_challenge_method=S256") {
		t.Errorf("authURL missing S256 method: %s", authURL)
	}
	if !strings.Contains(authURL, "state="+state) {
		t.Errorf("authURL missing state param: %s", authURL)
	}
	if !strings.Contains(authURL, "client_id=test-client") {
		t.Errorf("authURL missing client_id: %s", authURL)
	}
}

func TestStartFlowStoresInPending(t *testing.T) {
	fm := NewFlowManager(http.DefaultClient)
	disc := &DiscoveryResult{
		AuthorizationEndpoint: "https://auth.example.com/authorize",
		TokenEndpoint:         "https://auth.example.com/token",
	}
	_, state, err := fm.StartFlow(context.Background(), StartFlowParams{
		ServerID: uuid.New(), TenantID: uuid.New(),
		DiscoveryResult: disc, ClientID: "c",
		RedirectURI: "https://example.com/cb",
	})
	if err != nil {
		t.Fatalf("StartFlow() error: %v", err)
	}
	fm.mu.Lock()
	flow, ok := fm.pending[state]
	fm.mu.Unlock()
	if !ok {
		t.Fatal("pending flow not found")
	}
	if flow.ClientID != "c" {
		t.Errorf("ClientID = %q, want %q", flow.ClientID, "c")
	}
	if !flow.UsePKCE {
		t.Error("expected UsePKCE = true for pkce grant type")
	}
}

func TestExchangeCodeSuccess(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if r.FormValue("grant_type") != "authorization_code" {
			http.Error(w, "wrong grant_type: "+r.FormValue("grant_type"), http.StatusBadRequest)
			return
		}
		if r.FormValue("code_verifier") == "" {
			http.Error(w, "missing code_verifier", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(OAuthTokens{
			AccessToken:  "access-token-123",
			RefreshToken: "refresh-token-456",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
		})
	}))
	defer srv.Close()

	client := security.NewSafeClient(5 * time.Second)
	fm := NewFlowManager(client)
	disc := &DiscoveryResult{
		AuthorizationEndpoint: "https://auth.example.com/authorize",
		TokenEndpoint:         srv.URL,
	}
	_, state, err := fm.StartFlow(context.Background(), StartFlowParams{
		ServerID:        uuid.New(),
		TenantID:        uuid.New(),
		DiscoveryResult: disc,
		ClientID:        "test-client",
		RedirectURI:     srv.URL + "/callback",
		GrantType:       "pkce",
	})
	if err != nil {
		t.Fatalf("StartFlow() error: %v", err)
	}

	tokens, flow, err := fm.ExchangeCode(context.Background(), state, "auth-code-xyz")
	if err != nil {
		t.Fatalf("ExchangeCode() error: %v", err)
	}
	if tokens.AccessToken != "access-token-123" {
		t.Errorf("access_token = %q, want %q", tokens.AccessToken, "access-token-123")
	}
	if tokens.RefreshToken != "refresh-token-456" {
		t.Errorf("refresh_token = %q, want %q", tokens.RefreshToken, "refresh-token-456")
	}
	if flow == nil {
		t.Fatal("expected non-nil PendingFlow")
	}
}

func TestExchangeCodeInvalidState(t *testing.T) {
	fm := NewFlowManager(http.DefaultClient)
	_, _, err := fm.ExchangeCode(context.Background(), "no-such-state", "code")
	if err == nil {
		t.Fatal("expected error for unknown state, got nil")
	}
}

func TestExchangeCodeExpiredFlow(t *testing.T) {
	fm := NewFlowManager(http.DefaultClient)
	fm.mu.Lock()
	fm.pending["expired-state"] = &PendingFlow{
		CreatedAt: time.Now().Add(-11 * time.Minute), // > flowTTL (10 min)
	}
	fm.mu.Unlock()

	_, _, err := fm.ExchangeCode(context.Background(), "expired-state", "code")
	if err == nil {
		t.Fatal("expected error for expired flow, got nil")
	}
}

func TestExchangeCodeReplayPrevented(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(OAuthTokens{AccessToken: "tok", TokenType: "Bearer"})
	}))
	defer srv.Close()

	fm := NewFlowManager(security.NewSafeClient(5 * time.Second))
	disc := &DiscoveryResult{
		AuthorizationEndpoint: "https://auth.example.com/authorize",
		TokenEndpoint:         srv.URL,
	}
	_, state, err := fm.StartFlow(context.Background(), StartFlowParams{
		ServerID: uuid.New(), TenantID: uuid.New(),
		DiscoveryResult: disc, ClientID: "c", RedirectURI: srv.URL + "/cb",
	})
	if err != nil {
		t.Fatalf("StartFlow() error: %v", err)
	}

	// First exchange: succeeds.
	if _, _, err := fm.ExchangeCode(context.Background(), state, "code"); err != nil {
		t.Fatalf("first ExchangeCode() failed: %v", err)
	}
	// Second exchange with same state: state was deleted → must fail.
	if _, _, err := fm.ExchangeCode(context.Background(), state, "code"); err == nil {
		t.Fatal("expected error on replay, got nil")
	}
}

func TestExchangeCodeTokenEndpointError(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	fm := NewFlowManager(security.NewSafeClient(5 * time.Second))
	disc := &DiscoveryResult{
		AuthorizationEndpoint: "https://auth.example.com/authorize",
		TokenEndpoint:         srv.URL,
	}
	_, state, err := fm.StartFlow(context.Background(), StartFlowParams{
		ServerID: uuid.New(), TenantID: uuid.New(),
		DiscoveryResult: disc, ClientID: "c", RedirectURI: srv.URL + "/cb",
	})
	if err != nil {
		t.Fatalf("StartFlow() error: %v", err)
	}

	_, _, err = fm.ExchangeCode(context.Background(), state, "bad-code")
	if err == nil {
		t.Fatal("expected error when token endpoint returns 400, got nil")
	}
}

func TestClientCredentials(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if r.FormValue("grant_type") != "client_credentials" {
			http.Error(w, "wrong grant_type: "+r.FormValue("grant_type"), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(OAuthTokens{AccessToken: "cc-token", TokenType: "Bearer"})
	}))
	defer srv.Close()

	fm := NewFlowManager(security.NewSafeClient(5 * time.Second))
	tokens, err := fm.ClientCredentials(context.Background(), srv.URL, "client-id", "client-secret", "read write", "")
	if err != nil {
		t.Fatalf("ClientCredentials() error: %v", err)
	}
	if tokens.AccessToken != "cc-token" {
		t.Errorf("access_token = %q, want %q", tokens.AccessToken, "cc-token")
	}
}

func TestFlowCleanupPurgesExpired(t *testing.T) {
	fm := NewFlowManager(http.DefaultClient)

	fm.mu.Lock()
	fm.pending["fresh"] = &PendingFlow{CreatedAt: time.Now()}
	fm.pending["old"] = &PendingFlow{CreatedAt: time.Now().Add(-11 * time.Minute)}
	fm.mu.Unlock()

	// Simulate cleanup logic directly (same as cleanupLoop body).
	cutoff := time.Now().Add(-flowTTL)
	fm.mu.Lock()
	for state, flow := range fm.pending {
		if flow.CreatedAt.Before(cutoff) {
			delete(fm.pending, state)
		}
	}
	fm.mu.Unlock()

	fm.mu.Lock()
	_, freshOk := fm.pending["fresh"]
	_, oldOk := fm.pending["old"]
	fm.mu.Unlock()

	if !freshOk {
		t.Error("fresh flow should not be purged")
	}
	if oldOk {
		t.Error("expired flow should be purged by cleanup")
	}
}
