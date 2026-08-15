package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// setupUpdatePurgeHandler builds an MCPHandler wired with an in-memory server
// store (seeded with one OAuth-enabled server), an OAuth token store holding a
// global + per-user token for that server, and a recording pool evictor.
func setupUpdatePurgeHandler(t *testing.T, settings string) (*http.ServeMux, uuid.UUID, *mockOAuthTokenStore, *mockPoolEvictor) {
	t.Helper()
	setAdminToken(t, "test-admin-token")

	id := uuid.New()
	srvStore := newMockMCPServerForOAuth()
	srvStore.servers[id] = &store.MCPServerData{
		BaseModel: store.BaseModel{ID: id},
		Name:      "test-server",
		Transport: "streamable-http",
		URL:       "http://127.0.0.1:9/old",
		Settings:  json.RawMessage(settings),
	}

	oauthStore := newMockOAuthTokenStore()
	oauthStore.global[id.String()] = &store.MCPOAuthToken{ServerID: id}
	oauthStore.perUser[id.String()+":alice"] = &store.MCPOAuthToken{ServerID: id, UserID: "alice"}

	evictor := &mockPoolEvictor{}
	h := NewMCPHandler(srvStore, nil, nil)
	h.SetOAuthStore(oauthStore)
	h.SetPoolEvictor(evictor)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux, id, oauthStore, evictor
}

const oauthSettingsClientC1 = `{"oauth":{"auth_type":"oauth","client_id":"c1","token_endpoint":"http://127.0.0.1:9/token"}}`

func TestHandleUpdateServerPurgesOAuthOnURLChange(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	mux, id, oauthStore, evictor := setupUpdatePurgeHandler(t, oauthSettingsClientC1)

	body := map[string]any{"url": "http://127.0.0.1:9/new"}
	req := adminRequest(http.MethodPut, "/v1/mcp/servers/"+id.String(), body)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if oauthStore.deletes == 0 {
		t.Error("expected OAuth tokens to be purged on URL change")
	}
	if _, ok := oauthStore.global[id.String()]; ok {
		t.Error("global token should have been deleted")
	}
	if _, ok := oauthStore.perUser[id.String()+":alice"]; ok {
		t.Error("per-user token should have been deleted")
	}
	if evictor.evictServerCalls == 0 {
		t.Error("expected EvictServer to be called on URL change")
	}
}

func TestHandleUpdateServerPurgesOAuthOnOAuthConfigChange(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	mux, id, oauthStore, evictor := setupUpdatePurgeHandler(t, oauthSettingsClientC1)

	// Same URL, but client_id changes c1 -> c2 → tokens for c1 are now invalid.
	body := map[string]any{
		"settings": map[string]any{
			"oauth": map[string]any{
				"auth_type":      "oauth",
				"client_id":      "c2",
				"token_endpoint": "http://127.0.0.1:9/token",
			},
		},
	}
	req := adminRequest(http.MethodPut, "/v1/mcp/servers/"+id.String(), body)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if _, ok := oauthStore.global[id.String()]; ok {
		t.Error("global token should have been deleted on OAuth config change")
	}
	if evictor.evictServerCalls == 0 {
		t.Error("expected EvictServer to be called on OAuth config change")
	}
}

func TestHandleUpdateServerKeepsOAuthOnUnrelatedChange(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	mux, id, oauthStore, evictor := setupUpdatePurgeHandler(t, oauthSettingsClientC1)

	// Change an unrelated field; URL and oauth config are untouched.
	body := map[string]any{"tool_prefix": "tp_"}
	req := adminRequest(http.MethodPut, "/v1/mcp/servers/"+id.String(), body)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if oauthStore.deletes != 0 {
		t.Errorf("tokens must NOT be purged on unrelated change, deletes = %d", oauthStore.deletes)
	}
	if _, ok := oauthStore.global[id.String()]; !ok {
		t.Error("global token should still exist")
	}
	if evictor.evictServerCalls != 0 {
		t.Errorf("EvictServer must NOT be called on unrelated change, calls = %d", evictor.evictServerCalls)
	}
}
