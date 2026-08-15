package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type fakeConfigPermStore struct {
	writers []store.ConfigPermission
}

func (s *fakeConfigPermStore) CheckPermission(context.Context, uuid.UUID, string, string, string) (bool, error) {
	return false, nil
}
func (s *fakeConfigPermStore) Grant(context.Context, *store.ConfigPermission) error { return nil }
func (s *fakeConfigPermStore) Revoke(context.Context, uuid.UUID, string, string, string) error {
	return nil
}
func (s *fakeConfigPermStore) List(context.Context, uuid.UUID, string, string) ([]store.ConfigPermission, error) {
	return nil, nil
}
func (s *fakeConfigPermStore) ListFileWriters(context.Context, uuid.UUID, string) ([]store.ConfigPermission, error) {
	return s.writers, nil
}

func TestChannelWriterTestAllowsConfiguredWriter(t *testing.T) {
	token := "writer-read-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {Scopes: []string{"operator.write"}, OwnerID: "caller"},
	})
	agentID := uuid.New()
	instID := uuid.New()
	handler := NewChannelInstancesHandler(
		&stubChannelInstanceStore{inst: &store.ChannelInstanceData{BaseModel: store.BaseModel{ID: instID}, Name: "telegram", AgentID: agentID}},
		nil,
		&fakeConfigPermStore{writers: []store.ConfigPermission{{UserID: "386246614", Permission: "allow"}}},
		nil, nil, nil,
	)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/channels/instances/"+instID.String()+"/writers/test", strings.NewReader(`{"group_id":"group:telegram:-100123","user_id":"386246614"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Allowed     bool   `json:"allowed"`
		Reason      string `json:"reason"`
		WriterCount int    `json:"writer_count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.Allowed || body.Reason != "writer" || body.WriterCount != 1 {
		t.Fatalf("body = %+v", body)
	}
}

func TestChannelWriterTestRejectsWrongGroupScope(t *testing.T) {
	token := "writer-invalid-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {Scopes: []string{"operator.write"}, OwnerID: "caller"},
	})
	instID := uuid.New()
	handler := NewChannelInstancesHandler(
		&stubChannelInstanceStore{inst: &store.ChannelInstanceData{BaseModel: store.BaseModel{ID: instID}, Name: "telegram", AgentID: uuid.New()}},
		nil,
		&fakeConfigPermStore{writers: []store.ConfigPermission{{UserID: "386246614", Permission: "allow"}}},
		nil, nil, nil,
	)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/channels/instances/"+instID.String()+"/writers/test", strings.NewReader(`{"group_id":"group:discord:123","user_id":"386246614"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var body struct {
		Allowed bool   `json:"allowed"`
		Reason  string `json:"reason"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Allowed || body.Reason != "invalid_group" {
		t.Fatalf("body = %+v", body)
	}
}
