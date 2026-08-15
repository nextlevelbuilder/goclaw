package gateway

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

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
