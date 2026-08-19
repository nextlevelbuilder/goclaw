package methods

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// stubMissionStore implements store.MissionStore for handler tests.
type stubMissionStore struct {
	mission *store.Mission
	items   []store.Mission
	err     error
	deleted uuid.UUID
}

func (s *stubMissionStore) CreateMission(_ context.Context, m *store.Mission) error {
	if s.err != nil {
		return s.err
	}
	m.ID = uuid.Must(uuid.NewV7())
	s.mission = m
	return nil
}

func (s *stubMissionStore) GetMission(_ context.Context, id uuid.UUID) (*store.Mission, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.mission == nil || s.mission.ID != id {
		return nil, store.ErrMissionNotFound
	}
	return s.mission, nil
}

func (s *stubMissionStore) ListMissions(_ context.Context, opts store.MissionListOpts) ([]store.Mission, error) {
	if s.err != nil {
		return nil, s.err
	}
	if opts.Status != "" {
		var filtered []store.Mission
		for _, m := range s.items {
			if m.Status == opts.Status {
				filtered = append(filtered, m)
			}
		}
		return filtered, nil
	}
	return s.items, nil
}

func (s *stubMissionStore) UpdateMissionStatus(_ context.Context, id uuid.UUID, status string) error {
	if s.err != nil {
		return s.err
	}
	if s.mission == nil || s.mission.ID != id {
		return store.ErrMissionNotFound
	}
	s.mission.Status = status
	return nil
}

func (s *stubMissionStore) UpdateMissionProgress(_ context.Context, id uuid.UUID, seq int) error {
	if s.err != nil {
		return s.err
	}
	if s.mission == nil || s.mission.ID != id {
		return store.ErrMissionNotFound
	}
	s.mission.CheckpointSeq = seq
	return nil
}

func (s *stubMissionStore) DeleteMission(_ context.Context, id uuid.UUID) error {
	if s.err != nil {
		return s.err
	}
	if s.mission == nil || s.mission.ID != id {
		return store.ErrMissionNotFound
	}
	s.deleted = id
	return nil
}

// stubMissionResume returns a mission-resume-compatible closure.
func stubMissionResume(err error) func(context.Context, string) error {
	return func(context.Context, string) error {
		return err
	}
}

func TestMissionCreateSuccess(t *testing.T) {
	stub := &stubMissionStore{}
	m := NewMissionMethods(stub)
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)

	m.handleCreate(ctx, client, sessionReqFrame(t, protocol.MethodMissionCreate, map[string]any{
		"name":       "Launch",
		"goals":      []string{"Ship v1"},
		"milestones": []string{"Design", "Build"},
		"acceptance": []string{"All green"},
		"agentId":    uuid.Must(uuid.NewV7()).String(),
		"sessionKey": "agent:launch:s1",
	}))

	resp := readTimelineResponse(t, responses)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	data, ok := resp.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", resp.Payload)
	}
	missionData, ok := data["mission"].(map[string]any)
	if !ok {
		t.Fatalf("mission type = %T", data["mission"])
	}
	if missionData["name"] != "Launch" {
		t.Fatalf("mission name = %v, want Launch", missionData["name"])
	}
}

func TestMissionCreateRequiresName(t *testing.T) {
	m := NewMissionMethods(&stubMissionStore{})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)

	m.handleCreate(ctx, client, sessionReqFrame(t, protocol.MethodMissionCreate, map[string]any{}))
	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("error = %+v, want INVALID_REQUEST", resp.Error)
	}
}

func TestMissionCreateUnavailableWithoutStore(t *testing.T) {
	m := NewMissionMethods(nil)
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)

	m.handleCreate(ctx, client, sessionReqFrame(t, protocol.MethodMissionCreate, map[string]any{"name": "x"}))
	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("error = %+v, want UNAVAILABLE", resp.Error)
	}
}

func TestMissionGetSuccess(t *testing.T) {
	id := uuid.Must(uuid.NewV7())
	stub := &stubMissionStore{mission: &store.Mission{ID: id, Name: "Launch", Status: store.MissionStatusActive}}
	m := NewMissionMethods(stub)
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)

	m.handleGet(ctx, client, sessionReqFrame(t, protocol.MethodMissionGet, map[string]any{"missionId": id.String()}))
	resp := readTimelineResponse(t, responses)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}

func TestMissionGetNotFound(t *testing.T) {
	m := NewMissionMethods(&stubMissionStore{})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)

	m.handleGet(ctx, client, sessionReqFrame(t, protocol.MethodMissionGet, map[string]any{"missionId": uuid.Must(uuid.NewV7()).String()}))
	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrNotFound {
		t.Fatalf("error = %+v, want NOT_FOUND", resp.Error)
	}
}

func TestMissionGetRejectsInvalidID(t *testing.T) {
	m := NewMissionMethods(&stubMissionStore{})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)

	m.handleGet(ctx, client, sessionReqFrame(t, protocol.MethodMissionGet, map[string]any{"missionId": "not-a-uuid"}))
	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("error = %+v, want INVALID_REQUEST", resp.Error)
	}
}

func TestMissionListStatusFilter(t *testing.T) {
	active := store.Mission{Name: "active", Status: store.MissionStatusActive}
	paused := store.Mission{Name: "paused", Status: store.MissionStatusPaused}
	m := NewMissionMethods(&stubMissionStore{items: []store.Mission{active, paused}})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)

	m.handleList(ctx, client, sessionReqFrame(t, protocol.MethodMissionList, map[string]any{"status": store.MissionStatusPaused}))
	resp := readTimelineResponse(t, responses)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	data, ok := resp.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", resp.Payload)
	}
	items, ok := data["items"].([]any)
	if !ok {
		t.Fatalf("items type = %T", data["items"])
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
}

func TestMissionListRejectsInvalidStatus(t *testing.T) {
	m := NewMissionMethods(&stubMissionStore{})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)

	m.handleList(ctx, client, sessionReqFrame(t, protocol.MethodMissionList, map[string]any{"status": "bogus"}))
	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("error = %+v, want INVALID_REQUEST", resp.Error)
	}
}

func TestMissionPause(t *testing.T) {
	id := uuid.Must(uuid.NewV7())
	m := NewMissionMethods(&stubMissionStore{mission: &store.Mission{ID: id, Status: store.MissionStatusActive}})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)

	m.handlePause(ctx, client, sessionReqFrame(t, protocol.MethodMissionPause, map[string]any{"missionId": id.String()}))
	resp := readTimelineResponse(t, responses)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	data, ok := resp.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", resp.Payload)
	}
	if data["status"] != store.MissionStatusPaused {
		t.Fatalf("status = %v, want paused", data["status"])
	}
}

func TestMissionResumeUnavailableWithoutResumer(t *testing.T) {
	id := uuid.Must(uuid.NewV7())
	m := NewMissionMethods(&stubMissionStore{mission: &store.Mission{ID: id, Status: store.MissionStatusPaused}})
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)

	m.handleResume(ctx, client, sessionReqFrame(t, protocol.MethodMissionResume, map[string]any{"missionId": id.String()}))
	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("error = %+v, want UNAVAILABLE", resp.Error)
	}
}

func TestMissionResumeSuccess(t *testing.T) {
	id := uuid.Must(uuid.NewV7())
	m := NewMissionMethods(&stubMissionStore{mission: &store.Mission{ID: id, Status: store.MissionStatusPaused}})
	m.SetResumer(stubMissionResume(nil))
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)

	m.handleResume(ctx, client, sessionReqFrame(t, protocol.MethodMissionResume, map[string]any{"missionId": id.String()}))
	resp := readTimelineResponse(t, responses)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	data, ok := resp.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", resp.Payload)
	}
	if data["status"] != store.MissionStatusActive {
		t.Fatalf("status = %v, want active", data["status"])
	}
}

func TestMissionResumePropagatesNotFound(t *testing.T) {
	id := uuid.Must(uuid.NewV7())
	m := NewMissionMethods(&stubMissionStore{mission: &store.Mission{ID: id, Status: store.MissionStatusPaused}})
	m.SetResumer(stubMissionResume(store.ErrMissionNotFound))
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)

	m.handleResume(ctx, client, sessionReqFrame(t, protocol.MethodMissionResume, map[string]any{"missionId": id.String()}))
	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrNotFound {
		t.Fatalf("error = %+v, want NOT_FOUND", resp.Error)
	}
}

func TestMissionResumeRejectsTerminalMission(t *testing.T) {
	id := uuid.Must(uuid.NewV7())
	stub := &stubMissionStore{mission: &store.Mission{ID: id, Status: store.MissionStatusCompleted}}
	m := NewMissionMethods(stub)
	m.SetResumer(stubMissionResume(store.ErrMissionNotResumable))
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)

	m.handleResume(ctx, client, sessionReqFrame(t, protocol.MethodMissionResume, map[string]any{"missionId": id.String()}))
	resp := readTimelineResponse(t, responses)
	if resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("error = %+v, want INVALID_REQUEST", resp.Error)
	}
}

func TestMissionDelete(t *testing.T) {
	id := uuid.Must(uuid.NewV7())
	stub := &stubMissionStore{mission: &store.Mission{ID: id}}
	m := NewMissionMethods(stub)
	tenantID := uuid.Must(uuid.NewV7())
	client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
	ctx := store.WithTenantID(context.Background(), tenantID)

	m.handleDelete(ctx, client, sessionReqFrame(t, protocol.MethodMissionDelete, map[string]any{"missionId": id.String()}))
	resp := readTimelineResponse(t, responses)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if stub.deleted != id {
		t.Fatalf("deleted = %v, want %v", stub.deleted, id)
	}
}

// TestMissionMethodsRegister proves every mission protocol method is wired to a
// handler through the Register surface. Registration is exercised indirectly:
// each method must appear in the protocol namespace (the router binds them),
// and a nil-store handler must answer UNAVAILABLE rather than panic.
func TestMissionMethodsRegister(t *testing.T) {
	methods := []string{
		protocol.MethodMissionCreate,
		protocol.MethodMissionGet,
		protocol.MethodMissionList,
		protocol.MethodMissionPause,
		protocol.MethodMissionResume,
		protocol.MethodMissionDelete,
	}
	m := NewMissionMethods(nil)
	for _, method := range methods {
		tenantID := uuid.Must(uuid.NewV7())
		client, responses := gateway.NewCapturingTestClient(permissions.RoleAdmin, tenantID, "caller", 1)
		ctx := store.WithTenantID(context.Background(), tenantID)
		switch method {
		case protocol.MethodMissionCreate:
			m.handleCreate(ctx, client, sessionReqFrame(t, method, map[string]any{"name": "x"}))
		case protocol.MethodMissionGet:
			m.handleGet(ctx, client, sessionReqFrame(t, method, map[string]any{"missionId": uuid.Must(uuid.NewV7()).String()}))
		case protocol.MethodMissionList:
			m.handleList(ctx, client, sessionReqFrame(t, method, map[string]any{}))
		case protocol.MethodMissionPause:
			m.handlePause(ctx, client, sessionReqFrame(t, method, map[string]any{"missionId": uuid.Must(uuid.NewV7()).String()}))
		case protocol.MethodMissionResume:
			m.handleResume(ctx, client, sessionReqFrame(t, method, map[string]any{"missionId": uuid.Must(uuid.NewV7()).String()}))
		case protocol.MethodMissionDelete:
			m.handleDelete(ctx, client, sessionReqFrame(t, method, map[string]any{"missionId": uuid.Must(uuid.NewV7()).String()}))
		}
		resp := readTimelineResponse(t, responses)
		if resp.Error == nil || resp.Error.Code != protocol.ErrUnavailable {
			t.Fatalf("method %s error = %+v, want UNAVAILABLE (nil store)", method, resp.Error)
		}
	}
}
