package tools

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// recordingPub captures broadcast events for assertions.
type recordingPub struct {
	mu     sync.Mutex
	events []bus.Event
}

func (p *recordingPub) Subscribe(string, bus.EventHandler) {}
func (p *recordingPub) Unsubscribe(string)                 {}
func (p *recordingPub) Broadcast(e bus.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, e)
}
func (p *recordingPub) snapshot() []bus.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]bus.Event, len(p.events))
	copy(out, p.events)
	return out
}

func findEvent(t *testing.T, pub *recordingPub, name string) bus.Event {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, e := range pub.snapshot() {
			if e.Name == name {
				return e
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("event %q was never published (got %v)", name, pub.snapshot())
	return bus.Event{}
}

func payloadOf(t *testing.T, e bus.Event) map[string]any {
	t.Helper()
	m, ok := e.Payload.(map[string]any)
	if !ok {
		t.Fatalf("event %q payload is %T, want map[string]any", e.Name, e.Payload)
	}
	return m
}

// A raised approval must publish exec.approval.requested carrying userId — the
// gateway event filter scopes exec.approval.* by that field and BROADCASTS when
// it is absent, which would leak one user's pending approvals to everyone.
func TestRequestApproval_PublishesRequestedWithUserScope(t *testing.T) {
	pub := &recordingPub{}
	m := NewExecApprovalManager(DefaultExecApprovalConfig())
	m.SetEventBus(pub)

	tenant := uuid.New()
	done := make(chan ApprovalDecision, 1)
	go func() {
		d, _ := m.RequestApproval(ApprovalRequest{
			ToolName:   "delegate_external",
			Detail:     "claude -p 'port the parser'",
			AgentID:    "agent-1",
			SessionKey: "sess-1",
			UserID:     "user-42",
			TenantID:   tenant,
		}, 2*time.Second)
		done <- d
	}()

	ev := findEvent(t, pub, protocol.EventExecApprovalReq)
	if ev.TenantID != tenant {
		t.Fatalf("event tenant = %v, want %v (tenant filter is fail-closed)", ev.TenantID, tenant)
	}
	p := payloadOf(t, ev)
	if p["userId"] != "user-42" {
		t.Fatalf("payload userId = %v, want user-42", p["userId"])
	}
	for k, want := range map[string]any{
		"toolName":   "delegate_external",
		"detail":     "claude -p 'port the parser'",
		"agentId":    "agent-1",
		"sessionKey": "sess-1",
	} {
		if p[k] != want {
			t.Fatalf("payload %s = %v, want %v", k, p[k], want)
		}
	}
	id, _ := p["id"].(string)
	if id == "" {
		t.Fatal("payload has no id — the UI cannot resolve the approval")
	}

	if err := m.Resolve(id, ApprovalAllowOnce); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if d := <-done; d != ApprovalAllowOnce {
		t.Fatalf("decision = %q, want %q", d, ApprovalAllowOnce)
	}

	res := findEvent(t, pub, protocol.EventExecApprovalRes)
	rp := payloadOf(t, res)
	if rp["decision"] != string(ApprovalAllowOnce) {
		t.Fatalf("resolved decision = %v, want %v", rp["decision"], ApprovalAllowOnce)
	}
	if rp["userId"] != "user-42" {
		t.Fatalf("resolved userId = %v, want user-42", rp["userId"])
	}
}

// A timed-out approval must still publish a resolved event, otherwise the row
// lingers in the UI until a manual reload.
func TestRequestApproval_TimeoutPublishesResolved(t *testing.T) {
	pub := &recordingPub{}
	m := NewExecApprovalManager(DefaultExecApprovalConfig())
	m.SetEventBus(pub)

	d, err := m.RequestApproval(ApprovalRequest{Command: "rm -rf /", UserID: "u1"}, 20*time.Millisecond)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if d != ApprovalDeny {
		t.Fatalf("timeout decision = %q, want %q", d, ApprovalDeny)
	}
	rp := payloadOf(t, findEvent(t, pub, protocol.EventExecApprovalRes))
	if rp["reason"] != "timeout" {
		t.Fatalf("resolved reason = %v, want timeout", rp["reason"])
	}
	if len(m.ListPending()) != 0 {
		t.Fatal("timed-out approval still pending")
	}
}

// The broker must work with no bus wired (tests, embedded callers).
func TestRequestApproval_NilBusIsSafe(t *testing.T) {
	m := NewExecApprovalManager(DefaultExecApprovalConfig())
	m.SetEventBus(nil)

	done := make(chan ApprovalDecision, 1)
	go func() {
		d, _ := m.RequestExecApproval(context.Background(), "apt-get install jq", "agent-1", 2*time.Second)
		done <- d
	}()

	deadline := time.Now().Add(2 * time.Second)
	var pending []*PendingApproval
	for time.Now().Before(deadline) {
		if pending = m.ListPending(); len(pending) == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(pending))
	}
	// The exec path keeps filling Command (its UI and the always-allow allowlist
	// both depend on it) and defaults ToolName/Detail from it.
	if pending[0].Command != "apt-get install jq" {
		t.Fatalf("Command = %q", pending[0].Command)
	}
	if pending[0].ToolName != ToolNameExec {
		t.Fatalf("ToolName = %q, want %q", pending[0].ToolName, ToolNameExec)
	}
	if pending[0].Detail != "apt-get install jq" {
		t.Fatalf("Detail = %q, want the command", pending[0].Detail)
	}
	if err := m.Resolve(pending[0].ID, ApprovalDeny); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if d := <-done; d != ApprovalDeny {
		t.Fatalf("decision = %q, want %q", d, ApprovalDeny)
	}
}

// allow-always must still extend the dynamic allowlist for the exec path.
func TestRequestApproval_AllowAlwaysStillExtendsAllowlist(t *testing.T) {
	m := NewExecApprovalManager(ExecApprovalConfig{Security: ExecSecurityFull, Ask: ExecAskOnMiss})

	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if p := m.ListPending(); len(p) == 1 {
				m.Resolve(p[0].ID, ApprovalAllowAlways)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	if got := m.CheckCommand("terraform apply"); got != "ask" {
		t.Fatalf("CheckCommand before approval = %q, want ask", got)
	}
	if _, err := m.RequestExecApproval(context.Background(), "terraform apply", "agent-1", 2*time.Second); err != nil {
		t.Fatalf("RequestExecApproval: %v", err)
	}
	if got := m.CheckCommand("terraform plan"); got != "allow" {
		t.Fatalf("CheckCommand after allow-always = %q, want allow", got)
	}
}

// NewApprovalRequest lifts the scope fields off the context so every call site
// scopes its approval identically.
func TestNewApprovalRequest_ReadsContextScope(t *testing.T) {
	tenant := uuid.New()
	ctx := store.WithUserID(context.Background(), "user-7")
	ctx = store.WithTenantID(ctx, tenant)

	req := NewApprovalRequest(ctx, "exec", "agent-9")
	if req.UserID != "user-7" {
		t.Fatalf("UserID = %q, want user-7", req.UserID)
	}
	if req.TenantID != tenant {
		t.Fatalf("TenantID = %v, want %v", req.TenantID, tenant)
	}
	if req.ToolName != "exec" || req.AgentID != "agent-9" {
		t.Fatalf("unexpected request %+v", req)
	}
}
