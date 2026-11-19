package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type evoAuthSuggestionStore struct {
	store.EvolutionSuggestionStore
	s        store.EvolutionSuggestion
	statusTo string
}

func (m *evoAuthSuggestionStore) GetSuggestion(_ context.Context, id uuid.UUID) (*store.EvolutionSuggestion, error) {
	if id != m.s.ID {
		return nil, nil
	}
	cp := m.s
	return &cp, nil
}

func (m *evoAuthSuggestionStore) UpdateSuggestionStatus(_ context.Context, _ uuid.UUID, status, _ string) error {
	m.statusTo = status
	return nil
}

type evoAuthToolCfgStore struct {
	store.BuiltinToolTenantConfigStore
	disabled []string
}

func (m *evoAuthToolCfgStore) Set(_ context.Context, _ uuid.UUID, tool string, enabled bool) error {
	if !enabled {
		m.disabled = append(m.disabled, tool)
	}
	return nil
}

func evoAuthTenantStore() *mockTenantStore {
	ts := newMockTenantStore()
	ts.roles[store.MasterTenantID] = map[string]string{
		"viewer-user":   store.TenantRoleViewer,
		"operator-user": store.TenantRoleOperator,
		"admin-user":    store.TenantRoleAdmin,
		"owner-user":    store.TenantRoleOwner,
	}
	return ts
}

// Regression for #1120: approving / rejecting an agent evolution suggestion is
// an admin action (approval of a tool_order suggestion disables a builtin tool
// for the whole tenant). The gateway token maps any tenant member to
// RoleAdmin, so the handler must also require tenant owner/admin membership.
func TestEvolutionSuggestionPatch_RequiresTenantAdmin(t *testing.T) {
	for _, tc := range []struct {
		user   string
		status string
		want   int
	}{
		{"viewer-user", "approved", http.StatusForbidden},
		{"viewer-user", "rejected", http.StatusForbidden},
		{"operator-user", "approved", http.StatusForbidden},
		{"non-member", "approved", http.StatusForbidden},
		{"admin-user", "approved", http.StatusOK},
		{"owner-user", "rejected", http.StatusOK},
	} {
		t.Run(tc.user+"_"+tc.status, func(t *testing.T) {
			setupTestToken(t, "gw-token")
			agentID, sugID := uuid.New(), uuid.New()
			params, _ := json.Marshal(map[string]any{"tool": "web_fetch"})
			ss := &evoAuthSuggestionStore{s: store.EvolutionSuggestion{
				ID: sugID, TenantID: store.MasterTenantID, AgentID: agentID,
				SuggestionType: store.SuggestToolOrder, Parameters: params, Status: "pending",
			}}
			tc2 := &evoAuthToolCfgStore{}
			h := NewEvolutionHandler(nil, ss, WithToolTenantCfgs(tc2), WithEvolutionTenantStore(evoAuthTenantStore()))
			mux := http.NewServeMux()
			h.RegisterRoutes(mux)

			req := httptest.NewRequest(http.MethodPatch,
				"/v1/agents/"+agentID.String()+"/evolution/suggestions/"+sugID.String(),
				strings.NewReader(`{"status":"`+tc.status+`"}`))
			req.Header.Set("Authorization", "Bearer gw-token")
			req.Header.Set("X-GoClaw-User-Id", tc.user)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			t.Logf("status=%d body=%s suggestion_status_to=%q tenant_tools_disabled=%v",
				rec.Code, strings.TrimSpace(rec.Body.String()), ss.statusTo, tc2.disabled)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			if tc.want == http.StatusForbidden && (ss.statusTo != "" || len(tc2.disabled) != 0) {
				t.Fatalf("unauthorized caller mutated state: suggestion=%q disabled=%v", ss.statusTo, tc2.disabled)
			}
		})
	}
}

// Normal behaviour: listing suggestions stays available to tenant viewers.
func TestEvolutionSuggestionList_ViewerAllowed(t *testing.T) {
	setupTestToken(t, "gw-token")
	h := NewEvolutionHandler(nil, &evoAuthListStore{}, WithEvolutionTenantStore(evoAuthTenantStore()))
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/agents/"+uuid.NewString()+"/evolution/suggestions", nil)
	req.Header.Set("Authorization", "Bearer gw-token")
	req.Header.Set("X-GoClaw-User-Id", "viewer-user")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

type evoAuthListStore struct{ store.EvolutionSuggestionStore }

func (evoAuthListStore) ListSuggestions(context.Context, uuid.UUID, string, int) ([]store.EvolutionSuggestion, error) {
	return nil, nil
}
