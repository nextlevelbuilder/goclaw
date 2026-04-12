package http

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type stubHTTPAgentStore struct {
	listFn           func(ctx context.Context, ownerID string) ([]store.AgentData, error)
	listAccessibleFn func(ctx context.Context, userID string) ([]store.AgentData, error)
}

func (s *stubHTTPAgentStore) Create(_ context.Context, _ *store.AgentData) error { return nil }
func (s *stubHTTPAgentStore) GetByKey(_ context.Context, _ string) (*store.AgentData, error) {
	return nil, nil
}
func (s *stubHTTPAgentStore) GetByID(_ context.Context, _ uuid.UUID) (*store.AgentData, error) {
	return nil, nil
}
func (s *stubHTTPAgentStore) GetByIDUnscoped(_ context.Context, _ uuid.UUID) (*store.AgentData, error) {
	return nil, nil
}
func (s *stubHTTPAgentStore) GetByKeys(_ context.Context, _ []string) ([]store.AgentData, error) {
	return nil, nil
}
func (s *stubHTTPAgentStore) GetByIDs(_ context.Context, _ []uuid.UUID) ([]store.AgentData, error) {
	return nil, nil
}
func (s *stubHTTPAgentStore) Update(_ context.Context, _ uuid.UUID, _ map[string]any) error {
	return nil
}
func (s *stubHTTPAgentStore) Delete(_ context.Context, _ uuid.UUID) error { return nil }
func (s *stubHTTPAgentStore) List(ctx context.Context, ownerID string) ([]store.AgentData, error) {
	if s.listFn != nil {
		return s.listFn(ctx, ownerID)
	}
	return nil, nil
}
func (s *stubHTTPAgentStore) GetDefault(_ context.Context) (*store.AgentData, error) { return nil, nil }
func (s *stubHTTPAgentStore) ShareAgent(_ context.Context, _ uuid.UUID, _, _, _ string) error {
	return nil
}
func (s *stubHTTPAgentStore) RevokeShare(_ context.Context, _ uuid.UUID, _ string) error { return nil }
func (s *stubHTTPAgentStore) ListShares(_ context.Context, _ uuid.UUID) ([]store.AgentShareData, error) {
	return nil, nil
}
func (s *stubHTTPAgentStore) CanAccess(_ context.Context, _ uuid.UUID, _ string) (bool, string, error) {
	return true, "admin", nil
}
func (s *stubHTTPAgentStore) ListAccessible(ctx context.Context, userID string) ([]store.AgentData, error) {
	if s.listAccessibleFn != nil {
		return s.listAccessibleFn(ctx, userID)
	}
	return nil, nil
}
func (s *stubHTTPAgentStore) GetAgentContextFiles(_ context.Context, _ uuid.UUID) ([]store.AgentContextFileData, error) {
	return nil, nil
}
func (s *stubHTTPAgentStore) SetAgentContextFile(_ context.Context, _ uuid.UUID, _, _ string) error {
	return nil
}
func (s *stubHTTPAgentStore) PropagateContextFile(_ context.Context, _ uuid.UUID, _ string) (int, error) {
	return 0, nil
}
func (s *stubHTTPAgentStore) GetUserContextFiles(_ context.Context, _ uuid.UUID, _ string) ([]store.UserContextFileData, error) {
	return nil, nil
}
func (s *stubHTTPAgentStore) ListUserContextFilesByName(_ context.Context, _ uuid.UUID, _ string) ([]store.UserContextFileData, error) {
	return nil, nil
}
func (s *stubHTTPAgentStore) SetUserContextFile(_ context.Context, _ uuid.UUID, _, _, _ string) error {
	return nil
}
func (s *stubHTTPAgentStore) DeleteUserContextFile(_ context.Context, _ uuid.UUID, _, _ string) error {
	return nil
}
func (s *stubHTTPAgentStore) MigrateUserDataOnMerge(_ context.Context, _ []string, _ string) error {
	return nil
}
func (s *stubHTTPAgentStore) GetUserOverride(_ context.Context, _ uuid.UUID, _ string) (*store.UserAgentOverrideData, error) {
	return nil, nil
}
func (s *stubHTTPAgentStore) SetUserOverride(_ context.Context, _ *store.UserAgentOverrideData) error {
	return nil
}
func (s *stubHTTPAgentStore) GetOrCreateUserProfile(_ context.Context, _ uuid.UUID, _, _, _ string) (bool, string, error) {
	return false, "", nil
}
func (s *stubHTTPAgentStore) EnsureUserProfile(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}
func (s *stubHTTPAgentStore) ListUserInstances(_ context.Context, _ uuid.UUID) ([]store.UserInstanceData, error) {
	return nil, nil
}
func (s *stubHTTPAgentStore) UpdateUserProfileMetadata(_ context.Context, _ uuid.UUID, _ string, _ map[string]string) error {
	return nil
}

func TestAgentsHandleList_EmptySliceNeverNull(t *testing.T) {
	h := &AgentsHandler{
		agents: &stubHTTPAgentStore{
			listAccessibleFn: func(context.Context, string) ([]store.AgentData, error) {
				return nil, nil
			},
		},
	}

	req := httptest.NewRequest("GET", "/v1/agents", nil)
	req = req.WithContext(store.WithUserID(req.Context(), "user-1"))
	rec := httptest.NewRecorder()

	h.handleList(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var payload struct {
		Agents []store.AgentData `json:"agents"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Agents == nil {
		t.Fatal("expected agents to encode as [], got null")
	}
	if got := len(payload.Agents); got != 0 {
		t.Fatalf("expected empty agents slice, got %d entries", got)
	}
}
