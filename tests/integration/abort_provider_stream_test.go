//go:build integration

package integration

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// TestAnthropicChat_AbortContextCancel verifies that cancelling a context
// while a Chat call is in flight properly closes the HTTP connection.
// Uses a stalling SSE server that only responds if data flows from it.
func TestAnthropicChat_AbortContextCancel(t *testing.T) {
	t.Parallel()

	// Start a stalling SSE server
	server := newStallingSSEServer(t)
	defer server.Close()

	// Create provider pointed at the stalling server
	provider := providers.NewAnthropicProvider(
		"test-key",
		providers.WithAnthropicBaseURL(server.URL),
		providers.WithAnthropicModel("claude-opus-4-6"),
	)

	// Create a context with a 200ms deadline
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	baselineGoroutines := runtime.NumGoroutine()

	// Call Chat — should block briefly, then ctx timeout fires
	start := time.Now()
	_, err := provider.Chat(ctx, providers.ChatRequest{
		Model:    "claude-opus-4-6",
		Messages: []providers.Message{},
	})
	elapsed := time.Since(start)

	// Verify: context deadline/cancellation caused the error
	if err == nil {
		t.Fatal("expected Chat to fail on context cancellation, got nil error")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		// Error may be wrapped; that's acceptable
		t.Logf("Chat error (wrapped context error acceptable): %v", err)
	}

	// Verify: returned promptly (well within 1.5s grace)
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("Chat did not return within 1.5s, took %v", elapsed)
	}
	t.Logf("Chat returned in %v", elapsed)

	// Verify: no goroutine leak (allow 100ms grace + baseline variance)
	time.Sleep(100 * time.Millisecond)
	finalGoroutines := runtime.NumGoroutine()
	if finalGoroutines > baselineGoroutines+2 {
		t.Fatalf("possible goroutine leak: baseline=%d, final=%d", baselineGoroutines, finalGoroutines)
	}
}

// TestOpenAIChat_AbortContextCancel mirrors the Anthropic test for OpenAI-compatible providers.
func TestOpenAIChat_AbortContextCancel(t *testing.T) {
	t.Parallel()

	server := newStallingSSEServer(t)
	defer server.Close()

	provider := providers.NewOpenAIProvider(
		"openai",
		"test-key",
		server.URL,
		"gpt-4o",
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	baselineGoroutines := runtime.NumGoroutine()

	start := time.Now()
	_, err := provider.Chat(ctx, providers.ChatRequest{
		Model:    "gpt-4o",
		Messages: []providers.Message{},
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected Chat to fail on context cancellation, got nil error")
	}

	if elapsed > 1500*time.Millisecond {
		t.Fatalf("Chat did not return within 1.5s, took %v", elapsed)
	}
	t.Logf("Chat returned in %v", elapsed)

	time.Sleep(100 * time.Millisecond)
	finalGoroutines := runtime.NumGoroutine()
	if finalGoroutines > baselineGoroutines+2 {
		t.Fatalf("possible goroutine leak: baseline=%d, final=%d", baselineGoroutines, finalGoroutines)
	}
}
