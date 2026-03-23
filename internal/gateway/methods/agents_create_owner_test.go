package methods

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ---- stub AgentStore that captures Create calls ----

type createCaptureStore struct {
	created *store.AgentData
}

func (s *createCaptureStore) Create(_ context.Context, d *store.AgentData) error {
	d.ID = uuid.New() // simulate DB-assigned UUID so SeedToStore doesn't panic
	s.created = d
	return nil
}
func (s *createCaptureStore) GetByKey(_ context.Context, _ string) (*store.AgentData, error) {
	return nil, nil // agent does not exist yet → allows creation to proceed
}
func (s *createCaptureStore) GetByID(_ context.Context, _ uuid.UUID) (*store.AgentData, error) {
	return nil, nil
}
func (s *createCaptureStore) GetByKeys(_ context.Context, _ []string) ([]store.AgentData, error) {
	return nil, nil
}
func (s *createCaptureStore) GetByIDs(_ context.Context, _ []uuid.UUID) ([]store.AgentData, error) {
	return nil, nil
}
func (s *createCaptureStore) Update(_ context.Context, _ uuid.UUID, _ map[string]any) error {
	return nil
}
func (s *createCaptureStore) Delete(_ context.Context, _ uuid.UUID) error { return nil }
func (s *createCaptureStore) List(_ context.Context, _ string) ([]store.AgentData, error) {
	return nil, nil
}
func (s *createCaptureStore) GetDefault(_ context.Context) (*store.AgentData, error) {
	return nil, nil
}
func (s *createCaptureStore) ShareAgent(_ context.Context, _ uuid.UUID, _, _, _ string) error {
	return nil
}
func (s *createCaptureStore) RevokeShare(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}
func (s *createCaptureStore) ListShares(_ context.Context, _ uuid.UUID) ([]store.AgentShareData, error) {
	return nil, nil
}
func (s *createCaptureStore) CanAccess(_ context.Context, _ uuid.UUID, _ string) (bool, string, error) {
	return true, "owner", nil
}
func (s *createCaptureStore) ListAccessible(_ context.Context, _ string) ([]store.AgentData, error) {
	return nil, nil
}
func (s *createCaptureStore) GetAgentContextFiles(_ context.Context, _ uuid.UUID) ([]store.AgentContextFileData, error) {
	return nil, nil
}
func (s *createCaptureStore) SetAgentContextFile(_ context.Context, _ uuid.UUID, _, _ string) error {
	return nil
}
func (s *createCaptureStore) GetUserContextFiles(_ context.Context, _ uuid.UUID, _ string) ([]store.UserContextFileData, error) {
	return nil, nil
}
func (s *createCaptureStore) SetUserContextFile(_ context.Context, _ uuid.UUID, _, _, _ string) error {
	return nil
}
func (s *createCaptureStore) DeleteUserContextFile(_ context.Context, _ uuid.UUID, _, _ string) error {
	return nil
}
func (s *createCaptureStore) GetUserOverride(_ context.Context, _ uuid.UUID, _ string) (*store.UserAgentOverrideData, error) {
	return nil, nil
}
func (s *createCaptureStore) SetUserOverride(_ context.Context, _ *store.UserAgentOverrideData) error {
	return nil
}
func (s *createCaptureStore) GetOrCreateUserProfile(_ context.Context, _ uuid.UUID, _, _, _ string) (bool, string, error) {
	return false, "", nil
}
func (s *createCaptureStore) ListUserInstances(_ context.Context, _ uuid.UUID) ([]store.UserInstanceData, error) {
	return nil, nil
}
func (s *createCaptureStore) UpdateUserProfileMetadata(_ context.Context, _ uuid.UUID, _ string, _ map[string]string) error {
	return nil
}
func (s *createCaptureStore) EnsureUserProfile(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}
func (s *createCaptureStore) PropagateContextFile(_ context.Context, _ uuid.UUID, _ string) (int, error) {
	return 0, nil
}

// ---- helpers ----

// minimalConfig returns a config sufficient for handleCreate (provider + model defaults only).
func minimalConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Agents.Defaults.Provider = "anthropic"
	cfg.Agents.Defaults.Model = "claude-3-5-haiku-20241022"
	return cfg
}

// buildCreateRequest marshals params into a RequestFrame for agents.create.
func buildCreateRequest(t *testing.T, params map[string]any) *protocol.RequestFrame {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return &protocol.RequestFrame{
		ID:     "test-req-1",
		Method: protocol.MethodAgentsCreate,
		Params: raw,
	}
}

// nullClient returns a zero-value Client. Its send channel is nil, so SendResponse
// safely falls to the select default branch — no panic, response silently dropped.
func nullClient() *gateway.Client {
	return &gateway.Client{}
}

// newManagedMethods returns AgentsMethods wired with the given stub store.
func newManagedMethods(t *testing.T, stub store.AgentStore) *AgentsMethods {
	t.Helper()
	return &AgentsMethods{
		agents:     agent.NewRouter(),
		cfg:        minimalConfig(),
		workspace:  t.TempDir(),
		agentStore: stub,
	}
}

// ---- Tests ----

// TestHandleCreate_UsesProvidedOwnerID verifies that when owner_ids is supplied,
// the first entry is used as the agent's owner_id in the DB — not "system".
func TestHandleCreate_UsesProvidedOwnerID(t *testing.T) {
	stub := &createCaptureStore{}
	m := newManagedMethods(t, stub)

	req := buildCreateRequest(t, map[string]any{
		"name":       "Test Agent",
		"agent_type": "predefined",
		"owner_ids":  []string{"8514594032"},
	})

	m.handleCreate(context.Background(), nullClient(), req)

	if stub.created == nil {
		t.Fatal("agentStore.Create was not called")
	}
	if stub.created.OwnerID != "8514594032" {
		t.Errorf("OwnerID = %q, want %q", stub.created.OwnerID, "8514594032")
	}
}

// TestHandleCreate_FallsBackToSystem_WhenOwnerIDsAbsent verifies that omitting
// owner_ids preserves backward compat — agent is created with owner_id = "system".
func TestHandleCreate_FallsBackToSystem_WhenOwnerIDsAbsent(t *testing.T) {
	stub := &createCaptureStore{}
	m := newManagedMethods(t, stub)

	req := buildCreateRequest(t, map[string]any{
		"name":       "Test Agent Two",
		"agent_type": "predefined",
		// owner_ids intentionally absent
	})

	m.handleCreate(context.Background(), nullClient(), req)

	if stub.created == nil {
		t.Fatal("agentStore.Create was not called")
	}
	if stub.created.OwnerID != "system" {
		t.Errorf("OwnerID = %q, want %q", stub.created.OwnerID, "system")
	}
}

// TestHandleCreate_FallsBackToSystem_WhenOwnerIDsEmpty verifies that an empty
// owner_ids slice also falls back to "system".
func TestHandleCreate_FallsBackToSystem_WhenOwnerIDsEmpty(t *testing.T) {
	stub := &createCaptureStore{}
	m := newManagedMethods(t, stub)

	req := buildCreateRequest(t, map[string]any{
		"name":      "Test Agent Three",
		"owner_ids": []string{},
	})

	m.handleCreate(context.Background(), nullClient(), req)

	if stub.created == nil {
		t.Fatal("agentStore.Create was not called")
	}
	if stub.created.OwnerID != "system" {
		t.Errorf("OwnerID = %q, want %q", stub.created.OwnerID, "system")
	}
}

// TestHandleCreate_MultipleOwnerIDs_UsesFirst verifies that when multiple IDs
// are supplied, only the first entry is used as owner_id.
func TestHandleCreate_MultipleOwnerIDs_UsesFirst(t *testing.T) {
	stub := &createCaptureStore{}
	m := newManagedMethods(t, stub)

	req := buildCreateRequest(t, map[string]any{
		"name":      "Test Agent Four",
		"owner_ids": []string{"first-owner", "second-owner"},
	})

	m.handleCreate(context.Background(), nullClient(), req)

	if stub.created == nil {
		t.Fatal("agentStore.Create was not called")
	}
	if stub.created.OwnerID != "first-owner" {
		t.Errorf("OwnerID = %q, want %q", stub.created.OwnerID, "first-owner")
	}
}
