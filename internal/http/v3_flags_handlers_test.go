package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type v3FlagsTestAgentStore struct {
	store.AgentStore
	agent   *store.AgentData
	updated map[string]any
}

func (s *v3FlagsTestAgentStore) GetByID(_ context.Context, id uuid.UUID) (*store.AgentData, error) {
	if s.agent != nil && s.agent.ID == id {
		return s.agent, nil
	}
	return nil, nil
}

func (s *v3FlagsTestAgentStore) Update(_ context.Context, _ uuid.UUID, u map[string]any) error {
	s.updated = u
	return nil
}

func newV3FlagsTestMux(t *testing.T) (*http.ServeMux, *v3FlagsTestAgentStore, uuid.UUID) {
	t.Helper()
	setupTestToken(t, "gw-token")
	agentID := uuid.New()
	as := &v3FlagsTestAgentStore{agent: &store.AgentData{
		BaseModel: store.BaseModel{ID: agentID},
		TenantID:  store.MasterTenantID,
	}}
	ts := newMockTenantStore()
	ts.roles[store.MasterTenantID] = map[string]string{
		"viewer-user":   store.TenantRoleViewer,
		"operator-user": store.TenantRoleOperator,
		"admin-user":    store.TenantRoleAdmin,
		"owner-user":    store.TenantRoleOwner,
	}
	mux := http.NewServeMux()
	NewV3FlagsHandler(as, ts).RegisterRoutes(mux)
	return mux, as, agentID
}

func doV3FlagsRequest(mux *http.ServeMux, method string, agentID uuid.UUID, userID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/v1/agents/"+agentID.String()+"/v3-flags", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer gw-token")
	req.Header.Set("X-GoClaw-User-Id", userID)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// Regression for #1119: the gateway token maps any tenant member to RoleAdmin,
// so PATCH must additionally require tenant owner/admin membership.
func TestV3FlagsPatch_RequiresTenantAdmin(t *testing.T) {
	const body = `{"self_evolution_suggestions":true}`
	for _, tc := range []struct {
		user string
		want int
	}{
		{"viewer-user", http.StatusForbidden},
		{"operator-user", http.StatusForbidden},
		{"non-member", http.StatusForbidden},
		{"admin-user", http.StatusOK},
		{"owner-user", http.StatusOK},
	} {
		t.Run(tc.user, func(t *testing.T) {
			mux, as, agentID := newV3FlagsTestMux(t)
			rec := doV3FlagsRequest(mux, http.MethodPatch, agentID, tc.user, body)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
			if wrote := as.updated != nil; wrote != (tc.want == http.StatusOK) {
				t.Fatalf("other_config updated = %v, want %v", wrote, tc.want == http.StatusOK)
			}
		})
	}
}

// Normal behaviour: reading flags stays available to tenant viewers.
func TestV3FlagsGet_ViewerAllowed(t *testing.T) {
	mux, _, agentID := newV3FlagsTestMux(t)
	rec := doV3FlagsRequest(mux, http.MethodGet, agentID, "viewer-user", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

// Local/dev mode (no gateway token, no-auth fallback) has no tenant
// membership rows; the master-tenant admin must still be able to toggle flags.
func TestV3FlagsPatch_DevModeAllowed(t *testing.T) {
	mux, as, agentID := newV3FlagsTestMux(t)
	setupTestToken(t, "")
	setupTestNoAuthFallback(t, true)

	req := httptest.NewRequest(http.MethodPatch, "/v1/agents/"+agentID.String()+"/v3-flags",
		strings.NewReader(`{"self_evolution_suggestions":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("dev-mode PATCH status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if as.updated == nil {
		t.Fatal("dev-mode PATCH did not update the agent")
	}
}
