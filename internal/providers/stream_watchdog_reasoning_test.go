package providers

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/reliability"
)

// ---------------------------------------------------------------------------
// Case E — long reasoning must NOT trip the stream watchdog.
//
// A model deep in extended thinking emits a reasoning (thinking_delta) event
// every ~200ms for 3 seconds before its first text token. The idle watchdog is
// armed at 700ms: each reasoning event re-arms it, so a correctly wired provider
// never reports a stall. This regression guards the watchReset-per-event
// behavior in the provider stream loops — a broken reset that only armed on
// text deltas would fire at ~700ms and abort a healthy long-reasoning stream.
// ---------------------------------------------------------------------------

// TestWatchdog_ThinkingDeltas_NoFalseStall drives an OpenAI-compat provider
// against a fake SSE server that streams reasoning deltas (below the idle
// window) for ~3s, then a text chunk + [DONE]. It asserts the stream completes
// with the final text token and the stall counter stays at zero.
func TestWatchdog_ThinkingDeltas_NoFalseStall(t *testing.T) {
	server := newFakeLLMServerEmpty(t)

	// Reasoning deltas every ~200ms for ~3s, then the answer and a terminator.
	frames := make([]sseFrame, 0, 16)
	for i := 0; i < 14; i++ {
		frames = append(frames, server.openAIReasoningDelta("step "+string(rune('a'+i))))
	}
	frames = append(frames, server.openAITextDelta("final answer"))
	frames = append(frames, server.openAIStopDelta())
	server.script(responseStep{
		Status:      http.StatusOK,
		SSEFrames:   frames,
		SSEDone:     true,
		SSEFrameGap: 200 * time.Millisecond,
	})

	// The answer lands after ~2.8s of thinking, well past a naive idle window.

	p := NewOpenAIProvider("watchdog-reasoning", "test-key", server.URL(), "gpt-4o")
	p.retryConfig.Attempts = 1

	reg := reliability.Default()
	if reg == nil {
		t.Fatal("reliability.Default() returned nil")
	}
	before := reg.Metrics.Take().LLMStreamStalls

	// Idle window of 700ms: larger than the 200ms delta gap (keeps the stream
	// alive), far smaller than the 3s reasoning phase (a stale arm would fire).
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	ctx = WithStreamTimeouts(ctx, 700*time.Millisecond, 0)

	var chunks int
	start := time.Now()
	result, err := p.ChatStream(ctx, ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "think long"}},
	}, func(StreamChunk) { chunks++ })

	if err != nil {
		t.Fatalf("long-reasoning stream returned error (false stall?): %v", err)
	}
	if elapsed := time.Since(start); elapsed < 2500*time.Millisecond {
		t.Errorf("stream completed in %v, want to survive the full ~3s reasoning phase", elapsed)
	}
	if result == nil || result.Content != "final answer" {
		t.Errorf("result content = %+v, want the final text token delivered", result)
	}
	if chunks == 0 {
		t.Error("no chunks delivered on the reasoning stream")
	}
	after := reg.Metrics.Take().LLMStreamStalls
	if delta := after - before; delta != 0 {
		t.Errorf("LLMStreamStalls delta = %d, want 0 (reasoning deltas must not count as stalls)", delta)
	}
}

// Note: the inverse case — a stream with no events for longer than the idle
// window DOES fire — is already covered by TestStreamWatchdog_FiresOnIdle and
// TestOpenAIChatStream_WatchdogFires in stream_watchdog_test.go; it is not
// duplicated here.
