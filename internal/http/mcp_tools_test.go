package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// recordingMCPOAuthProvider records the userID arg passed to GetValidToken so we
// can assert the scope (global "" vs per-user) used by the preview endpoints.
type recordingMCPOAuthProvider struct {
	mu         sync.Mutex
	calls      int
	lastUserID string
	token      string
}

func (p *recordingMCPOAuthProvider) GetValidToken(_ context.Context, _ uuid.UUID, _ uuid.UUID, userID string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.lastUserID = userID
	return p.token, nil
}

// oauthServerWithRequireUserCreds returns an OAuth server that ALSO has
// require_user_credentials=true — the case where the old code wrongly used the
// caller's per-user token scope for discovery/test.
func oauthServerWithRequireUserCreds(id uuid.UUID) *store.MCPServerData {
	return &store.MCPServerData{
		BaseModel: store.BaseModel{ID: id},
		Name:      "oauth-srv",
		Transport: "streamable-http",
		URL:       "http://127.0.0.1:1/mcp",
		Settings:  json.RawMessage(`{"oauth":{"auth_type":"oauth"},"require_user_credentials":true}`),
	}
}

// ctxWithUser attaches a tenant + caller user id (the old code would have used
// this caller id as the OAuth scope).
func ctxWithUser(r *http.Request, userID string) *http.Request {
	ctx := store.WithTenantID(store.WithUserID(r.Context(), userID), uuid.New())
	return r.WithContext(ctx)
}

// list-tools must use the GLOBAL OAuth token scope (userID="") even when the
// server has require_user_credentials=true — discovery is server-wide.
func TestHandleListServerToolsUsesGlobalOAuthScope(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	st := newMockMCPServerForOAuth()
	id := uuid.New()
	st.servers[id] = oauthServerWithRequireUserCreds(id)

	prov := &recordingMCPOAuthProvider{token: ""} // not authorized → discovery returns empty
	h := NewMCPHandler(st, nil, nil)
	h.SetOAuthProvider(prov)

	req := httptest.NewRequest(http.MethodGet, "/v1/mcp/servers/"+id.String()+"/tools", nil)
	req.SetPathValue("id", id.String())
	req = ctxWithUser(req, "alice")
	rec := httptest.NewRecorder()
	h.handleListServerTools(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	prov.mu.Lock()
	defer prov.mu.Unlock()
	if prov.calls == 0 {
		t.Fatal("expected GetValidToken to be called for OAuth server")
	}
	if prov.lastUserID != "" {
		t.Errorf("list-tools must use GLOBAL scope (userID=\"\"), got %q", prov.lastUserID)
	}
}

// test-connection must use the GLOBAL OAuth token scope (userID="") for OAuth
// servers, regardless of require_user_credentials.
func TestHandleTestConnectionUsesGlobalOAuthScope(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	st := newMockMCPServerForOAuth()
	id := uuid.New()
	st.servers[id] = oauthServerWithRequireUserCreds(id)

	prov := &recordingMCPOAuthProvider{token: ""}
	h := NewMCPHandler(st, nil, nil)
	h.SetOAuthProvider(prov)

	body := `{"server_id":"` + id.String() + `","transport":"streamable-http","url":"http://127.0.0.1:1/mcp","headers":{"Authorization":"Bearer body-token"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/mcp/servers/test", strings.NewReader(body))
	req = ctxWithUser(req, "alice")
	rec := httptest.NewRecorder()
	h.handleTestConnection(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	prov.mu.Lock()
	defer prov.mu.Unlock()
	if prov.calls == 0 {
		t.Fatal("expected GetValidToken to be called for OAuth server")
	}
	if prov.lastUserID != "" {
		t.Errorf("test-connection must use GLOBAL scope (userID=\"\"), got %q", prov.lastUserID)
	}
}
