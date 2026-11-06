package gateway

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// stubAgentKeyStore satisfies store.AgentStore via an embedded (nil) interface and
// implements only GetByIDUnscoped — the single method bridgeContextMiddleware calls.
type stubAgentKeyStore struct {
	store.AgentStore
	ag  *store.AgentData
	err error
}

func (s *stubAgentKeyStore) GetByIDUnscoped(context.Context, uuid.UUID) (*store.AgentData, error) {
	return s.ag, s.err
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

func TestBridgeContextMiddleware_InjectsSignedDelegationArtifactContext(t *testing.T) {
	const (
		gatewayToken = "test-gateway-token"
		delegationID = "delegation-123"
		inputsRoot   = "/runtime/delegations/delegation-123/inputs"
	)
	agentID := uuid.New()
	var gotDelegationID, gotInputs string
	var artifactRun bool
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotDelegationID = tools.DelegationIDFromCtx(r.Context())
		gotInputs = tools.DelegationArtifactInputsFromCtx(r.Context())
		artifactRun = tools.IsDelegationArtifactRun(r.Context())
	})
	mw := bridgeContextMiddleware(gatewayToken, nil, next)

	sig := providers.SignBridgeContext(
		gatewayToken,
		agentID.String(),
		"",
		"",
		"",
		"",
		"/runtime/delegations/delegation-123/outputs",
		"",
		"",
		"",
		delegationID,
		inputsRoot,
	)
	req := httptest.NewRequest(http.MethodPost, "/mcp/bridge", nil)
	req.Header.Set("X-Agent-ID", agentID.String())
	req.Header.Set("X-Workspace", "/runtime/delegations/delegation-123/outputs")
	req.Header.Set("X-Delegation-ID", delegationID)
	req.Header.Set("X-Delegation-Inputs", inputsRoot)
	req.Header.Set("X-Bridge-Sig", sig)

	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !artifactRun {
		t.Fatalf("status=%d artifactRun=%v body=%s", rec.Code, artifactRun, rec.Body.String())
	}
	if gotDelegationID != delegationID || gotInputs != inputsRoot {
		t.Fatalf("delegation context = (%q, %q)", gotDelegationID, gotInputs)
	}
}

func TestBridgeContextMiddleware_RejectsLegacySignatureWithDelegationHeaders(t *testing.T) {
	const gatewayToken = "test-gateway-token"
	agentID := uuid.New()
	legacySig := providers.SignBridgeContext(
		gatewayToken,
		agentID.String(),
		"",
		"",
		"",
		"",
		"/runtime/outputs",
		"",
		"",
		"",
	)
	req := httptest.NewRequest(http.MethodPost, "/mcp/bridge", nil)
	req.Header.Set("X-Agent-ID", agentID.String())
	req.Header.Set("X-Workspace", "/runtime/outputs")
	req.Header.Set("X-Delegation-ID", "delegation-123")
	req.Header.Set("X-Delegation-Inputs", "/runtime/inputs")
	req.Header.Set("X-Bridge-Sig", legacySig)

	var called bool
	mw := bridgeContextMiddleware(gatewayToken, nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden || called {
		t.Fatalf("status=%d called=%v, want forbidden", rec.Code, called)
	}
}

// TestBridgeContextMiddleware_InjectsAgentBudget guards the media tools on claude_cli
// agents: read_image/read_audio/read_video refuse to run with an AgentBudgetWiringError
// unless the context carries the agent's context window and max_tokens. Unset (0)
// columns must resolve to the same defaults the agent loop uses.
func TestBridgeContextMiddleware_InjectsAgentBudget(t *testing.T) {
	const gatewayToken = "test-gateway-token"
	tests := []struct {
		name       string
		agentID    string // empty means a fresh valid UUID
		ag         *store.AgentData
		err        error
		wantWindow int
		wantMax    int
	}{
		{"explicit", "", &store.AgentData{AgentKey: "a", ContextWindow: 120000, MaxTokens: 4096}, nil, 120000, 4096},
		{"unset falls back to defaults", "", &store.AgentData{AgentKey: "a"}, nil, config.DefaultContextWindow, config.DefaultMaxTokens},
		{"missing row falls back to defaults", "", nil, nil, config.DefaultContextWindow, config.DefaultMaxTokens},
		{"lookup error falls back to defaults", "", nil, sql.ErrNoRows, config.DefaultContextWindow, config.DefaultMaxTokens},
		{"non-UUID agent id keeps defaults", "not-a-uuid", nil, nil, config.DefaultContextWindow, config.DefaultMaxTokens},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentID := tt.agentID
			if agentID == "" {
				agentID = uuid.NewString()
			}
			var gotWindow, gotMax int
			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				gotWindow = store.AgentContextWindowFromContext(r.Context())
				gotMax = store.AgentMaxTokensFromContext(r.Context())
			})
			mw := bridgeContextMiddleware(gatewayToken, &stubAgentKeyStore{ag: tt.ag, err: tt.err}, next)

			sig := providers.SignBridgeContext(gatewayToken, agentID, "", "", "", "", "", "", "", "")
			req := httptest.NewRequest(http.MethodPost, "/mcp/bridge", nil)
			req.Header.Set("X-Agent-ID", agentID)
			req.Header.Set("X-Bridge-Sig", sig)
			mw.ServeHTTP(httptest.NewRecorder(), req)

			if gotWindow != tt.wantWindow {
				t.Errorf("context window = %d, want %d", gotWindow, tt.wantWindow)
			}
			if gotMax != tt.wantMax {
				t.Errorf("max tokens = %d, want %d", gotMax, tt.wantMax)
			}
		})
	}
}
