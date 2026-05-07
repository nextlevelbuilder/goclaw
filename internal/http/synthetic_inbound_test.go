package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func TestSyntheticInbound_QueuesThreadScopedMessage(t *testing.T) {
	InitGatewayToken("test-token")
	t.Cleanup(func() { InitGatewayToken("") })

	msgBus := bus.New()
	h := NewSyntheticInboundHandler(msgBus)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := []byte(`{
		"message":"Investigate the failure and open a PR.",
		"channel":"discord-eng",
		"thread_id":"1501990716161654824",
		"display_name":"Gillen system",
		"metadata":{"event_type":"memory-updates-failure"}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/agents/eng/synthetic-inbound", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("X-GoClaw-User-Id", "agent-service")
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	ctx, cancel := testTimeout(t)
	defer cancel()
	msg, ok := msgBus.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("expected queued inbound message")
	}
	if msg.AgentID != "eng" || msg.Channel != "discord-eng" || msg.ChatID != "1501990716161654824" {
		t.Fatalf("bad route: agent=%q channel=%q chat=%q", msg.AgentID, msg.Channel, msg.ChatID)
	}
	if msg.SenderID != "system:agent-service" || msg.UserID != "agent-service" {
		t.Fatalf("bad sender/user: sender=%q user=%q", msg.SenderID, msg.UserID)
	}
	if msg.PeerKind != "group" {
		t.Fatalf("peer kind = %q, want group", msg.PeerKind)
	}
	if msg.TenantID != store.MasterTenantID {
		t.Fatalf("tenant = %s, want master", msg.TenantID)
	}
	if !strings.Contains(msg.Content, "[From: Gillen system]") ||
		!strings.Contains(msg.Content, "Investigate the failure") {
		t.Fatalf("content missing group attribution: %q", msg.Content)
	}
	if msg.Metadata["synthetic_inbound"] != "true" ||
		msg.Metadata["is_thread"] != "true" ||
		msg.Metadata["event_type"] != "memory-updates-failure" {
		t.Fatalf("metadata missing synthetic/thread/event fields: %#v", msg.Metadata)
	}
	if msg.Metadata[tools.MetaOriginSenderID] != "agent-service" ||
		msg.Metadata[tools.MetaOriginUserID] != "agent-service" ||
		msg.Metadata[tools.MetaOriginRole] != "admin" {
		t.Fatalf("metadata missing origin auth: %#v", msg.Metadata)
	}
	if !strings.HasPrefix(msg.Metadata["message_id"], "synthetic-") {
		t.Fatalf("message id = %q, want synthetic-*", msg.Metadata["message_id"])
	}

	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "accepted" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestSyntheticInboundRejectsRealSenderSpoof(t *testing.T) {
	InitGatewayToken("test-token")
	t.Cleanup(func() { InitGatewayToken("") })

	msgBus := bus.New()
	h := NewSyntheticInboundHandler(msgBus)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := []byte(`{"message":"x","channel":"discord-eng","thread_id":"t","sender_id":"real-user"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/agents/eng/synthetic-inbound", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("X-GoClaw-User-Id", "agent-service")
	rr := httptest.NewRecorder()

	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "sender_id must be an internal sender") {
		t.Fatalf("unexpected body: %s", rr.Body.String())
	}
}

func testTimeout(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 2*time.Second)
}
