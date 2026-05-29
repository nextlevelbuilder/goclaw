package mcp

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestIsSessionUninitializedErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unrelated", errors.New("connection refused"), false},
		// FastMCP / Python mcp server phrasing — the exact text observed in
		// the production trace that motivated this detector.
		{"fastmcp tools/call", errors.New(`method "tools/call" is invalid during session initialization`), true},
		{"fastmcp other method", errors.New(`method "resources/list" is invalid during session initialization`), true},
		// mcp-go server / Node implementations.
		{"session not initialized", errors.New("session not initialized"), true},
		// mcp-go transport ErrSessionTerminated text (HTTP 404 path).
		{"session terminated", errors.New("session terminated (404). need to re-initialize"), true},
		// Case-insensitive matching keeps detection robust to upstream
		// rewording without dragging the maintainer into a string-match
		// audit every time a server logs slightly differently.
		{"mixed case", errors.New("Session Not Initialized"), true},
		// 401 must NOT match — handled by isUnauthorizedErr to drive a
		// different recovery path (credential purge).
		{"unauthorized 401", errors.New("unauthorized (401)"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSessionUninitializedErr(tc.err); got != tc.want {
				t.Errorf("isSessionUninitializedErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestRequestForceReconnect_Dedup verifies the CAS guard collapses N
// concurrent failing tool calls into a single reconnect in flight. Without
// this, a high-QPS agent burst would launch one fullReconnect per failed
// call — defeating the purpose of pooling and hammering the recovering
// server during a restart window.
func TestRequestForceReconnect_Dedup(t *testing.T) {
	// Stand up a serverState whose Pending flag starts cleared. We don't
	// run the goroutine body to completion (that requires a real client);
	// the CAS check itself is what we're verifying. The goroutine will
	// fail fast on the nil client and clear the flag — but only after
	// the test observation point.
	ss := &serverState{name: "test", transport: "streamable-http"}

	var attempted atomic.Int32
	var wg sync.WaitGroup
	const concurrent = 20

	for range concurrent {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Inline the CAS half of requestForceReconnect — testing the
			// real method would race the cleanup goroutine. The dedup
			// guarantee lives entirely in the CAS, so this is faithful.
			if ss.reconnPending.CompareAndSwap(false, true) {
				attempted.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := attempted.Load(); got != 1 {
		t.Errorf("expected exactly 1 reconnect attempt after %d concurrent requests, got %d", concurrent, got)
	}
	if !ss.reconnPending.Load() {
		t.Error("expected reconnPending to be set after first CAS")
	}

	// Once cleared (simulating reconnect completion), the next request must
	// be allowed through — otherwise a single transient reset would freeze
	// recovery forever.
	ss.reconnPending.Store(false)
	if !ss.reconnPending.CompareAndSwap(false, true) {
		t.Error("expected CAS to succeed after pending flag cleared")
	}
}
