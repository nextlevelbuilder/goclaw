package methods

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// capturePublisher is a fake bus.EventPublisher that records broadcast events
// for assertions. It implements the full bus.EventPublisher interface.
type capturePublisher struct {
	mu     sync.Mutex
	events []bus.Event
	subs   map[string]bus.EventHandler
}

func newCapturePublisher() *capturePublisher {
	return &capturePublisher{subs: make(map[string]bus.EventHandler)}
}

func (p *capturePublisher) Subscribe(id string, handler bus.EventHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.subs[id] = handler
}

func (p *capturePublisher) Unsubscribe(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.subs, id)
}

func (p *capturePublisher) Broadcast(event bus.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, event)
}

func (p *capturePublisher) lastAudit() (bus.AuditEventPayload, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := len(p.events) - 1; i >= 0; i-- {
		if p.events[i].Name == protocol.EventAuditLog {
			payload, ok := p.events[i].Payload.(bus.AuditEventPayload)
			return payload, ok
		}
	}
	return bus.AuditEventPayload{}, false
}

// TestEmitAuditCtx_ContextTenantOverridesClient verifies that when a request
// context carries an explicit tenant, it wins over the client's fallback tenant.
func TestEmitAuditCtx_ContextTenantOverridesClient(t *testing.T) {
	pub := newCapturePublisher()
	client, _ := gateway.NewCapturingTestClient(permissions.RoleAdmin, uuid.MustParse("22222222-2222-2222-2222-222222222222"), "user-1", 4)

	ctxTenant := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ctx := store.WithTenantID(context.Background(), ctxTenant)
	emitAuditCtx(pub, client, ctx, "agent.create", "agent", "agent-1")

	payload, ok := pub.lastAudit()
	if !ok {
		t.Fatal("expected an audit event to be broadcast")
	}
	if payload.TenantID != ctxTenant {
		t.Errorf("TenantID = %v, want ctx tenant %v", payload.TenantID, ctxTenant)
	}
	if payload.Action != "agent.create" || payload.EntityType != "agent" || payload.EntityID != "agent-1" {
		t.Errorf("payload = %+v, want agent.create/agent/agent-1", payload)
	}
	if payload.ActorID != "user-1" {
		t.Errorf("ActorID = %q, want user-1", payload.ActorID)
	}
	if payload.ActorType != "user" {
		t.Errorf("ActorType = %q, want user", payload.ActorType)
	}
	if payload.IPAddress != "" {
		t.Errorf("IPAddress = %q, want empty (NewCapturingTestClient sets none)", payload.IPAddress)
	}
}

// TestEmitAuditCtx_FallsBackToClientTenant verifies that without a request
// context, the client's own resolved tenant is used (the pre-fix behavior was
// to emit the raw client tenant already, but the ctx-absent path is exercised
// here to lock the fallback ordering).
func TestEmitAuditCtx_FallsBackToClientTenant(t *testing.T) {
	pub := newCapturePublisher()
	clientTenant := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	client, _ := gateway.NewCapturingTestClient(permissions.RoleAdmin, clientTenant, "user-2", 4)

	emitAuditCtx(pub, client, nil, "session.create", "session", "session-1")

	payload, ok := pub.lastAudit()
	if !ok {
		t.Fatal("expected an audit event to be broadcast")
	}
	if payload.TenantID != clientTenant {
		t.Errorf("TenantID = %v, want client tenant %v", payload.TenantID, clientTenant)
	}
}

// TestEmitAudit_NilPublisherIsNoop verifies that a nil publisher is a safe
// no-op (no panic), including through the non-context wrapper.
func TestEmitAudit_NilPublisherIsNoop(t *testing.T) {
	client := gateway.NewTestClient(permissions.RoleAdmin, uuid.MustParse("44444444-4444-4444-4444-444444444444"), "user-3")
	emitAudit(nil, client, "agent.delete", "agent", "agent-1")
	emitAuditCtx(nil, client, context.Background(), "agent.delete", "agent", "agent-1")
}