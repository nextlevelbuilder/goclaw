package channels

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func TestHandleAgentEvent_QuickAckNonStreamingOnly(t *testing.T) {
	behavior := ResolvedChatBehavior{
		Enabled: true,
		QuickAck: ResolvedQuickAckConfig{
			Enabled:    true,
			Mode:       QuickAckModeFixedTemplate,
			MinDelayMs: 0,
			Templates:  []string{"On it."},
		},
	}

	mb := bus.New()
	mgr := NewManager(mb)
	mgr.RegisterChannel("test", &chatBehaviorTestChannel{name: "test"})
	mgr.RegisterRunWithBehavior("run-1", "test", "chat-1", "msg-1", map[string]string{"local_key": "chat-1/topic"}, uuid.Nil, false, false, true, behavior)

	mgr.HandleAgentEvent(protocol.AgentEventRunStarted, "run-1", nil)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, ok := mb.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected quick acknowledgement outbound message")
	}
	if got.Content != "On it." || got.ChatID != "chat-1" || got.Metadata["local_key"] != "chat-1/topic" {
		t.Fatalf("quick ack outbound = %+v, want content and routing metadata", got)
	}

	mb = bus.New()
	mgr = NewManager(mb)
	mgr.RegisterChannel("test", &chatBehaviorTestChannel{name: "test"})
	mgr.RegisterRunWithBehavior("run-2", "test", "chat-1", "msg-1", nil, uuid.Nil, true, false, true, behavior)

	mgr.HandleAgentEvent(protocol.AgentEventRunStarted, "run-2", nil)

	ctx, cancel = context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if got, ok := mb.SubscribeOutbound(ctx); ok {
		t.Fatalf("streaming run emitted quick ack: %+v", got)
	}
}

func TestUnregisterRun_CancelsPendingQuickAck(t *testing.T) {
	mb := bus.New()
	mgr := NewManager(mb)
	mgr.RegisterChannel("test", &chatBehaviorTestChannel{name: "test"})
	mgr.RegisterRunWithBehavior("run-1", "test", "chat-1", "msg-1", nil, uuid.Nil, false, false, true, ResolvedChatBehavior{
		Enabled: true,
		QuickAck: ResolvedQuickAckConfig{
			Enabled:    true,
			Mode:       QuickAckModeFixedTemplate,
			MinDelayMs: 500,
			Templates:  []string{"On it."},
		},
	})

	mgr.HandleAgentEvent(protocol.AgentEventRunStarted, "run-1", nil)
	mgr.UnregisterRun("run-1")

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	if got, ok := mb.SubscribeOutbound(ctx); ok {
		t.Fatalf("unregistered run emitted quick ack: %+v", got)
	}
}

func TestCancelQuickAck_BlocksInFlightSend(t *testing.T) {
	mb := bus.New()
	mgr := NewManager(mb)
	rc := &RunContext{
		ChannelName: "test",
		ChatID:      "chat-1",
		ChatBehavior: ResolvedChatBehavior{
			Enabled: true,
			QuickAck: ResolvedQuickAckConfig{
				Enabled:   true,
				Mode:      QuickAckModeFixedTemplate,
				Templates: []string{"On it."},
			},
		},
	}

	mgr.cancelQuickAck(rc)
	mgr.sendQuickAck(rc)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if got, ok := mb.SubscribeOutbound(ctx); ok {
		t.Fatalf("cancelled quick ack emitted message: %+v", got)
	}
}

func TestHandleAgentEvent_GeneratedProgressCancelsFallback(t *testing.T) {
	behavior := ResolvedChatBehavior{
		Enabled: true,
		IntermediateReplies: ResolvedIntermediateRepliesConfig{
			Enabled:   true,
			Mode:      IntermediateModeSidecar,
			MaxTokens: 20,
			MaxChars:  120,
		},
		QuickAck: ResolvedQuickAckConfig{
			Enabled:    true,
			Mode:       QuickAckModeLLMGenerated,
			MinDelayMs: 500,
			Templates:  []string{"Fallback."},
		},
	}

	mb := bus.New()
	mgr := NewManager(mb)
	mgr.RegisterChannel("test", &chatBehaviorTestChannel{name: "test"})
	mgr.RegisterRunWithDelivery("run-1", "test", "chat-1", "msg-1", map[string]string{"local_key": "chat-1/topic"}, uuid.Nil, false, false, true, behavior, DeliveryRuntime{
		ProgressGenerator: fakeDeliveryGenerator{content: "Mình đang kiểm tra tiếp."},
	})

	mgr.HandleAgentEvent(protocol.AgentEventRunStarted, "run-1", nil)
	mgr.HandleAgentEvent(protocol.AgentEventToolCall, "run-1", map[string]string{"name": "skill_search"})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, ok := mb.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected generated progress outbound message")
	}
	if got.Content != "Mình đang kiểm tra tiếp." || got.Metadata["local_key"] != "chat-1/topic" {
		t.Fatalf("generated progress outbound = %+v, want generated content and routing metadata", got)
	}

	ctx, cancel = context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	if got, ok := mb.SubscribeOutbound(ctx); ok {
		t.Fatalf("fallback emitted after generated progress: %+v", got)
	}
}

func TestHandleAgentEvent_ToolStatusMessagesRetired(t *testing.T) {
	mb := bus.New()
	mgr := NewManager(mb)
	mgr.RegisterChannel("test", &chatBehaviorTestChannel{name: "test"})
	mgr.RegisterRunWithBehavior("run-1", "test", "chat-1", "msg-1", nil, uuid.Nil, false, false, true, ResolvedChatBehavior{})

	mgr.HandleAgentEvent(protocol.AgentEventToolCall, "run-1", map[string]string{"name": "skill_search"})

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if got, ok := mb.SubscribeOutbound(ctx); ok {
		t.Fatalf("tool status emitted outbound message: %+v", got)
	}
}

func TestHandleAgentEvent_GeneratedQuickAckDoesNotUseTemplateWithoutGenerator(t *testing.T) {
	behavior := ResolvedChatBehavior{
		Enabled: true,
		QuickAck: ResolvedQuickAckConfig{
			Enabled:    true,
			Mode:       QuickAckModeLLMGenerated,
			MinDelayMs: 0,
			Templates:  []string{"Fallback."},
		},
	}

	mb := bus.New()
	mgr := NewManager(mb)
	mgr.RegisterChannel("test", &chatBehaviorTestChannel{name: "test"})
	mgr.RegisterRunWithBehavior("run-1", "test", "chat-1", "msg-1", nil, uuid.Nil, false, false, true, behavior)

	mgr.HandleAgentEvent(protocol.AgentEventRunStarted, "run-1", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if got, ok := mb.SubscribeOutbound(ctx); ok {
		t.Fatalf("generated quick ack used template without generator: %+v", got)
	}
}

func TestHandleAgentEvent_GeneratedQuickAckUsesSidecarGenerator(t *testing.T) {
	behavior := ResolvedChatBehavior{
		Enabled: true,
		QuickAck: ResolvedQuickAckConfig{
			Enabled:    true,
			Mode:       QuickAckModeLLMGenerated,
			MinDelayMs: 0,
			MaxTokens:  20,
			MaxChars:   80,
		},
	}

	mb := bus.New()
	mgr := NewManager(mb)
	mgr.RegisterChannel("test", &chatBehaviorTestChannel{name: "test"})
	mgr.RegisterRunWithDelivery("run-1", "test", "chat-1", "msg-1", nil, uuid.Nil, false, false, true, behavior, DeliveryRuntime{
		QuickAckGenerator: fakeDeliveryGenerator{content: "Mình nhận rồi, để mình xử lý."},
		Inbound:           "kiểm tra giúp tôi",
		Locale:            "vi",
	})

	mgr.HandleAgentEvent(protocol.AgentEventRunStarted, "run-1", nil)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, ok := mb.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected generated quick acknowledgement")
	}
	if got.Content != "Mình nhận rồi, để mình xử lý." {
		t.Fatalf("quick ack content = %q, want generated content", got.Content)
	}
}

func TestHandleAgentEvent_GeneratedQuickAckReceivesPersonaBrief(t *testing.T) {
	behavior := ResolvedChatBehavior{
		Enabled: true,
		QuickAck: ResolvedQuickAckConfig{
			Enabled:    true,
			Mode:       QuickAckModeLLMGenerated,
			MinDelayMs: 0,
			MaxTokens:  20,
			MaxChars:   80,
		},
	}

	requests := make(chan DeliveryMessageRequest, 1)
	mb := bus.New()
	mgr := NewManager(mb)
	mgr.RegisterChannel("test", &chatBehaviorTestChannel{name: "test"})
	mgr.RegisterRunWithDelivery("run-1", "test", "chat-1", "msg-1", nil, uuid.Nil, false, false, true, behavior, DeliveryRuntime{
		QuickAckGenerator: captureDeliveryGenerator{content: "Mình nhận rồi.", requests: requests},
		Inbound:           "kiểm tra giúp tôi",
		Locale:            "vi",
		PersonaBrief:      "Style: concise, warm",
	})

	mgr.HandleAgentEvent(protocol.AgentEventRunStarted, "run-1", nil)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, ok := mb.SubscribeOutbound(ctx); !ok {
		t.Fatal("expected generated quick acknowledgement")
	}

	select {
	case got := <-requests:
		if got.PersonaBrief != "Style: concise, warm" {
			t.Fatalf("quick ack persona brief = %q, want runtime persona", got.PersonaBrief)
		}
		if got.Purpose != DeliveryPurposeQuickAck {
			t.Fatalf("quick ack purpose = %q", got.Purpose)
		}
	case <-time.After(time.Second):
		t.Fatal("generator did not receive quick ack request")
	}
}

func TestHandleAgentEvent_GeneratedQuickAckDoesNotUseTemplateWhenGeneratorFails(t *testing.T) {
	behavior := ResolvedChatBehavior{
		Enabled: true,
		QuickAck: ResolvedQuickAckConfig{
			Enabled:    true,
			Mode:       QuickAckModeSidecar,
			MinDelayMs: 0,
			MaxTokens:  20,
			MaxChars:   120,
			Templates:  []string{defaultAckTemplate},
		},
	}

	mb := bus.New()
	mgr := NewManager(mb)
	mgr.RegisterChannel("test", &chatBehaviorTestChannel{name: "test"})
	mgr.RegisterRunWithDelivery("run-1", "test", "chat-1", "msg-1", nil, uuid.Nil, false, false, true, behavior, DeliveryRuntime{
		QuickAckGenerator: fakeDeliveryGenerator{err: errors.New("sidecar timeout")},
		Inbound:           "kiểm tra giúp tôi",
		Locale:            "vi",
	})

	mgr.HandleAgentEvent(protocol.AgentEventRunStarted, "run-1", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if got, ok := mb.SubscribeOutbound(ctx); ok {
		t.Fatalf("generated quick ack used template after generator failure: %+v", got)
	}
}

func TestHandleAgentEvent_IntermediateProgressDoesNotUseFallbackWhenGeneratorFails(t *testing.T) {
	behavior := ResolvedChatBehavior{
		Enabled: true,
		IntermediateReplies: ResolvedIntermediateRepliesConfig{
			Enabled:   true,
			Mode:      IntermediateModeSidecar,
			MaxTokens: 20,
			MaxChars:  120,
		},
		QuickAck: ResolvedQuickAckConfig{
			Enabled: false,
		},
	}

	mb := bus.New()
	mgr := NewManager(mb)
	mgr.RegisterChannel("test", &chatBehaviorTestChannel{name: "test"})
	mgr.RegisterRunWithDelivery("run-1", "test", "chat-1", "msg-1", map[string]string{"local_key": "chat-1/topic"}, uuid.Nil, false, false, true, behavior, DeliveryRuntime{
		ProgressGenerator: fakeDeliveryGenerator{err: errors.New("sidecar timeout")},
		Inbound:           "kiểm tra giúp tôi",
		Locale:            "vi",
	})

	mgr.HandleAgentEvent(protocol.AgentEventRunStarted, "run-1", nil)
	mgr.HandleAgentEvent(protocol.AgentEventToolCall, "run-1", map[string]string{"name": "skill_search"})

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if got, ok := mb.SubscribeOutbound(ctx); ok {
		t.Fatalf("intermediate progress used fallback after generator failure: %+v", got)
	}
}

func TestHandleAgentEvent_IntermediateProgressReceivesPersonaBrief(t *testing.T) {
	behavior := ResolvedChatBehavior{
		Enabled: true,
		IntermediateReplies: ResolvedIntermediateRepliesConfig{
			Enabled:   true,
			Mode:      IntermediateModeSidecar,
			MaxTokens: 20,
			MaxChars:  120,
		},
		QuickAck: ResolvedQuickAckConfig{Enabled: false},
	}

	requests := make(chan DeliveryMessageRequest, 1)
	mb := bus.New()
	mgr := NewManager(mb)
	mgr.RegisterChannel("test", &chatBehaviorTestChannel{name: "test"})
	mgr.RegisterRunWithDelivery("run-1", "test", "chat-1", "msg-1", nil, uuid.Nil, false, false, true, behavior, DeliveryRuntime{
		ProgressGenerator: captureDeliveryGenerator{content: "Đang soi tiếp.", requests: requests},
		Inbound:           "kiểm tra giúp tôi",
		Locale:            "vi",
		PersonaBrief:      "Style: concise, warm",
	})

	mgr.HandleAgentEvent(protocol.AgentEventToolCall, "run-1", map[string]string{"name": "skill_search"})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, ok := mb.SubscribeOutbound(ctx); !ok {
		t.Fatal("expected generated progress outbound message")
	}

	select {
	case got := <-requests:
		if got.PersonaBrief != "Style: concise, warm" {
			t.Fatalf("progress persona brief = %q, want runtime persona", got.PersonaBrief)
		}
		if got.Purpose != DeliveryPurposeProgress {
			t.Fatalf("progress purpose = %q", got.Purpose)
		}
	case <-time.After(time.Second):
		t.Fatal("generator did not receive progress request")
	}
}

func TestHandleAgentEvent_FixedQuickAckPreservesInitialExplicitBlockReply(t *testing.T) {
	behavior := ResolvedChatBehavior{
		Enabled: true,
		QuickAck: ResolvedQuickAckConfig{
			Enabled:    true,
			Mode:       QuickAckModeFixedTemplate,
			MinDelayMs: 50,
			Templates:  []string{"Fallback."},
		},
	}

	mb := bus.New()
	mgr := NewManager(mb)
	mgr.RegisterChannel("test", &chatBehaviorTestChannel{name: "test"})
	mgr.RegisterRunWithBehavior("run-1", "test", "chat-1", "msg-1", nil, uuid.Nil, false, true, true, behavior)

	mgr.HandleAgentEvent(protocol.AgentEventRunStarted, "run-1", nil)
	mgr.HandleAgentEvent(protocol.AgentEventBlockReply, "run-1", map[string]string{"content": "Explicit block reply."})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, ok := mb.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected explicit block reply outbound message")
	}
	if got.Content != "Explicit block reply." {
		t.Fatalf("explicit block reply content = %q", got.Content)
	}

	ctx, cancel = context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	if got, ok := mb.SubscribeOutbound(ctx); ok {
		t.Fatalf("fallback emitted after explicit block reply: %+v", got)
	}
}

func TestHandleAgentEvent_QuickAckDisabledSuppressesInitialExplicitBlockReply(t *testing.T) {
	for _, tc := range []struct {
		name  string
		quick ResolvedQuickAckConfig
	}{
		{
			name:  "enabled_false",
			quick: ResolvedQuickAckConfig{Enabled: false, Templates: []string{"Fallback."}},
		},
		{
			name:  "mode_off",
			quick: ResolvedQuickAckConfig{Enabled: true, Mode: QuickAckModeOff, Templates: []string{"Fallback."}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			behavior := ResolvedChatBehavior{
				Enabled:  true,
				QuickAck: tc.quick,
			}

			mb := bus.New()
			mgr := NewManager(mb)
			mgr.RegisterChannel("test", &chatBehaviorTestChannel{name: "test"})
			mgr.RegisterRunWithBehavior("run-1", "test", "chat-1", "msg-1", nil, uuid.Nil, false, true, true, behavior)

			mgr.HandleAgentEvent(protocol.AgentEventRunStarted, "run-1", nil)
			mgr.HandleAgentEvent(protocol.AgentEventBlockReply, "run-1", map[string]string{"content": "Initial quick acknowledgement."})

			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()
			if got, ok := mb.SubscribeOutbound(ctx); ok {
				t.Fatalf("initial explicit block reply emitted with quick ack disabled: %+v", got)
			}

			delivered, last := mgr.InterimDeliverySnapshot("run-1")
			if delivered != 0 || last != "" {
				t.Fatalf("interim delivery snapshot = (%d, %q), want none", delivered, last)
			}

			mgr.HandleAgentEvent(protocol.AgentEventBlockReply, "run-1", map[string]string{"content": "Second iteration update."})

			ctx, cancel = context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			got, ok := mb.SubscribeOutbound(ctx)
			if !ok {
				t.Fatal("expected second explicit block reply outbound message")
			}
			if got.Content != "Second iteration update." {
				t.Fatalf("explicit block reply content = %q", got.Content)
			}
			delivered, last = mgr.InterimDeliverySnapshot("run-1")
			if delivered != 1 || last != "Second iteration update." {
				t.Fatalf("interim delivery snapshot = (%d, %q), want delivered second reply", delivered, last)
			}

			mgr.HandleAgentEvent(protocol.AgentEventRunCompleted, "run-1", nil)
			delivered, last = mgr.InterimDeliverySnapshot("run-1")
			if delivered != 1 || last != "Second iteration update." {
				t.Fatalf("completed interim delivery snapshot = (%d, %q), want preserved second reply until unregister", delivered, last)
			}
			mgr.UnregisterRun("run-1")
			delivered, last = mgr.InterimDeliverySnapshot("run-1")
			if delivered != 0 || last != "" {
				t.Fatalf("unregistered interim delivery snapshot = (%d, %q), want none", delivered, last)
			}
		})
	}
}

func TestHandleAgentEvent_ToolAnnouncementBypassesInitialQuickAckSuppression(t *testing.T) {
	for _, tc := range []struct {
		name  string
		quick ResolvedQuickAckConfig
	}{
		{
			name:  "enabled_false",
			quick: ResolvedQuickAckConfig{Enabled: false, Templates: []string{"Fallback."}},
		},
		{
			name:  "mode_off",
			quick: ResolvedQuickAckConfig{Enabled: true, Mode: QuickAckModeOff, Templates: []string{"Fallback."}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			behavior := ResolvedChatBehavior{
				Enabled:  true,
				QuickAck: tc.quick,
			}

			mb := bus.New()
			mgr := NewManager(mb)
			mgr.RegisterChannel("test", &chatBehaviorTestChannel{name: "test"})
			mgr.RegisterRunWithBehavior("run-1", "test", "chat-1", "msg-1", nil, uuid.Nil, false, true, true, behavior)

			mgr.HandleAgentEvent(protocol.AgentEventRunStarted, "run-1", nil)
			mgr.HandleAgentEvent(protocol.AgentEventBlockReply, "run-1", map[string]string{
				"content": "Tôi sẽ dùng `skill_search` để xử lý bước tiếp theo.",
				"source":  protocol.BlockReplySourceToolAnnouncement,
			})

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			got, ok := mb.SubscribeOutbound(ctx)
			if !ok {
				t.Fatal("expected tool announcement outbound message")
			}
			if got.Content != "Tôi sẽ dùng `skill_search` để xử lý bước tiếp theo." {
				t.Fatalf("tool announcement content = %q", got.Content)
			}

			delivered, last := mgr.InterimDeliverySnapshot("run-1")
			if delivered != 1 || last != got.Content {
				t.Fatalf("interim delivery snapshot = (%d, %q), want delivered announcement", delivered, last)
			}
		})
	}
}

func TestHandleAgentEvent_FixedQuickAckIgnoresPersonaBrief(t *testing.T) {
	behavior := ResolvedChatBehavior{
		Enabled: true,
		QuickAck: ResolvedQuickAckConfig{
			Enabled:    true,
			Mode:       QuickAckModeFixedTemplate,
			MinDelayMs: 0,
			MaxChars:   120,
			Templates:  []string{"Checking the tool result now."},
		},
	}

	requests := make(chan DeliveryMessageRequest, 1)
	mb := bus.New()
	mgr := NewManager(mb)
	mgr.RegisterChannel("test", &chatBehaviorTestChannel{name: "test"})
	mgr.RegisterRunWithDelivery("run-1", "test", "chat-1", "msg-1", nil, uuid.Nil, false, false, true, behavior, DeliveryRuntime{
		QuickAckGenerator: captureDeliveryGenerator{content: "Generated.", requests: requests},
		PersonaBrief:      "Style: concise, warm",
	})

	mgr.HandleAgentEvent(protocol.AgentEventRunStarted, "run-1", nil)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, ok := mb.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected fixed quick acknowledgement")
	}
	if got.Content != "Checking the tool result now." {
		t.Fatalf("fixed quick ack content = %q, want template", got.Content)
	}

	select {
	case req := <-requests:
		t.Fatalf("fixed quick ack unexpectedly called generator with request %+v", req)
	default:
	}
}

type fakeDeliveryGenerator struct {
	content string
	err     error
}

func (g fakeDeliveryGenerator) GenerateDeliveryMessage(context.Context, DeliveryMessageRequest) (string, error) {
	return g.content, g.err
}

type captureDeliveryGenerator struct {
	content  string
	requests chan<- DeliveryMessageRequest
}

func (g captureDeliveryGenerator) GenerateDeliveryMessage(_ context.Context, req DeliveryMessageRequest) (string, error) {
	if g.requests != nil {
		g.requests <- req
	}
	return g.content, nil
}
