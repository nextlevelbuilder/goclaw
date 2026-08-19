package gateway

import (
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// TestUnregisterClient_EmitsAuthLogout verifies that disconnecting an
// authenticated client produces an auth.logout audit event with the client's
// tenant.
func TestUnregisterClient_EmitsAuthLogout(t *testing.T) {
	cfg := config.Default()
	mb := bus.New()
	server := NewServer(cfg, mb, nil, nil)

	var mu sync.Mutex
	var logoutEvents []bus.AuditEventPayload
	mb.Subscribe(bus.TopicAudit, func(evt bus.Event) {
		if evt.Name != protocol.EventAuditLog {
			return
		}
		p, ok := evt.Payload.(bus.AuditEventPayload)
		if !ok || p.Action != "auth.logout" {
			return
		}
		mu.Lock()
		logoutEvents = append(logoutEvents, p)
		mu.Unlock()
	})

	tenantID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	client := NewTestClient(permissions.RoleAdmin, tenantID, "user-logout")
	server.registerClient(client)
	server.unregisterClient(client)

	mu.Lock()
	defer mu.Unlock()
	if len(logoutEvents) != 1 {
		t.Fatalf("logout events = %d, want 1", len(logoutEvents))
	}
	evt := logoutEvents[0]
	if evt.ActorID != "user-logout" {
		t.Errorf("ActorID = %q, want user-logout", evt.ActorID)
	}
	if evt.EntityType != "auth" || evt.EntityID != "disconnect" {
		t.Errorf("EntityType/EntityID = %q/%q, want auth/disconnect", evt.EntityType, evt.EntityID)
	}
	if evt.TenantID != tenantID {
		t.Errorf("TenantID = %v, want %v", evt.TenantID, tenantID)
	}
}

// TestUnregisterClient_NoLogoutForUnauthenticated verifies that a client that
// never authenticated (connect failure) produces no auth.logout event.
func TestUnregisterClient_NoLogoutForUnauthenticated(t *testing.T) {
	cfg := config.Default()
	mb := bus.New()
	server := NewServer(cfg, mb, nil, nil)

	var mu sync.Mutex
	var logoutCount int
	mb.Subscribe(bus.TopicAudit, func(evt bus.Event) {
		if evt.Name != protocol.EventAuditLog {
			return
		}
		if p, ok := evt.Payload.(bus.AuditEventPayload); ok && p.Action == "auth.logout" {
			mu.Lock()
			logoutCount++
			mu.Unlock()
		}
	})

	client := NewClient(nil, server, "203.0.113.10") // not authenticated
	server.registerClient(client)
	server.unregisterClient(client)

	mu.Lock()
	defer mu.Unlock()
	if logoutCount != 0 {
		t.Fatalf("logout events = %d, want 0 for unauthenticated client", logoutCount)
	}
}