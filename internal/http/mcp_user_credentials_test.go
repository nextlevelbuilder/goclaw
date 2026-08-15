package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// countMCPCacheInvalidate counts CacheKindMCP cache-invalidate events.
func countMCPCacheInvalidate(events []bus.Event) int {
	n := 0
	for _, e := range events {
		if e.Name != protocol.EventCacheInvalidate {
			continue
		}
		if p, ok := e.Payload.(bus.CacheInvalidatePayload); ok && p.Kind == bus.CacheKindMCP {
			n++
		}
	}
	return n
}

// Saving and deleting per-user MCP credentials must broadcast a CacheKindMCP
// cache-invalidate so pooled per-user connections are evicted and the new/removed
// credentials take effect immediately (instead of waiting for idle TTL).
func TestMCPUserCredentialsEmitCacheInvalidateOnSetAndDelete(t *testing.T) {
	srvStore := newMockMCPServerForOAuth()
	mb := bus.New()

	var mu sync.Mutex
	var events []bus.Event
	mb.Subscribe("test-capture", func(e bus.Event) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})

	h := NewMCPUserCredentialsHandler(srvStore, nil, mb)
	serverID := uuid.New()
	ctxWith := func(r *http.Request) *http.Request {
		ctx := store.WithTenantID(store.WithUserID(r.Context(), "user-1"), uuid.New())
		return r.WithContext(ctx)
	}

	// SET credentials.
	setReq := httptest.NewRequest(http.MethodPut,
		"/v1/mcp/servers/"+serverID.String()+"/user-credentials",
		strings.NewReader(`{"api_key":"k"}`))
	setReq.SetPathValue("id", serverID.String())
	setReq = ctxWith(setReq)
	setRec := httptest.NewRecorder()
	h.handleSet(setRec, setReq)
	if setRec.Code != http.StatusOK {
		t.Fatalf("handleSet status = %d, want 200; body: %s", setRec.Code, setRec.Body.String())
	}

	// DELETE credentials.
	delReq := httptest.NewRequest(http.MethodDelete,
		"/v1/mcp/servers/"+serverID.String()+"/user-credentials", nil)
	delReq.SetPathValue("id", serverID.String())
	delReq = ctxWith(delReq)
	delRec := httptest.NewRecorder()
	h.handleDelete(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("handleDelete status = %d, want 200; body: %s", delRec.Code, delRec.Body.String())
	}

	mu.Lock()
	got := countMCPCacheInvalidate(events)
	mu.Unlock()
	if got < 2 {
		t.Errorf("expected >=2 CacheKindMCP events (set + delete), got %d", got)
	}
}
