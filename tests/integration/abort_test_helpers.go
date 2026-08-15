//go:build integration

package integration

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

var errSimulatedFailure = errors.New("simulated database failure")

// newStallingSSEServer returns an httptest server that sends SSE headers
// then blocks indefinitely, simulating an unresponsive LLM endpoint.
func newStallingSSEServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Block until client closes connection.
		<-r.Context().Done()
	}))
}

// newStallingHTTPServer returns an httptest server that accepts connections
// but never sends any bytes, simulating a server that hangs before headers.
// Useful for testing ResponseHeaderTimeout.
func newStallingHTTPServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block forever without sending headers
		<-r.Context().Done()
	}))
}

// flakyTracingStore wraps a real TracingStore and fails the first N UpdateTrace
// calls, then succeeds thereafter. Atomic counter allows concurrent test calls.
type flakyTracingStore struct {
	store.TracingStore
	failsRemaining int32
}

// NewFlakyTracingStore creates a wrapper that fails N times then succeeds.
func newFlakyTracingStore(base store.TracingStore, failCount int) *flakyTracingStore {
	return &flakyTracingStore{
		TracingStore:   base,
		failsRemaining: int32(failCount),
	}
}

// UpdateTrace fails the first failCount calls, then delegates to the wrapped store.
func (s *flakyTracingStore) UpdateTrace(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	fails := atomic.AddInt32(&s.failsRemaining, -1)
	if fails >= 0 {
		return errSimulatedFailure
	}
	return s.TracingStore.UpdateTrace(ctx, id, updates)
}

// captureBusEvent subscribes to a message bus and forwards events matching
// the given name into a channel. Automatically unsubscribes on test cleanup.
func captureBusEvent(t *testing.T, msgBus *bus.MessageBus, eventName string) <-chan bus.Event {
	t.Helper()
	captured := make(chan bus.Event, 10)
	subID := "test-sub-" + uuid.New().String()[:8]

	msgBus.Subscribe(subID, func(e bus.Event) {
		if e.Name == eventName {
			select {
			case captured <- e:
			default:
				// channel full, drop (shouldn't happen in tests)
			}
		}
	})

	t.Cleanup(func() {
		msgBus.Unsubscribe(subID)
		close(captured)
	})

	return captured
}

// mockTraceCollector implements agent.TraceCollector for testing.
type mockTraceCollector struct {
	finishCalls []struct {
		TraceID       uuid.UUID
		TenantID      uuid.UUID // tenant extracted from ctx at call time
		Status        string
		ErrMsg        string
		OutputPreview string
	}
}

// FinishTrace records the call for later assertion.
func (m *mockTraceCollector) FinishTrace(ctx context.Context, traceID uuid.UUID, status, errMsg, outputPreview string) {
	m.finishCalls = append(m.finishCalls, struct {
		TraceID       uuid.UUID
		TenantID      uuid.UUID
		Status        string
		ErrMsg        string
		OutputPreview string
	}{traceID, store.TenantIDFromContext(ctx), status, errMsg, outputPreview})
}

// LastFinishTrace returns the most recent FinishTrace call, or nil if none.
func (m *mockTraceCollector) LastFinishTrace() *struct {
	TraceID       uuid.UUID
	TenantID      uuid.UUID
	Status        string
	ErrMsg        string
	OutputPreview string
} {
	if len(m.finishCalls) == 0 {
		return nil
	}
	return &m.finishCalls[len(m.finishCalls)-1]
}

// FinishCallCount returns the number of FinishTrace calls.
func (m *mockTraceCollector) FinishCallCount() int {
	return len(m.finishCalls)
}
