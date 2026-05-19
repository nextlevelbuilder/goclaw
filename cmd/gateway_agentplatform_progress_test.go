package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func TestAgentPlatformProgressForwarderPostsMCPProgress(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	t.Cleanup(func() { security.SetAllowLoopbackForTest(false) })

	received := make(chan agentPlatformProgressCallback, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/progress" {
			t.Fatalf("path = %q, want /progress", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q", got)
		}
		var event agentPlatformProgressCallback
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		received <- event
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	t.Setenv(agentPlatformProgressURLEnv, server.URL+"/progress")
	t.Setenv(gatewayProgressTokenEnv, "test-token")

	msgBus := bus.New()
	deps := &gatewayDeps{msgBus: msgBus}
	deps.wireAgentPlatformProgressForwarder()

	msgBus.Broadcast(bus.Event{
		Name: protocol.EventAgent,
		Payload: agent.AgentEvent{
			Type:       protocol.AgentEventActivity,
			AgentID:    "agent-1",
			RunID:      "run-1",
			SessionKey: "session-1",
			Channel:    "ws",
			UserID:     "user-1",
			Metadata: map[string]string{
				"gateway_context_id":  "gwctx_1",
				"channel":             "dingtalk",
				"internal_session_id": "dts_1",
				"conversation_id":     "cid_1",
				"message_id":          "msg_1",
				"out_track_id":        "track_1",
				"reply_mode":          "card",
			},
			Payload: agentPlatformProgressPayload(),
		},
	})

	select {
	case got := <-received:
		if got.Payload["version"] != "goclaw.gateway.reply.v1" || got.Payload["kind"] != "progress" {
			t.Fatalf("unexpected gateway payload: %+v", got.Payload)
		}
		if got.Title != "正在填写表单" || got.Text != "浏览器动作：input" {
			t.Fatalf("unexpected top-level render fields: title=%q text=%q", got.Title, got.Text)
		}
		if got.Summary == nil || got.Questions == nil || got.Fields == nil {
			t.Fatalf("expected top-level render arrays, got summary=%v questions=%v fields=%v", got.Summary, got.Questions, got.Fields)
		}
		if got.GatewayContextID != "gwctx_1" || got.OutTrackID != "track_1" || got.Channel != "dingtalk" {
			t.Fatalf("unexpected top-level gateway fields: %+v", got)
		}
		if got.MessageID != "msg_1" || got.InternalSessionID != "dts_1" || got.ConversationID != "cid_1" || got.ReplyMode != "card" {
			t.Fatalf("unexpected top-level routing fields: %+v", got)
		}
		if got.Metadata["gateway_context_id"] != "gwctx_1" || got.Metadata["out_track_id"] != "track_1" {
			t.Fatalf("unexpected compatibility metadata: %+v", got.Metadata)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for progress POST")
	}
}

func TestAgentPlatformProgressForwarderSkipsAskUserByDefault(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	t.Cleanup(func() { security.SetAllowLoopbackForTest(false) })

	received := make(chan agentPlatformProgressCallback, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event agentPlatformProgressCallback
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		received <- event
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	t.Setenv(agentPlatformProgressURLEnv, server.URL+"/progress")
	t.Setenv(gatewayProgressTokenEnv, "test-token")

	msgBus := bus.New()
	deps := &gatewayDeps{msgBus: msgBus}
	deps.wireAgentPlatformProgressForwarder()

	msgBus.Broadcast(bus.Event{
		Name: protocol.EventAgent,
		Payload: agent.AgentEvent{
			Type:    protocol.AgentEventActivity,
			AgentID: "agent-1",
			RunID:   "run-1",
			Payload: agentPlatformProgressPayloadWithKind("ask_user"),
		},
	})

	select {
	case got := <-received:
		t.Fatalf("unexpected ask_user callback: %+v", got)
	case <-time.After(150 * time.Millisecond):
	}
}

func agentPlatformProgressPayload() map[string]any {
	return agentPlatformProgressPayloadWithKind("progress")
}

func agentPlatformProgressPayloadWithKind(kind string) map[string]any {
	return map[string]any{
		"phase":     "mcp_progress",
		"tool":      "mcp_mcp_agent__feishu_form_fill_input",
		"mcp_tool":  "feishu_form_fill_input",
		"progress":  70,
		"total":     100,
		"message":   "完成一个浏览器步骤",
		"event":     "step_end",
		"run_id":    "child-run-1",
		"timestamp": "2026-05-15T00:00:00Z",
		"event_data": map[string]any{
			"event":     "step_end",
			"run_id":    "child-run-1",
			"timestamp": "2026-05-15T00:00:00Z",
			"data": map[string]any{
				"payload": map[string]any{
					"version":   "goclaw.gateway.reply.v1",
					"kind":      kind,
					"title":     "正在填写表单",
					"text":      "浏览器动作：input",
					"summary":   []any{},
					"questions": []any{},
					"fields":    []any{},
				},
			},
		},
	}
}
