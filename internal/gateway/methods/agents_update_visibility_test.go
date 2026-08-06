package methods

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// updateVisibilityStore is a fixed single agent, resolvable by key or ID, whose
// Update calls are captured. It embeds createCaptureStore's zero-value methods
// (agents_create_owner_test.go, same package) are NOT reused here — this needs
// GetByKey/GetByID to return a REAL fixture, which that stub deliberately does
// not do (it returns nil, nil so creation proceeds as "does not exist yet").
type updateVisibilityStore struct {
	agent   store.AgentData
	updates map[string]any // captured from the last Update call; nil if never called
}

func (s *updateVisibilityStore) GetByKey(_ context.Context, key string) (*store.AgentData, error) {
	if key != s.agent.AgentKey {
		return nil, nil
	}
	a := s.agent
	return &a, nil
}
func (s *updateVisibilityStore) GetByID(_ context.Context, id uuid.UUID) (*store.AgentData, error) {
	if id != s.agent.ID {
		return nil, nil
	}
	a := s.agent
	return &a, nil
}
func (s *updateVisibilityStore) Update(_ context.Context, _ uuid.UUID, updates map[string]any) error {
	s.updates = updates
	return nil
}

// Every other AgentStore method is unused by handleUpdate's DB-backed path for
// these tests — no context files are touched unless params.Name/Avatar are set,
// which none of these requests do.
func (s *updateVisibilityStore) GetByIDUnscoped(_ context.Context, _ uuid.UUID) (*store.AgentData, error) {
	return nil, nil
}
func (s *updateVisibilityStore) GetByKeys(_ context.Context, _ []string) ([]store.AgentData, error) {
	return nil, nil
}
func (s *updateVisibilityStore) GetByIDs(_ context.Context, _ []uuid.UUID) ([]store.AgentData, error) {
	return nil, nil
}
func (s *updateVisibilityStore) Create(_ context.Context, _ *store.AgentData) error { return nil }
func (s *updateVisibilityStore) Delete(_ context.Context, _ uuid.UUID) error        { return nil }
func (s *updateVisibilityStore) List(_ context.Context, _ string) ([]store.AgentData, error) {
	return nil, nil
}
func (s *updateVisibilityStore) GetDefault(_ context.Context) (*store.AgentData, error) {
	return nil, nil
}
func (s *updateVisibilityStore) ShareAgent(_ context.Context, _ uuid.UUID, _, _, _ string) error {
	return nil
}
func (s *updateVisibilityStore) RevokeShare(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}
func (s *updateVisibilityStore) ListShares(_ context.Context, _ uuid.UUID) ([]store.AgentShareData, error) {
	return nil, nil
}
func (s *updateVisibilityStore) CanAccess(_ context.Context, _ uuid.UUID, _ string) (bool, string, error) {
	return true, "owner", nil
}
func (s *updateVisibilityStore) ListAccessible(_ context.Context, _ string) ([]store.AgentData, error) {
	return nil, nil
}
func (s *updateVisibilityStore) GetAgentContextFiles(_ context.Context, _ uuid.UUID) ([]store.AgentContextFileData, error) {
	return nil, nil
}
func (s *updateVisibilityStore) SetAgentContextFile(_ context.Context, _ uuid.UUID, _, _ string) error {
	return nil
}
func (s *updateVisibilityStore) GetUserContextFiles(_ context.Context, _ uuid.UUID, _ string) ([]store.UserContextFileData, error) {
	return nil, nil
}
func (s *updateVisibilityStore) SetUserContextFile(_ context.Context, _ uuid.UUID, _, _, _ string) error {
	return nil
}
func (s *updateVisibilityStore) ListUserContextFilesByName(_ context.Context, _ uuid.UUID, _ string) ([]store.UserContextFileData, error) {
	return nil, nil
}
func (s *updateVisibilityStore) DeleteUserContextFile(_ context.Context, _ uuid.UUID, _, _ string) error {
	return nil
}
func (s *updateVisibilityStore) MigrateUserDataOnMerge(_ context.Context, _ []string, _ string) error {
	return nil
}
func (s *updateVisibilityStore) GetUserOverride(_ context.Context, _ uuid.UUID, _ string) (*store.UserAgentOverrideData, error) {
	return nil, nil
}
func (s *updateVisibilityStore) SetUserOverride(_ context.Context, _ *store.UserAgentOverrideData) error {
	return nil
}
func (s *updateVisibilityStore) GetOrCreateUserProfile(_ context.Context, _ uuid.UUID, _, _, _ string) (bool, string, error) {
	return false, "", nil
}
func (s *updateVisibilityStore) ListUserInstances(_ context.Context, _ uuid.UUID) ([]store.UserInstanceData, error) {
	return nil, nil
}
func (s *updateVisibilityStore) UpdateUserProfileMetadata(_ context.Context, _ uuid.UUID, _ string, _ map[string]string) error {
	return nil
}
func (s *updateVisibilityStore) EnsureUserProfile(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}
func (s *updateVisibilityStore) PropagateContextFile(_ context.Context, _ uuid.UUID, _ string) (int, error) {
	return 0, nil
}

// ---- fixtures ----

func personalAgentFixture() store.AgentData {
	return store.AgentData{
		BaseModel: store.BaseModel{ID: uuid.New()},
		AgentKey:  "my-researcher",
		OwnerID:   "user-1",
		IsLocked:  false,
	}
}

// A locked, system-owned agent is what every built-in and the canonical default
// look like. OwnerID is deliberately set to "user-1" here too — matching the
// caller in the ownership tests below — so a lock-check test proves the LOCK
// stops the change, not merely that ownership failed to match.
func lockedAgentFixture() store.AgentData {
	return store.AgentData{
		BaseModel: store.BaseModel{ID: uuid.New()},
		AgentKey:  "default",
		OwnerID:   "user-1",
		IsLocked:  true,
	}
}

func updateVisibilityRequest(t *testing.T, agentKey, visibility string) *protocol.RequestFrame {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"agentId":    agentKey,
		"visibility": visibility,
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return &protocol.RequestFrame{ID: "req-1", Method: protocol.MethodAgentsUpdate, Params: raw}
}

func newVisibilityMethods(stub *updateVisibilityStore) *AgentsMethods {
	return &AgentsMethods{
		agents:     agent.NewRouter(),
		cfg:        &config.Config{},
		agentStore: stub,
	}
}

func readErrorCode(t *testing.T, ch <-chan []byte) string {
	t.Helper()
	select {
	case raw := <-ch:
		var frame protocol.ResponseFrame
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if frame.OK {
			t.Fatalf("expected an error response, got OK")
		}
		return frame.Error.Code
	default:
		t.Fatal("handler sent no response")
		return ""
	}
}

// ---- tests ----

func TestUpdateVisibility_Owner_Allowed(t *testing.T) {
	stub := &updateVisibilityStore{agent: personalAgentFixture()}
	m := newVisibilityMethods(stub)
	client, ch := gateway.NewTestClientWithSend(permissions.RoleOperator, uuid.New(), "user-1")

	m.handleUpdate(context.Background(), client, updateVisibilityRequest(t, "my-researcher", "org"))

	if stub.updates == nil {
		t.Fatal("Update was not called — the owner's own request should have succeeded")
	}
	if got := stub.updates["visibility"]; got != "org" {
		t.Errorf("updates[visibility] = %v, want %q", got, "org")
	}
	select {
	case raw := <-ch:
		var frame protocol.ResponseFrame
		json.Unmarshal(raw, &frame)
		if !frame.OK {
			t.Errorf("expected OK response, got error: %+v", frame.Error)
		}
	default:
		t.Fatal("handler sent no response")
	}
}

func TestUpdateVisibility_NonOwner_Denied(t *testing.T) {
	stub := &updateVisibilityStore{agent: personalAgentFixture()} // owned by user-1
	m := newVisibilityMethods(stub)
	client, ch := gateway.NewTestClientWithSend(permissions.RoleAdmin, uuid.New(), "user-2")

	m.handleUpdate(context.Background(), client, updateVisibilityRequest(t, "my-researcher", "org"))

	// ADMIN role deliberately does not help here. Sharing your agent is YOUR call
	// about YOUR agent — an admin forcing it would undo the point of the privacy
	// fix that made agents private in the first place.
	if stub.updates != nil {
		t.Fatalf("Update was called for a non-owner (even an admin): %v", stub.updates)
	}
	if code := readErrorCode(t, ch); code != protocol.ErrUnauthorized {
		t.Errorf("error code = %q, want %q", code, protocol.ErrUnauthorized)
	}
}

func TestUpdateVisibility_LockedAgent_Denied(t *testing.T) {
	stub := &updateVisibilityStore{agent: lockedAgentFixture()} // owner_id matches the caller
	m := newVisibilityMethods(stub)
	client, ch := gateway.NewTestClientWithSend(permissions.RoleOperator, uuid.New(), "user-1")

	m.handleUpdate(context.Background(), client, updateVisibilityRequest(t, "default", "org"))

	if stub.updates != nil {
		t.Fatalf("Update was called for a locked agent: %v", stub.updates)
	}
	if code := readErrorCode(t, ch); code != protocol.ErrUnauthorized {
		t.Errorf("error code = %q, want %q", code, protocol.ErrUnauthorized)
	}
}

func TestUpdateVisibility_InvalidValue_Denied(t *testing.T) {
	stub := &updateVisibilityStore{agent: personalAgentFixture()}
	m := newVisibilityMethods(stub)
	client, ch := gateway.NewTestClientWithSend(permissions.RoleOperator, uuid.New(), "user-1")

	m.handleUpdate(context.Background(), client, updateVisibilityRequest(t, "my-researcher", "public"))

	if stub.updates != nil {
		t.Fatalf("Update was called with an invalid visibility value: %v", stub.updates)
	}
	if code := readErrorCode(t, ch); code != protocol.ErrInvalidRequest {
		t.Errorf("error code = %q, want %q", code, protocol.ErrInvalidRequest)
	}
}

// A request touching an unrelated field must not be blocked by the visibility
// checks, and must not silently write a visibility column nobody asked to change.
func TestUpdateVisibility_OmittedField_LeavesVisibilityAlone(t *testing.T) {
	stub := &updateVisibilityStore{agent: personalAgentFixture()}
	m := newVisibilityMethods(stub)
	client, _ := gateway.NewTestClientWithSend(permissions.RoleOperator, uuid.New(), "someone-else-entirely")

	raw, _ := json.Marshal(map[string]any{"agentId": "my-researcher", "name": "Renamed"})
	m.handleUpdate(context.Background(), client, &protocol.RequestFrame{ID: "r", Method: protocol.MethodAgentsUpdate, Params: raw})

	if stub.updates == nil {
		t.Fatal("Update was not called for an ordinary rename")
	}
	if _, present := stub.updates["visibility"]; present {
		t.Error("updates map contains a visibility key nobody asked to set")
	}
	if stub.updates["display_name"] != "Renamed" {
		t.Errorf("display_name = %v, want %q", stub.updates["display_name"], "Renamed")
	}
}
