package gateway

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// TestHandleConnect_AuditsGatewayTokenLogin verifies the WS connect path emits
// an auth.login audit event with the gateway_token method and a resolved tenant.
func TestHandleConnect_AuditsGatewayTokenLogin(t *testing.T) {
	cfg := config.Default()
	cfg.Gateway.Token = "secret"
	cfg.Gateway.Host = "127.0.0.1"

	mb := bus.New()
	server := NewServer(cfg, mb, nil, nil)

	var mu sync.Mutex
	var got *bus.AuditEventPayload
	mb.Subscribe(bus.TopicAudit, func(evt bus.Event) {
		if evt.Name != protocol.EventAuditLog {
			return
		}
		p, ok := evt.Payload.(bus.AuditEventPayload)
		if !ok {
			return
		}
		if p.Action != "auth.login" {
			return
		}
		mu.Lock()
		got = &p
		mu.Unlock()
	})

	client := NewClient(nil, server, "203.0.113.10")
	req := &protocol.RequestFrame{ID: "req-1", Method: protocol.MethodConnect}
	req.Params = json.RawMessage(`{"token":"secret","user_id":"owner-x"}`)

	server.router.Handle(context.Background(), client, req)

	if !client.authenticated {
		t.Fatal("expected authenticated client")
	}
	mu.Lock()
	audit := got
	mu.Unlock()
	if audit == nil {
		t.Fatal("no auth.login audit event emitted")
	}
	if audit.EntityType != "auth" {
		t.Errorf("EntityType = %q, want auth", audit.EntityType)
	}
	if audit.EntityID != "gateway_token" {
		t.Errorf("method = %q, want gateway_token", audit.EntityID)
	}
	if audit.ActorID != "owner-x" {
		t.Errorf("ActorID = %q, want owner-x", audit.ActorID)
	}
	// Owner with no tenant hint resolves to master tenant.
	if audit.TenantID != store.MasterTenantID {
		t.Errorf("TenantID = %v, want master %v", audit.TenantID, store.MasterTenantID)
	}
	if audit.IPAddress != "203.0.113.10" {
		t.Errorf("IPAddress = %q, want 203.0.113.10", audit.IPAddress)
	}
}

// TestHandleConnect_AuditsRejectedLogin verifies a fail-closed connect (no
// valid credentials) emits auth.login_failed.
func TestHandleConnect_AuditsRejectedLogin(t *testing.T) {
	cfg := config.Default()
	cfg.Gateway.Token = "secret"
	cfg.Gateway.Host = "127.0.0.1"

	mb := bus.New()
	server := NewServer(cfg, mb, nil, nil)

	var mu sync.Mutex
	var got *bus.AuditEventPayload
	mb.Subscribe(bus.TopicAudit, func(evt bus.Event) {
		if evt.Name != protocol.EventAuditLog {
			return
		}
		p, ok := evt.Payload.(bus.AuditEventPayload)
		if !ok {
			return
		}
		if p.Action != "auth.login_failed" {
			return
		}
		mu.Lock()
		got = &p
		mu.Unlock()
	})

	client := NewClient(nil, server, "203.0.113.10")
	req := &protocol.RequestFrame{ID: "req-1", Method: protocol.MethodConnect}
	req.Params = json.RawMessage(`{"token":"wrong","user_id":"intruder"}`)

	server.router.Handle(context.Background(), client, req)

	if client.authenticated {
		t.Fatal("expected unauthenticated client")
	}
	mu.Lock()
	audit := got
	mu.Unlock()
	if audit == nil {
		t.Fatal("no auth.login_failed audit event emitted")
	}
	if audit.EntityID != "invalid_credentials" {
		t.Errorf("method = %q, want invalid_credentials", audit.EntityID)
	}
	if audit.ActorID != "intruder" {
		t.Errorf("ActorID = %q, want intruder", audit.ActorID)
	}
}

// TestRouterTenantID_Ordering verifies the tenant resolution precedence for
// WS audit events: ctx tenant wins, then client tenant, then master fallback.
func TestRouterTenantID_Ordering(t *testing.T) {
	cfg := config.Default()
	server := NewServer(cfg, nil, nil, nil)
	router := server.router

	ctxTenant := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	clientTenant := uuid.MustParse("66666666-6666-6666-6666-666666666666")

	// 1. ctx tenant wins.
	client := NewClient(nil, server, "203.0.113.10")
	client.tenantID = clientTenant
	ctx := store.WithTenantID(context.Background(), ctxTenant)
	if got := router.routerTenantID(ctx, client); got != ctxTenant {
		t.Errorf("ctx tenant = %v, want %v", got, ctxTenant)
	}

	// 2. client tenant used when ctx has none.
	ctx = context.Background()
	if got := router.routerTenantID(ctx, client); got != clientTenant {
		t.Errorf("client tenant = %v, want %v", got, clientTenant)
	}

	// 3. master fallback when neither is set.
	client2 := NewClient(nil, server, "203.0.113.10")
	if got := router.routerTenantID(ctx, client2); got != store.MasterTenantID {
		t.Errorf("master fallback = %v, want %v", got, store.MasterTenantID)
	}
}

func TestHandleConnectRejectsNoTokenExternalBind(t *testing.T) {
	cfg := config.Default()
	cfg.Gateway.Host = "0.0.0.0"
	cfg.Gateway.Token = ""
	t.Setenv(config.GatewayAllowInsecureNoAuthEnv, "")

	server := NewServer(cfg, nil, nil, nil)
	client := NewClient(nil, server, "203.0.113.10")
	req := &protocol.RequestFrame{ID: "req-1", Method: protocol.MethodConnect}

	server.router.Handle(context.Background(), client, req)

	if client.authenticated {
		t.Fatal("expected unauthenticated client for external no-token connect")
	}
	if client.role != "" {
		t.Fatalf("role = %q, want empty", client.role)
	}
	select {
	case raw := <-client.send:
		var resp protocol.ResponseFrame
		if err := json.Unmarshal(raw, &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Error == nil || resp.Error.Code != protocol.ErrUnauthorized {
			t.Fatalf("response error = %#v, want unauthorized", resp.Error)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected unauthorized response")
	}
}

func TestHandleConnectAllowsExplicitInsecureNoTokenOptIn(t *testing.T) {
	cfg := config.Default()
	cfg.Gateway.Host = "0.0.0.0"
	cfg.Gateway.Token = ""
	t.Setenv(config.GatewayAllowInsecureNoAuthEnv, "1")

	server := NewServer(cfg, nil, nil, nil)
	client := NewClient(nil, server, "127.0.0.1")
	req := &protocol.RequestFrame{ID: "req-1", Method: protocol.MethodConnect}

	server.router.Handle(context.Background(), client, req)

	if !client.authenticated {
		t.Fatal("expected authenticated client with explicit insecure opt-in")
	}
	if client.role != permissions.RoleOperator {
		t.Fatalf("role = %q, want operator", client.role)
	}
}
