package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// fakeOAuthProvider is a minimal OAuthTokenProvider for resolveServerCredentials tests.
type fakeOAuthProvider struct {
	token string
	err   error
	calls int
}

func (f *fakeOAuthProvider) GetValidToken(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ string) (string, error) {
	f.calls++
	return f.token, f.err
}

func oauthAccessInfo(apiKey string) store.MCPAccessInfo {
	return store.MCPAccessInfo{Server: store.MCPServerData{
		BaseModel: store.BaseModel{ID: uuid.New()},
		Name:      "oauth-srv",
		Transport: "streamable-http",
		URL:       "http://127.0.0.1:1/mcp",
		APIKey:    apiKey,
		Enabled:   true,
		Settings:  json.RawMessage(`{"oauth":{"auth_type":"oauth"}}`),
	}}
}

// OAuth server + no token → resolveServerCredentials must return nil (skip),
// NOT fall back to the server-level api_key.
func TestResolveServerCredentials_OAuthSkipsWithoutToken(t *testing.T) {
	prov := &fakeOAuthProvider{token: ""}
	m := &Manager{oauthTokenProvider: prov}

	rs := m.resolveServerCredentials(context.Background(), oauthAccessInfo("shared-server-key"), "")
	if rs != nil {
		t.Fatalf("expected nil (skip) for OAuth server without token, got headers=%v", rs.headers)
	}
	if prov.calls == 0 {
		t.Error("expected GetValidToken to be consulted")
	}
}

// OAuth server + valid token → Authorization must be the token, never the api_key.
func TestResolveServerCredentials_OAuthUsesTokenNotApiKey(t *testing.T) {
	m := &Manager{oauthTokenProvider: &fakeOAuthProvider{token: "tok-123"}}

	rs := m.resolveServerCredentials(context.Background(), oauthAccessInfo("shared-server-key"), "")
	if rs == nil {
		t.Fatal("expected resolved server when token present")
	}
	if got := rs.headers["Authorization"]; got != "Bearer tok-123" {
		t.Errorf("Authorization = %q, want %q (token, not server api_key)", got, "Bearer tok-123")
	}
}

// OAuth server but no provider wired → skip (cannot obtain a token).
func TestResolveServerCredentials_OAuthNoProviderSkips(t *testing.T) {
	m := &Manager{} // oauthTokenProvider == nil

	rs := m.resolveServerCredentials(context.Background(), oauthAccessInfo("shared-server-key"), "")
	if rs != nil {
		t.Fatal("expected nil (skip) for OAuth server with no provider wired")
	}
}

// Non-OAuth server → server-level api_key is still injected as before.
func TestResolveServerCredentials_NonOAuthUsesApiKey(t *testing.T) {
	m := &Manager{}
	info := store.MCPAccessInfo{Server: store.MCPServerData{
		BaseModel: store.BaseModel{ID: uuid.New()},
		Name:      "plain-srv",
		Transport: "streamable-http",
		URL:       "http://127.0.0.1:1/mcp",
		APIKey:    "shared-server-key",
		Enabled:   true,
	}}

	rs := m.resolveServerCredentials(context.Background(), info, "")
	if rs == nil {
		t.Fatal("expected resolved server for non-OAuth")
	}
	if got := rs.headers["Authorization"]; got != "Bearer shared-server-key" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer shared-server-key")
	}
}
