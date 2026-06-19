package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// stubAgentKeyStore satisfies store.AgentStore via an embedded (nil) interface and
// implements only GetByIDUnscoped — the single method bridgeContextMiddleware calls.
type stubAgentKeyStore struct {
	store.AgentStore
	ag *store.AgentData
}

func (s *stubAgentKeyStore) GetByIDUnscoped(context.Context, uuid.UUID) (*store.AgentData, error) {
	return s.ag, nil
}

// TestBridgeContextMiddleware_InjectsAgentKey guards the MCP bridge identity path:
// a signed X-Agent-ID must put the agent key into the tool context
// (tools.ToolAgentKeyFromCtx). Session tools (sessions_list/history/send) resolve the
// caller via that key and otherwise fail with "agent context required".
func TestBridgeContextMiddleware_InjectsAgentKey(t *testing.T) {
	const (
		gatewayToken = "test-gateway-token"
		wantKey      = "vault-keeper"
	)
	agentID := uuid.New()
	agentStore := &stubAgentKeyStore{ag: &store.AgentData{AgentKey: wantKey}}

	var handlerCalled bool
	var gotKey string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		gotKey = tools.ToolAgentKeyFromCtx(r.Context())
	})

	mw := bridgeContextMiddleware(gatewayToken, agentStore, next)

	// Sign X-Agent-ID exactly as the claude-cli provider does: empty user/channel/
	// chat/peer/workspace/tenant plus the two trailing extras (localKey, sessionKey).
	sig := providers.SignBridgeContext(gatewayToken, agentID.String(), "", "", "", "", "", "", "", "")
	req := httptest.NewRequest(http.MethodPost, "/mcp/bridge", nil)
	req.Header.Set("X-Agent-ID", agentID.String())
	req.Header.Set("X-Bridge-Sig", sig)

	mw.ServeHTTP(httptest.NewRecorder(), req)

	if !handlerCalled {
		t.Fatal("next handler was not called: middleware rejected the signed request")
	}
	if gotKey != wantKey {
		t.Errorf("ToolAgentKeyFromCtx = %q, want %q", gotKey, wantKey)
	}
}

// TestBridgeContextMiddleware_NoStore_NoAgentKey is the negative control: with no
// agent store wired (or an unsigned request), the agent key must stay empty so the
// regression that motivated this fix cannot silently reappear masked by a default.
func TestBridgeContextMiddleware_NoStore_NoAgentKey(t *testing.T) {
	const gatewayToken = "test-gateway-token"
	agentID := uuid.New()

	var gotKey string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotKey = tools.ToolAgentKeyFromCtx(r.Context())
	})

	// agentStore == nil: middleware injects the UUID but has no row to recover the key from.
	mw := bridgeContextMiddleware(gatewayToken, nil, next)

	sig := providers.SignBridgeContext(gatewayToken, agentID.String(), "", "", "", "", "", "", "", "")
	req := httptest.NewRequest(http.MethodPost, "/mcp/bridge", nil)
	req.Header.Set("X-Agent-ID", agentID.String())
	req.Header.Set("X-Bridge-Sig", sig)

	mw.ServeHTTP(httptest.NewRecorder(), req)

	if gotKey != "" {
		t.Errorf("ToolAgentKeyFromCtx = %q, want empty when no agent store is wired", gotKey)
	}
}
