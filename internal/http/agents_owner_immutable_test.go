package http

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Embeds the interface so unexpected method calls panic — surfaces test drift.
type stubAgentStoreForUpdate struct {
	store.AgentStore
	agent       *store.AgentData
	updatedWith map[string]any
	updateCount int
}

func (s *stubAgentStoreForUpdate) GetByID(_ context.Context, _ uuid.UUID) (*store.AgentData, error) {
	return s.agent, nil
}

func (s *stubAgentStoreForUpdate) Update(_ context.Context, _ uuid.UUID, updates map[string]any) error {
	s.updatedWith = updates
	s.updateCount++
	return nil
}

func setupOwnerImmutableTest(t *testing.T) (*http.ServeMux, *stubAgentStoreForUpdate, string) {
	t.Helper()
	const token = "admin-token"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {
			ID:       uuid.New(),
			Scopes:   []string{"operator.admin"},
			TenantID: store.MasterTenantID,
		},
	})

	stub := &stubAgentStoreForUpdate{
		agent: &store.AgentData{
			BaseModel: store.BaseModel{ID: uuid.New()},
			TenantID:  store.MasterTenantID,
			AgentKey:  "test-agent",
			AgentType: store.AgentTypePredefined,
			OwnerID:   "alice",
			Provider:  "anthropic",
			Model:     "claude-sonnet-4-5",
		},
	}

	h := NewAgentsHandler(stub, nil, nil, nil, nil, "", nil, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux, stub, token
}

func TestPutAgentRejectsOwnerID(t *testing.T) {
	mux, stub, token := setupOwnerImmutableTest(t)

	body := `{"owner_id":"bob"}`
	req := httptest.NewRequest(http.MethodPut, "/v1/agents/"+stub.agent.ID.String(), bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", w.Code, w.Body.String())
	}
	wantMsg := i18n.T("en", i18n.MsgOwnerIDImmutable)
	if !strings.Contains(w.Body.String(), wantMsg) {
		t.Fatalf("body = %q, want substring %q", w.Body.String(), wantMsg)
	}
	if stub.updateCount != 0 {
		t.Fatalf("Update called %d times, want 0 (reject must not mutate)", stub.updateCount)
	}
}

func TestPutAgentAllowsBodyWithoutOwnerID(t *testing.T) {
	mux, stub, token := setupOwnerImmutableTest(t)

	body := `{"model":"claude-opus-4-1"}`
	req := httptest.NewRequest(http.MethodPut, "/v1/agents/"+stub.agent.ID.String(), bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if stub.updateCount != 1 {
		t.Fatalf("Update called %d times, want 1", stub.updateCount)
	}
	if got := stub.updatedWith["model"]; got != "claude-opus-4-1" {
		t.Fatalf("Update model = %v, want claude-opus-4-1", got)
	}
}
