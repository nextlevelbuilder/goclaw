package gateway

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

type capturedLogRecord struct {
	message string
	attrs   map[string]any
}

type captureLogHandler struct {
	mu      sync.Mutex
	records []capturedLogRecord
}

func (h *captureLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureLogHandler) Handle(_ context.Context, record slog.Record) error {
	attrs := make(map[string]any)
	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value.Any()
		return true
	})
	h.mu.Lock()
	h.records = append(h.records, capturedLogRecord{message: record.Message, attrs: attrs})
	h.mu.Unlock()
	return nil
}

func (h *captureLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureLogHandler) WithGroup(string) slog.Handler      { return h }

func (h *captureLogHandler) find(message string) (capturedLogRecord, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, record := range h.records {
		if record.message == message {
			return record, true
		}
	}
	return capturedLogRecord{}, false
}

type snapshotTeamAccessStore struct {
	mu       sync.Mutex
	byTenant map[uuid.UUID]map[string][]uuid.UUID
	err      error
	calls    int
}

func (s *snapshotTeamAccessStore) ListUserTeamIDs(ctx context.Context, userID string) ([]uuid.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	ids := s.byTenant[store.TenantIDFromContext(ctx)][userID]
	return append([]uuid.UUID(nil), ids...), nil
}

func newSnapshotRouter(t *testing.T, access store.UserTeamIDLister) (*Server, *MethodRouter) {
	t.Helper()
	server := NewServer(config.Default(), nil, nil, nil)
	server.router.SetTeamAccessStore(access)
	return server, server.router
}

func TestConnectPopulatesTenantBoundTeamAccessSnapshot(t *testing.T) {
	t.Setenv(config.GatewayAllowInsecureNoAuthEnv, "1")
	cfg := config.Default()
	cfg.Gateway.Host = "127.0.0.1"
	cfg.Gateway.Token = ""
	tenantID, accessibleTeam := uuid.New(), uuid.New()
	access := &snapshotTeamAccessStore{byTenant: map[uuid.UUID]map[string][]uuid.UUID{
		store.MasterTenantID: {"user-a": {accessibleTeam}},
	}}
	server := NewServer(cfg, nil, nil, nil)
	server.router.SetTeamAccessStore(access)
	client := NewClient(nil, server, "127.0.0.1")

	server.router.Handle(context.Background(), client, &protocol.RequestFrame{
		ID: "connect", Method: protocol.MethodConnect, Params: []byte(`{"user_id":"user-a"}`),
	})

	if !client.authenticated {
		t.Fatal("connect did not authenticate")
	}
	if access.calls != 1 {
		t.Fatalf("ListUserTeamIDs calls = %d, want 1", access.calls)
	}
	if !client.hasTeamAccess(accessibleTeam.String()) {
		t.Fatal("connect did not publish the loaded team access snapshot")
	}
	if clientCanReceiveEvent(client, bus.Event{
		Name: protocol.EventTeamTaskCreated, TenantID: store.MasterTenantID,
		Payload: map[string]any{"team_id": accessibleTeam.String()},
	}) == false {
		t.Fatal("client did not receive team event allowed by connect snapshot")
	}
	if clientCanReceiveEvent(client, bus.Event{
		Name: protocol.EventTeamTaskCreated, TenantID: tenantID,
		Payload: map[string]any{"team_id": accessibleTeam.String()},
	}) {
		t.Fatal("client received same team event from another tenant")
	}
}

func TestConnectFailsClosedWithoutTeamAccessStore(t *testing.T) {
	t.Setenv(config.GatewayAllowInsecureNoAuthEnv, "1")
	cfg := config.Default()
	cfg.Gateway.Host = "127.0.0.1"
	cfg.Gateway.Token = ""
	server := NewServer(cfg, nil, nil, nil)
	client := NewClient(nil, server, "127.0.0.1")

	server.router.Handle(context.Background(), client, &protocol.RequestFrame{
		ID: "connect", Method: protocol.MethodConnect, Params: []byte(`{"user_id":"user-a"}`),
	})

	if client.authenticated {
		t.Fatal("connect remained authenticated without a team access store")
	}
}

func TestConnectFailsClosedWhenTeamAccessSnapshotLoadFails(t *testing.T) {
	t.Setenv(config.GatewayAllowInsecureNoAuthEnv, "1")
	logHandler := &captureLogHandler{}
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(logHandler))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	cfg := config.Default()
	cfg.Gateway.Host = "127.0.0.1"
	cfg.Gateway.Token = ""
	access := &snapshotTeamAccessStore{err: errors.New("team grants unavailable")}
	server := NewServer(cfg, nil, nil, nil)
	server.router.SetTeamAccessStore(access)
	client := NewClient(nil, server, "127.0.0.1")

	server.router.Handle(context.Background(), client, &protocol.RequestFrame{
		ID: "connect", Method: protocol.MethodConnect, Params: []byte(`{"user_id":"user-a"}`),
	})

	if client.authenticated {
		t.Fatal("connect remained authenticated after team snapshot load failed")
	}
	if client.UserID() != "" || client.TenantID() != uuid.Nil {
		t.Fatalf("failed connect identity was not scrubbed: user=%q tenant=%s", client.UserID(), client.TenantID())
	}
	logRecord, ok := logHandler.find("security.team_access_snapshot_load_failed")
	if !ok {
		t.Fatal("missing team access snapshot failure security log")
	}
	if got := logRecord.attrs["tenant_id"]; got != store.MasterTenantID {
		t.Fatalf("security log tenant_id = %#v, want %s", got, store.MasterTenantID)
	}
	if got := logRecord.attrs["user_id"]; got != "user-a" {
		t.Fatalf("security log user_id = %#v, want user-a", got)
	}
	if client.hasTeamAccess(uuid.NewString()) {
		t.Fatal("failed snapshot load retained team access")
	}
	response := <-client.send
	if len(response) == 0 {
		t.Fatal("expected failed connect response")
	}
}

func TestTeamAccessInvalidationRefreshesOnlyAffectedTenant(t *testing.T) {
	tenantA, tenantB := uuid.New(), uuid.New()
	oldTeam, newTeam, otherTenantTeam := uuid.New(), uuid.New(), uuid.New()
	access := &snapshotTeamAccessStore{byTenant: map[uuid.UUID]map[string][]uuid.UUID{
		tenantA: {"same-user": {newTeam}},
		tenantB: {"same-user": {otherTenantTeam}},
	}}
	server, router := newSnapshotRouter(t, access)
	clientA := makeClient(permissions.RoleOperator, "same-user", tenantA)
	clientA.id = "tenant-a"
	clientA.SetTeamAccess([]string{oldTeam.String()})
	clientB := makeClient(permissions.RoleOperator, "same-user", tenantB)
	clientB.id = "tenant-b"
	clientB.SetTeamAccess([]string{otherTenantTeam.String()})
	server.clients[clientA.id] = clientA
	server.clients[clientB.id] = clientB

	router.HandleTeamAccessInvalidation(context.Background(), tenantA)

	if clientA.hasTeamAccess(oldTeam.String()) || !clientA.hasTeamAccess(newTeam.String()) {
		t.Fatal("affected tenant did not receive replacement snapshot")
	}
	if !clientB.hasTeamAccess(otherTenantTeam.String()) {
		t.Fatal("invalidation changed another tenant's snapshot")
	}
	if access.calls != 1 {
		t.Fatalf("ListUserTeamIDs calls = %d, want 1 for affected tenant", access.calls)
	}
}

func TestTeamAccessInvalidationFailureClearsSnapshot(t *testing.T) {
	tenantID, teamID := uuid.New(), uuid.New()
	access := &snapshotTeamAccessStore{err: errors.New("team grants unavailable")}
	server, router := newSnapshotRouter(t, access)
	client := makeClient(permissions.RoleOperator, "user", tenantID)
	client.id = "client"
	client.SetTeamAccess([]string{teamID.String()})
	server.clients[client.id] = client

	router.HandleTeamAccessInvalidation(context.Background(), tenantID)

	if client.hasTeamAccess(teamID.String()) {
		t.Fatal("failed refresh retained stale team access")
	}
}

func TestTeamAccessSnapshotConcurrentRefreshAndRead(t *testing.T) {
	client := makeClient(permissions.RoleOperator, "user", uuid.New())
	teamIDs := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	const iterations = 1_000

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for n := 0; n < iterations; n++ {
				client.SetTeamAccess([]string{teamIDs[(n+offset)%len(teamIDs)]})
			}
		}(i)
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for n := 0; n < iterations; n++ {
				_ = client.hasTeamAccess(teamIDs[(n+offset)%len(teamIDs)])
			}
		}(i)
	}
	wg.Wait()
}
