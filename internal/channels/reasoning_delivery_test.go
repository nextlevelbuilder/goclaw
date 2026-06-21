package channels

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func TestResolveReasoningDelivery_PrecedenceAndLegacyFallback(t *testing.T) {
	falseValue := false
	trueValue := true

	tests := []struct {
		name          string
		mode          string
		legacy        *bool
		wantMode      string
		wantShow      bool
		wantForce     bool
		wantBubbles   bool
		wantStreaming bool
	}{
		{
			name:          "explicit always bubbles wins over legacy false",
			mode:          ReasoningDeliveryAlwaysBubbles,
			legacy:        &falseValue,
			wantMode:      ReasoningDeliveryAlwaysBubbles,
			wantShow:      true,
			wantForce:     true,
			wantBubbles:   true,
			wantStreaming: true,
		},
		{
			name:          "explicit streaming only wins over legacy false",
			mode:          ReasoningDeliveryStreamingOnly,
			legacy:        &falseValue,
			wantMode:      ReasoningDeliveryStreamingOnly,
			wantShow:      true,
			wantForce:     false,
			wantBubbles:   false,
			wantStreaming: false,
		},
		{
			name:          "explicit off wins over legacy true",
			mode:          ReasoningDeliveryOff,
			legacy:        &trueValue,
			wantMode:      ReasoningDeliveryOff,
			wantShow:      false,
			wantForce:     false,
			wantBubbles:   false,
			wantStreaming: true,
		},
		{
			name:          "legacy false maps to off",
			mode:          "",
			legacy:        &falseValue,
			wantMode:      ReasoningDeliveryOff,
			wantShow:      false,
			wantForce:     false,
			wantBubbles:   false,
			wantStreaming: false,
		},
		{
			name:          "missing legacy preserves streaming only default",
			mode:          "",
			legacy:        nil,
			wantMode:      ReasoningDeliveryStreamingOnly,
			wantShow:      true,
			wantForce:     false,
			wantBubbles:   false,
			wantStreaming: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveReasoningDelivery(tc.mode, tc.legacy)
			if got.Mode != tc.wantMode || got.ShowInChannel != tc.wantShow || got.ForceProviderStream != tc.wantForce || got.BubbleDelivery != tc.wantBubbles {
				t.Fatalf("ResolveReasoningDelivery() = %+v", got)
			}
			if ShouldStreamProviderForDelivery(tc.wantStreaming, got) != (tc.wantStreaming || tc.wantForce) {
				t.Fatalf("ShouldStreamProviderForDelivery(%v, %+v) returned unexpected result", tc.wantStreaming, got)
			}
		})
	}
}

func TestHandleAgentEvent_AlwaysBubblesReasoningForNonStreamingRun(t *testing.T) {
	mb := bus.New()
	mgr := NewManager(mb)
	mgr.RegisterChannel("test", &chatBehaviorTestChannel{name: "test"})
	delivery := ResolveReasoningDelivery(ReasoningDeliveryAlwaysBubbles, nil)
	mgr.RegisterRunWithBehavior("run-1", "test", "chat-1", "msg-1", map[string]string{"local_key": "chat-1/topic"}, uuid.Nil, false, false, true, ResolvedChatBehavior{}, delivery)

	mgr.HandleAgentEvent(protocol.ChatEventThinking, "run-1", map[string]string{"content": "first "})
	mgr.HandleAgentEvent(protocol.ChatEventThinking, "run-1", map[string]string{"content": "second"})
	mgr.HandleAgentEvent(protocol.ChatEventChunk, "run-1", map[string]string{"content": "final chunk should stay internal"})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, ok := mb.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected batched reasoning outbound message")
	}
	if got.ChatID != "chat-1" || got.Metadata["local_key"] != "chat-1/topic" {
		t.Fatalf("reasoning outbound routing = %+v", got)
	}
	if !strings.Contains(got.Content, "first second") {
		t.Fatalf("reasoning outbound content = %q, want batched thinking", got.Content)
	}
	if strings.Contains(got.Content, "final chunk") {
		t.Fatalf("reasoning outbound leaked final content chunk: %q", got.Content)
	}

	ctx, cancel = context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if got, ok := mb.SubscribeOutbound(ctx); ok {
		t.Fatalf("unexpected duplicate outbound after reasoning bubble: %+v", got)
	}
}

func TestHandleAgentEvent_ReasoningOffSuppressesThinking(t *testing.T) {
	mb := bus.New()
	mgr := NewManager(mb)
	mgr.RegisterChannel("test", &chatBehaviorTestChannel{name: "test"})
	delivery := ResolveReasoningDelivery(ReasoningDeliveryOff, nil)
	mgr.RegisterRunWithBehavior("run-1", "test", "chat-1", "msg-1", nil, uuid.Nil, false, false, true, ResolvedChatBehavior{}, delivery)

	mgr.HandleAgentEvent(protocol.ChatEventThinking, "run-1", map[string]string{"content": "hidden"})
	mgr.HandleAgentEvent(protocol.AgentEventRunCompleted, "run-1", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if got, ok := mb.SubscribeOutbound(ctx); ok {
		t.Fatalf("reasoning off emitted outbound message: %+v", got)
	}
}

func TestHandleAgentEvent_AlwaysBubblesUsesAnswerStreamWhenChannelStreaming(t *testing.T) {
	mb := bus.New()
	mgr := NewManager(mb)
	ch := &reasoningStreamingTestChannel{name: "test", reasoningEnabled: true}
	mgr.RegisterChannel("test", ch)
	delivery := ResolveReasoningDelivery(ReasoningDeliveryAlwaysBubbles, nil)
	mgr.RegisterRunWithBehavior("run-1", "test", "chat-1", "msg-1", nil, uuid.Nil, true, false, true, ResolvedChatBehavior{}, delivery)

	mgr.HandleAgentEvent(protocol.AgentEventRunStarted, "run-1", nil)
	mgr.HandleAgentEvent(protocol.ChatEventThinking, "run-1", map[string]string{"content": "native reasoning"})
	mgr.HandleAgentEvent(protocol.ChatEventChunk, "run-1", map[string]string{"content": "answer"})

	if len(ch.streams) != 1 {
		t.Fatalf("streams = %d, want answer stream only", len(ch.streams))
	}
	if ch.streams[0].firstStream {
		t.Fatal("always_bubbles created a reasoning stream instead of answer stream")
	}
	if got := strings.Join(ch.streams[0].updates, " "); got != "answer" {
		t.Fatalf("stream updates = %q, want answer only", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, ok := mb.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected reasoning bubble outbound message")
	}
	if !strings.Contains(got.Content, "native reasoning") {
		t.Fatalf("reasoning bubble content = %q", got.Content)
	}
}

func TestHandleAgentEvent_ReasoningOffStripsThinkTagsFromStreamingAnswer(t *testing.T) {
	mb := bus.New()
	mgr := NewManager(mb)
	ch := &reasoningStreamingTestChannel{name: "test", reasoningEnabled: true}
	mgr.RegisterChannel("test", ch)
	delivery := ResolveReasoningDelivery(ReasoningDeliveryOff, nil)
	mgr.RegisterRunWithBehavior("run-1", "test", "chat-1", "msg-1", nil, uuid.Nil, true, false, true, ResolvedChatBehavior{}, delivery)

	mgr.HandleAgentEvent(protocol.AgentEventRunStarted, "run-1", nil)
	mgr.HandleAgentEvent(protocol.ChatEventChunk, "run-1", map[string]string{"content": "<think>hidden</think>visible"})

	if len(ch.streams) != 1 {
		t.Fatalf("streams = %d, want answer stream only", len(ch.streams))
	}
	if ch.streams[0].firstStream {
		t.Fatal("reasoning off created a reasoning stream")
	}
	if got := strings.Join(ch.streams[0].updates, " "); got != "visible" {
		t.Fatalf("stream updates = %q, want stripped answer", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if got, ok := mb.SubscribeOutbound(ctx); ok {
		t.Fatalf("reasoning off emitted outbound message: %+v", got)
	}
}

func TestHandleAgentEvent_AlwaysBubblesExtractsThinkTagsFromStreamingChunk(t *testing.T) {
	mb := bus.New()
	mgr := NewManager(mb)
	ch := &reasoningStreamingTestChannel{name: "test", reasoningEnabled: true}
	mgr.RegisterChannel("test", ch)
	delivery := ResolveReasoningDelivery(ReasoningDeliveryAlwaysBubbles, nil)
	mgr.RegisterRunWithBehavior("run-1", "test", "chat-1", "msg-1", nil, uuid.Nil, true, false, true, ResolvedChatBehavior{}, delivery)

	mgr.HandleAgentEvent(protocol.AgentEventRunStarted, "run-1", nil)
	mgr.HandleAgentEvent(protocol.ChatEventChunk, "run-1", map[string]string{"content": "<think>tagged reasoning</think>tagged answer"})

	if len(ch.streams) != 1 {
		t.Fatalf("streams = %d, want answer stream only", len(ch.streams))
	}
	if got := strings.Join(ch.streams[0].updates, " "); got != "tagged answer" {
		t.Fatalf("stream updates = %q, want stripped answer", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, ok := mb.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected think-tag reasoning bubble")
	}
	if !strings.Contains(got.Content, "tagged reasoning") {
		t.Fatalf("reasoning bubble content = %q", got.Content)
	}
}

type reasoningStreamingTestChannel struct {
	chatBehaviorTestChannel
	name             string
	reasoningEnabled bool
	streams          []*reasoningRecordingStream
}

func (c *reasoningStreamingTestChannel) Name() string                 { return c.name }
func (c *reasoningStreamingTestChannel) Type() string                 { return c.name }
func (c *reasoningStreamingTestChannel) StreamEnabled(bool) bool      { return true }
func (c *reasoningStreamingTestChannel) ReasoningStreamEnabled() bool { return c.reasoningEnabled }
func (c *reasoningStreamingTestChannel) FinalizeStream(context.Context, string, ChannelStream) {
}
func (c *reasoningStreamingTestChannel) CreateStream(_ context.Context, _ string, firstStream bool) (ChannelStream, error) {
	stream := &reasoningRecordingStream{firstStream: firstStream}
	c.streams = append(c.streams, stream)
	return stream, nil
}

type reasoningRecordingStream struct {
	firstStream bool
	updates     []string
	stopped     bool
}

func (s *reasoningRecordingStream) Update(_ context.Context, text string) {
	s.updates = append(s.updates, text)
}

func (s *reasoningRecordingStream) Stop(context.Context) error {
	s.stopped = true
	return nil
}

func (s *reasoningRecordingStream) MessageID() int { return 1 }
