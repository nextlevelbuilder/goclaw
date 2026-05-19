package channels

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

type fakeGatewayProgressChannel struct {
	*BaseChannel
	events []GatewayProgressEvent
}

func newFakeGatewayProgressChannel(name string, msgBus *bus.MessageBus) *fakeGatewayProgressChannel {
	return &fakeGatewayProgressChannel{BaseChannel: NewBaseChannel(name, msgBus, nil)}
}

func (c *fakeGatewayProgressChannel) Start(context.Context) error { return nil }
func (c *fakeGatewayProgressChannel) Stop(context.Context) error  { return nil }
func (c *fakeGatewayProgressChannel) Send(context.Context, bus.OutboundMessage) error {
	return nil
}
func (c *fakeGatewayProgressChannel) OnGatewayProgress(_ context.Context, event GatewayProgressEvent) error {
	c.events = append(c.events, event)
	return nil
}

func TestHandleAgentEventForwardsGatewayProgressChannel(t *testing.T) {
	msgBus := bus.New()
	mgr := NewManager(msgBus)
	ch := newFakeGatewayProgressChannel("gateway-test", msgBus)
	mgr.RegisterChannel("gateway-test", ch)
	mgr.RegisterRun("run-1", "gateway-test", "chat-1", "msg-1", map[string]string{
		"out_track_id": "track-1",
		"channel":      "dingtalk",
	}, uuid.Nil, false, false, false)

	mgr.HandleAgentEvent(protocol.AgentEventActivity, "run-1", gatewayProgressPayload())

	if len(ch.events) != 1 {
		t.Fatalf("expected 1 gateway event, got %d", len(ch.events))
	}
	got := ch.events[0]
	if got.EventType != gatewayProgressEventType {
		t.Fatalf("EventType = %q, want %q", got.EventType, gatewayProgressEventType)
	}
	if got.Version != gatewayReplyVersion {
		t.Fatalf("Version = %q, want %q", got.Version, gatewayReplyVersion)
	}
	if got.Kind != "progress" {
		t.Fatalf("Kind = %q, want progress", got.Kind)
	}
	if got.RunID != "run-1" || got.ChatID != "chat-1" || got.MessageID != "msg-1" {
		t.Fatalf("unexpected routing fields: %+v", got)
	}
	if got.Payload["title"] != "正在填写表单" {
		t.Fatalf("payload title = %v", got.Payload["title"])
	}
	if got.GatewayContext["out_track_id"] != "track-1" || got.GatewayContext["channel"] != "dingtalk" {
		t.Fatalf("unexpected gateway context: %+v", got.GatewayContext)
	}
	if got.Metadata["out_track_id"] != "track-1" || got.Metadata["channel"] != "dingtalk" {
		t.Fatalf("unexpected metadata: %+v", got.Metadata)
	}
}

func TestHandleAgentEventCanPublishGatewayProgressAsOutboundJSON(t *testing.T) {
	msgBus := bus.New()
	mgr := NewManager(msgBus)
	ch := newFakeHealthChannel("plain-channel")
	mgr.RegisterChannel("plain-channel", ch)
	mgr.RegisterRun("run-1", "plain-channel", "chat-1", "msg-1", map[string]string{
		gatewayProgressModeMetaKey: gatewayProgressModeJSON,
	}, uuid.Nil, false, false, false)

	mgr.HandleAgentEvent(protocol.AgentEventActivity, "run-1", gatewayProgressPayload())

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	msg, ok := msgBus.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected outbound gateway progress message")
	}
	if msg.Metadata[gatewayProgressMetaKey] != gatewayProgressEventType {
		t.Fatalf("metadata event = %q", msg.Metadata[gatewayProgressMetaKey])
	}

	var event GatewayProgressEvent
	if err := json.Unmarshal([]byte(msg.Content), &event); err != nil {
		t.Fatalf("invalid outbound JSON: %v\n%s", err, msg.Content)
	}
	if event.Kind != "progress" || event.Payload["text"] != "浏览器动作：input" {
		t.Fatalf("unexpected outbound event: %+v", event)
	}
}

func gatewayProgressPayload() map[string]any {
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
					"version":   gatewayReplyVersion,
					"kind":      "progress",
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
