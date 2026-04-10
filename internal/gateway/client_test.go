package gateway

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func TestClient_IsOwner(t *testing.T) {
	owner := &Client{role: permissions.RoleOwner}
	if !owner.IsOwner() {
		t.Error("owner should return true")
	}
	admin := &Client{role: permissions.RoleAdmin}
	if admin.IsOwner() {
		t.Error("admin should not be owner")
	}
}

func TestClient_HasScope(t *testing.T) {
	c := &Client{scopes: []permissions.Scope{permissions.ScopeRead, permissions.ScopeWrite}}
	if !c.HasScope(permissions.ScopeRead) {
		t.Error("should have read scope")
	}
	if !c.HasScope(permissions.ScopeWrite) {
		t.Error("should have write scope")
	}
	if c.HasScope(permissions.ScopeAdmin) {
		t.Error("should NOT have admin scope")
	}
}

func TestClient_HasScope_Empty(t *testing.T) {
	c := &Client{}
	if c.HasScope(permissions.ScopeRead) {
		t.Error("empty scopes should have no scope")
	}
}

func TestClient_HasTeamAccess_Admin(t *testing.T) {
	c := &Client{role: permissions.RoleAdmin}
	if !c.hasTeamAccess("any-team") {
		t.Error("admin should have access to any team")
	}
}

func TestClient_HasTeamAccess_OwnerImpliesAccess(t *testing.T) {
	c := &Client{role: permissions.RoleOwner}
	if !c.hasTeamAccess("any-team") {
		t.Error("owner should have access to any team (higher than admin)")
	}
}

func TestClient_HasTeamAccess_OperatorWithTeams(t *testing.T) {
	c := &Client{role: permissions.RoleOperator}
	c.SetTeamAccess([]string{"team-1", "team-2"})
	if !c.hasTeamAccess("team-1") {
		t.Error("should have access to team-1")
	}
	if c.hasTeamAccess("team-3") {
		t.Error("should NOT have access to team-3")
	}
}

func TestClient_HasTeamAccess_OperatorNoTeams(t *testing.T) {
	c := &Client{role: permissions.RoleOperator}
	if c.hasTeamAccess("any-team") {
		t.Error("operator with no team access should be denied")
	}
}

func TestClient_SetTeamAccess_VerifiedViaBehavior(t *testing.T) {
	c := &Client{role: permissions.RoleOperator}
	c.SetTeamAccess([]string{"a", "b", "c"})
	for _, id := range []string{"a", "b", "c"} {
		if !c.hasTeamAccess(id) {
			t.Errorf("should have access to team %q after SetTeamAccess", id)
		}
	}
	if c.hasTeamAccess("d") {
		t.Error("should NOT have access to team not in SetTeamAccess list")
	}
}

func TestClient_SendResponse_BufferFull(t *testing.T) {
	// Client with a tiny buffer — should not panic when full
	c := &Client{id: "test", send: make(chan []byte, 1)}
	resp := protocol.NewOKResponse("1", map[string]any{"ok": true})
	c.SendResponse(resp) // fills buffer
	c.SendResponse(resp) // would block, should be dropped silently
	// Verify at least one message in buffer
	select {
	case msg := <-c.send:
		var frame protocol.ResponseFrame
		if err := json.Unmarshal(msg, &frame); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if frame.ID != "1" {
			t.Errorf("frame.ID = %q, want %q", frame.ID, "1")
		}
	default:
		t.Fatal("expected at least one message in buffer")
	}
}

func TestClient_SendEvent_BufferFull(t *testing.T) {
	c := &Client{id: "test", send: make(chan []byte, 1)}
	evt := *protocol.NewEvent("test", map[string]any{"ok": true})
	c.SendEvent(evt) // fills buffer
	c.SendEvent(evt) // should be dropped silently, no panic
}

func TestClient_SendResponse_NilChannel(t *testing.T) {
	// Client with nil send channel — should recover from panic
	c := &Client{id: "nil-chan"}
	resp := protocol.NewOKResponse("1", nil)
	// Should not panic
	c.SendResponse(resp)
}

func TestNewClient_GeneratesUniqueID(t *testing.T) {
	c1 := NewClient(nil, nil, "")
	c2 := NewClient(nil, nil, "")
	if c1.ID() == c2.ID() {
		t.Error("two clients should have different IDs")
	}
	// ID should be a valid UUID
	if _, err := uuid.Parse(c1.ID()); err != nil {
		t.Errorf("client ID should be a valid UUID, got %q", c1.ID())
	}
}
