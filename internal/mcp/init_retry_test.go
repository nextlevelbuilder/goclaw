package mcp

import (
	"context"
	"testing"
	"time"
)

func TestConnectAndDiscoverRetriesOnStdioInitFailure(t *testing.T) {
	// "cat" starts instantly but doesn't speak JSON-RPC → Initialize fails every time.
	// The retry loop should add observable backoff delay before exhausting attempts.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	_, _, err := connectAndDiscover(ctx, "test-retry", "stdio",
		"cat", nil, nil, "", nil, 2)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error — cat doesn't speak MCP")
	}

	// First retry waits 2s → elapsed must be at least 2s proving retry happened.
	if elapsed < 2*time.Second {
		t.Errorf("elapsed %v — expected ≥2s proving retries happened", elapsed)
	}
	t.Logf("elapsed: %v (retries confirmed)", elapsed)
}

func TestConnectAndDiscoverNoRetryForSSE(t *testing.T) {
	// SSE/HTTP transports should fail immediately without retrying.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	_, _, err := connectAndDiscover(ctx, "test-no-retry", "sse",
		"", nil, nil, "http://127.0.0.1:1", nil, 10)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error for bad SSE URL")
	}

	// SSE should fail fast (no retries) — well under 2 seconds.
	if elapsed > 2*time.Second {
		t.Errorf("elapsed %v — SSE should fail fast without retries", elapsed)
	}
}
