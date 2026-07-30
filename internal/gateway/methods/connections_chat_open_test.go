package methods

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/clisession"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
	"github.com/nextlevelbuilder/goclaw/internal/sessions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Stubs
// ---------------------------------------------------------------------------

// stubCLIConnStore is an in-memory CLIConnectionStore that enforces the same
// visibility rule the real ones do: a row is visible to its own tenant, or to
// everyone when it is global (TenantID nil). Getting that wrong in the stub would
// make the cross-tenant test prove nothing.
type stubCLIConnStore struct {
	mu    sync.Mutex
	rows  map[uuid.UUID]store.CLIConnection
	creds map[uuid.UUID]store.CLIConnectionCredential
}

func newStubCLIConnStore(rows ...store.CLIConnection) *stubCLIConnStore {
	s := &stubCLIConnStore{rows: map[uuid.UUID]store.CLIConnection{}, creds: map[uuid.UUID]store.CLIConnectionCredential{}}
	for _, r := range rows {
		s.rows[r.ID] = r
	}
	return s
}

func (s *stubCLIConnStore) visible(tenantID *uuid.UUID, row store.CLIConnection) bool {
	if row.TenantID == nil {
		return true
	}
	return tenantID != nil && *tenantID == *row.TenantID
}

func (s *stubCLIConnStore) ListForTenant(_ context.Context, tenantID *uuid.UUID, enabledOnly bool) ([]store.CLIConnection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.CLIConnection
	for _, row := range s.rows {
		if !s.visible(tenantID, row) || (enabledOnly && !row.Enabled) {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func (s *stubCLIConnStore) GetByID(_ context.Context, tenantID *uuid.UUID, id uuid.UUID) (*store.CLIConnection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[id]
	if !ok || !s.visible(tenantID, row) {
		return nil, nil // invisible is indistinguishable from absent
	}
	copied := row
	return &copied, nil
}

func (s *stubCLIConnStore) Upsert(_ context.Context, conn *store.CLIConnection) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if conn.ID == uuid.Nil {
		conn.ID = uuid.New()
	}
	s.rows[conn.ID] = *conn
	return nil
}

func (s *stubCLIConnStore) Delete(_ context.Context, _ *uuid.UUID, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, id)
	return nil
}

func (s *stubCLIConnStore) PutCredential(_ context.Context, cred store.CLIConnectionCredential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creds[cred.ConnectionID] = cred
	return nil
}

func (s *stubCLIConnStore) GetCredential(_ context.Context, connectionID uuid.UUID, _ string) (*store.CLIConnectionCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cred, ok := s.creds[connectionID]; ok {
		copied := cred
		return &copied, nil
	}
	return nil, nil
}

func (s *stubCLIConnStore) DeleteCredential(_ context.Context, connectionID uuid.UUID, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.creds, connectionID)
	return nil
}

// stubSandboxManager satisfies sandbox.Manager without a Docker daemon. Get is
// never reached by these tests (connections.chat.open starts nothing), so it
// reports a clear failure rather than pretending to hand back a container.
type stubSandboxManager struct{}

func (stubSandboxManager) Get(context.Context, string, string, *sandbox.Config) (sandbox.Sandbox, error) {
	return nil, errors.New("stub sandbox manager: no container in tests")
}
func (stubSandboxManager) BaseConfig() sandbox.Config            { return sandbox.Config{Image: "stub"} }
func (stubSandboxManager) Release(context.Context, string) error { return nil }
func (stubSandboxManager) ReleaseAll(context.Context) error      { return nil }
func (stubSandboxManager) Stop()                                 {}
func (stubSandboxManager) Stats() map[string]any                 { return map[string]any{} }

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

// openHarness wires ConnectionsMethods over a stub store with the CLI-chat layer
// present, so the availability check is not what these tests trip on.
func openHarness(t *testing.T, rows ...store.CLIConnection) *ConnectionsMethods {
	t.Helper()
	chat := NewCLIChat(CLIChatDeps{
		Manager:     clisession.NewManager(clisession.ManagerOpts{}),
		Connections: newStubCLIConnStore(rows...),
		Sandbox:     stubSandboxManager{},
	})
	t.Cleanup(chat.Shutdown)
	m := NewConnectionsMethods(chat.deps.Connections, nil)
	m.SetCLIChat(chat)
	return m
}

func openRequest(t *testing.T, connectionID string) *protocol.RequestFrame {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"connectionId": connectionID})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return &protocol.RequestFrame{Type: protocol.FrameTypeRequest, ID: "open-1", Method: protocol.MethodConnectionsChatOpen, Params: raw}
}

// readFrame returns the single response the handler sent.
func readFrame(t *testing.T, ch <-chan []byte) protocol.ResponseFrame {
	t.Helper()
	select {
	case raw := <-ch:
		var frame protocol.ResponseFrame
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		return frame
	default:
		t.Fatal("handler sent no response")
		return protocol.ResponseFrame{}
	}
}

// claudeCodeConn is an enabled connection whose provider CAN hold a conversation.
func claudeCodeConn(tenantID *uuid.UUID) store.CLIConnection {
	return store.CLIConnection{
		ID: uuid.New(), TenantID: tenantID, Name: "Claude Code",
		Kind: "external_cli", Provider: "claude_code", Mode: "delegate", Enabled: true,
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestChatOpenSucceeds(t *testing.T) {
	tenantID := uuid.New()
	conn := claudeCodeConn(&tenantID)
	m := openHarness(t, conn)

	client, out := gateway.NewTestClientWithSend(permissions.RoleOperator, tenantID, "user-1")
	m.handleChatOpen(wsCallCtx(client), client, openRequest(t, conn.ID.String()))

	frame := readFrame(t, out)
	if !frame.OK {
		t.Fatalf("chat.open failed: %+v", frame.Error)
	}
	payload, ok := frame.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload = %T, want an object", frame.Payload)
	}
	key, _ := payload["sessionKey"].(string)
	gotConn, _, parsed := sessions.ParseCLISessionKey(key)
	if !parsed {
		t.Fatalf("sessionKey %q is not a usable CLI session key", key)
	}
	if gotConn != conn.ID.String() {
		t.Errorf("sessionKey names connection %q, want %q", gotConn, conn.ID)
	}
	// The returned key must route to the CLI handler, not the agent path.
	if routeForSessionKey(key) != routeCLI {
		t.Errorf("sessionKey %q would be routed to the agent path", key)
	}
	if got, _ := payload["permissionMode"].(string); got != "auto" {
		t.Errorf("permissionMode = %q, want auto (no config = today's behaviour)", got)
	}
}

// TestChatOpenTwiceGivesDistinctConversations: two conversations with the same
// connection must not collide, or the second would join the first's CLI process.
func TestChatOpenTwiceGivesDistinctConversations(t *testing.T) {
	tenantID := uuid.New()
	conn := claudeCodeConn(&tenantID)
	m := openHarness(t, conn)

	keys := map[string]bool{}
	for i := 0; i < 2; i++ {
		client, out := gateway.NewTestClientWithSend(permissions.RoleOperator, tenantID, "user-1")
		m.handleChatOpen(wsCallCtx(client), client, openRequest(t, conn.ID.String()))
		frame := readFrame(t, out)
		if !frame.OK {
			t.Fatalf("chat.open failed: %+v", frame.Error)
		}
		payload := frame.Payload.(map[string]any)
		keys[payload["sessionKey"].(string)] = true
	}
	if len(keys) != 2 {
		t.Fatalf("two opens produced %d distinct session keys, want 2", len(keys))
	}
}

func TestChatOpenRejectsUnknownConnection(t *testing.T) {
	tenantID := uuid.New()
	m := openHarness(t, claudeCodeConn(&tenantID))

	client, out := gateway.NewTestClientWithSend(permissions.RoleOperator, tenantID, "user-1")
	m.handleChatOpen(wsCallCtx(client), client, openRequest(t, uuid.NewString()))

	frame := readFrame(t, out)
	if frame.OK {
		t.Fatal("chat.open succeeded for an unknown connection")
	}
	if frame.Error.Code != protocol.ErrNotFound {
		t.Errorf("error code = %q, want %q", frame.Error.Code, protocol.ErrNotFound)
	}
}

func TestChatOpenRejectsMalformedID(t *testing.T) {
	tenantID := uuid.New()
	m := openHarness(t, claudeCodeConn(&tenantID))

	client, out := gateway.NewTestClientWithSend(permissions.RoleOperator, tenantID, "user-1")
	m.handleChatOpen(wsCallCtx(client), client, openRequest(t, "not-a-uuid"))

	frame := readFrame(t, out)
	if frame.OK {
		t.Fatal("chat.open succeeded for a malformed connection id")
	}
	if frame.Error.Code != protocol.ErrInvalidRequest {
		t.Errorf("error code = %q, want %q", frame.Error.Code, protocol.ErrInvalidRequest)
	}
}

// TestChatOpenRejectsAnotherTenantsConnection is the isolation case: the row
// exists, but not for this caller — and the answer must not confirm it exists.
func TestChatOpenRejectsAnotherTenantsConnection(t *testing.T) {
	ownerTenant := uuid.New()
	callerTenant := uuid.New()
	conn := claudeCodeConn(&ownerTenant)
	m := openHarness(t, conn)

	client, out := gateway.NewTestClientWithSend(permissions.RoleAdmin, callerTenant, "user-2")
	m.handleChatOpen(wsCallCtx(client), client, openRequest(t, conn.ID.String()))

	frame := readFrame(t, out)
	if frame.OK {
		t.Fatal("chat.open succeeded for ANOTHER TENANT's connection")
	}
	if frame.Error.Code != protocol.ErrNotFound {
		t.Errorf("error code = %q, want %q (existence must not leak across tenants)", frame.Error.Code, protocol.ErrNotFound)
	}
	if strings.Contains(strings.ToLower(frame.Error.Message), "disabled") {
		t.Errorf("error message %q leaks the row's state", frame.Error.Message)
	}
}

func TestChatOpenRejectsDisabledConnection(t *testing.T) {
	tenantID := uuid.New()
	conn := claudeCodeConn(&tenantID)
	conn.Enabled = false
	m := openHarness(t, conn)

	client, out := gateway.NewTestClientWithSend(permissions.RoleOperator, tenantID, "user-1")
	m.handleChatOpen(wsCallCtx(client), client, openRequest(t, conn.ID.String()))

	frame := readFrame(t, out)
	if frame.OK {
		t.Fatal("chat.open succeeded for a DISABLED connection")
	}
	if !strings.Contains(strings.ToLower(frame.Error.Message), "disabled") {
		t.Errorf("error message = %q, want it to say the connection is disabled", frame.Error.Message)
	}
}

// TestChatOpenRejectsProviderWithoutInteractive is the honesty requirement: a
// provider with no stdin protocol must be refused BY NAME, not handed a session
// that would swallow every message.
func TestChatOpenRejectsProviderWithoutInteractive(t *testing.T) {
	tenantID := uuid.New()
	for _, provider := range []string{"aider", "codex", "gemini_cli"} {
		conn := claudeCodeConn(&tenantID)
		conn.Provider = provider
		conn.Name = provider + " connection"
		m := openHarness(t, conn)

		client, out := gateway.NewTestClientWithSend(permissions.RoleOperator, tenantID, "user-1")
		m.handleChatOpen(wsCallCtx(client), client, openRequest(t, conn.ID.String()))

		frame := readFrame(t, out)
		if frame.OK {
			t.Fatalf("%s: chat.open succeeded for a provider with no interactive mode", provider)
		}
		if !strings.Contains(frame.Error.Message, provider) {
			t.Errorf("%s: error message = %q, want it to name the provider", provider, frame.Error.Message)
		}
		if !strings.Contains(strings.ToLower(frame.Error.Message), "interactive") {
			t.Errorf("%s: error message = %q, want it to explain the missing interactive mode", provider, frame.Error.Message)
		}
	}
}

// TestChatOpenReportsUnavailableDeployment: with no CLI-chat layer wired the
// method must say so instead of minting a key chat.send would refuse.
func TestChatOpenReportsUnavailableDeployment(t *testing.T) {
	tenantID := uuid.New()
	conn := claudeCodeConn(&tenantID)
	m := NewConnectionsMethods(newStubCLIConnStore(conn), nil) // no SetCLIChat

	client, out := gateway.NewTestClientWithSend(permissions.RoleOperator, tenantID, "user-1")
	m.handleChatOpen(wsCallCtx(client), client, openRequest(t, conn.ID.String()))

	frame := readFrame(t, out)
	if frame.OK {
		t.Fatal("chat.open succeeded with no CLI chat layer wired")
	}
	if !strings.Contains(strings.ToLower(frame.Error.Message), "not available") {
		t.Errorf("error message = %q, want it to say the feature is unavailable", frame.Error.Message)
	}
}

// TestChatOpenRequiresOperator pins the permission gate next to its siblings:
// opening a conversation can start a process on the user's credential, so a
// read-only viewer must not be able to.
func TestChatOpenRequiresOperator(t *testing.T) {
	if got := permissions.MethodRole(protocol.MethodConnectionsChatOpen); got != permissions.RoleOperator {
		t.Errorf("MethodRole(%q) = %q, want %q", protocol.MethodConnectionsChatOpen, got, permissions.RoleOperator)
	}
	// The sibling read must stay viewer-readable — a bare "connections." prefix
	// in the write list would have broken that.
	if got := permissions.MethodRole(protocol.MethodConnectionsList); got != permissions.RoleViewer {
		t.Errorf("MethodRole(%q) = %q, want %q", protocol.MethodConnectionsList, got, permissions.RoleViewer)
	}
	pe := permissions.NewPolicyEngine(nil)
	if pe.CanAccess(permissions.RoleViewer, protocol.MethodConnectionsChatOpen) {
		t.Error("a viewer can open a CLI conversation")
	}
	if !pe.CanAccess(permissions.RoleOperator, protocol.MethodConnectionsChatOpen) {
		t.Error("an operator cannot open a CLI conversation")
	}
}
