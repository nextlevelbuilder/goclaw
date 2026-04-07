package http

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

type agentLookupCapture struct {
	called   bool
	agentKey string
	tenantID uuid.UUID
}

func newCaptureRouter(capture *agentLookupCapture) *agent.Router {
	router := agent.NewRouter()
	router.SetResolver(func(ctx context.Context, agentKey string) (agent.Agent, error) {
		capture.called = true
		capture.agentKey = agentKey
		capture.tenantID = store.TenantIDFromContext(ctx)
		return nil, fmt.Errorf("agent not found: %s", agentKey)
	})
	return router
}

type toolContextCapture struct {
	called   bool
	tenantID uuid.UUID
}

type captureTool struct {
	capture *toolContextCapture
}

func (t *captureTool) Name() string {
	return "capture_tool"
}

func (t *captureTool) Description() string {
	return "captures tenant context for tests"
}

func (t *captureTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *captureTool) Execute(ctx context.Context, _ map[string]any) *tools.Result {
	t.capture.called = true
	t.capture.tenantID = store.TenantIDFromContext(ctx)
	return tools.NewResult("ok")
}

type wakeRunCapture struct {
	ctxUserID string
	reqUserID string
	tenantID  uuid.UUID
}

type fakeAgent struct {
	runCapture *wakeRunCapture
}

func (a *fakeAgent) ID() string {
	return "test-agent"
}

func (a *fakeAgent) Run(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error) {
	a.runCapture.ctxUserID = store.UserIDFromContext(ctx)
	a.runCapture.reqUserID = req.UserID
	a.runCapture.tenantID = store.TenantIDFromContext(ctx)
	return &agent.RunResult{Content: "ok", RunID: "run-1"}, nil
}

func (a *fakeAgent) IsRunning() bool {
	return false
}

func (a *fakeAgent) Model() string {
	return "test-model"
}

func (a *fakeAgent) ProviderName() string {
	return "test-provider"
}

func (a *fakeAgent) Provider() providers.Provider {
	return nil
}

func TestInlineAuthHandlersInjectTenantContext(t *testing.T) {
	tenantID := uuid.New()
	ts := newMockTenantStore()
	ts.addTenant(tenantID, "acme")
	ts.setUserRole(tenantID, "user-1", store.TenantRoleAdmin)
	setupTestTenantStore(t, ts)
	setupTestToken(t, "gateway-token")
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey("tenant-api-key"): {
			ID:       uuid.New(),
			TenantID: tenantID,
			Scopes:   []string{"operator.write"},
		},
	})

	tests := []struct {
		name       string
		serve      func(*agentLookupCapture, *httptest.ResponseRecorder)
		wantStatus int
	}{
		{
			name: "chat completions with gateway tenant hint",
			serve: func(capture *agentLookupCapture, rec *httptest.ResponseRecorder) {
				req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"agent:test-agent","messages":[{"role":"user","content":"hello"}]}`))
				req.Header.Set("Authorization", "Bearer gateway-token")
				req.Header.Set("X-GoClaw-User-Id", "user-1")
				req.Header.Set("X-GoClaw-Tenant-Id", "acme")
				NewChatCompletionsHandler(newCaptureRouter(capture), nil, false).ServeHTTP(rec, req)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "responses with tenant-bound api key",
			serve: func(capture *agentLookupCapture, rec *httptest.ResponseRecorder) {
				req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"agent:test-agent","messages":[{"role":"user","content":"hello"}]}`))
				req.Header.Set("Authorization", "Bearer tenant-api-key")
				NewResponsesHandler(newCaptureRouter(capture), nil).ServeHTTP(rec, req)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "wake with gateway tenant hint",
			serve: func(capture *agentLookupCapture, rec *httptest.ResponseRecorder) {
				req := httptest.NewRequest(http.MethodPost, "/v1/agents/test-agent/wake", bytes.NewBufferString(`{"message":"hello"}`))
				req.Header.Set("Authorization", "Bearer gateway-token")
				req.Header.Set("X-GoClaw-User-Id", "user-1")
				req.Header.Set("X-GoClaw-Tenant-Id", "acme")
				mux := http.NewServeMux()
				NewWakeHandler(newCaptureRouter(capture)).RegisterRoutes(mux)
				mux.ServeHTTP(rec, req)
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture := &agentLookupCapture{}
			rec := httptest.NewRecorder()
			tt.serve(capture, rec)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if !capture.called {
				t.Fatal("expected agent lookup")
			}
			if capture.agentKey != "test-agent" {
				t.Fatalf("agentKey = %q, want test-agent", capture.agentKey)
			}
			if capture.tenantID != tenantID {
				t.Fatalf("tenantID = %v, want %v", capture.tenantID, tenantID)
			}
		})
	}
}

func TestToolsInvokeHandlerInjectsTenantContext(t *testing.T) {
	tenantID := uuid.New()
	ts := newMockTenantStore()
	ts.addTenant(tenantID, "acme")
	ts.setUserRole(tenantID, "user-1", store.TenantRoleAdmin)
	setupTestTenantStore(t, ts)
	setupTestToken(t, "gateway-token")

	capture := &toolContextCapture{}
	registry := tools.NewRegistry()
	registry.Register(&captureTool{capture: capture})

	req := httptest.NewRequest(http.MethodPost, "/v1/tools/invoke", bytes.NewBufferString(`{"tool":"capture_tool","args":{}}`))
	req.Header.Set("Authorization", "Bearer gateway-token")
	req.Header.Set("X-GoClaw-User-Id", "user-1")
	req.Header.Set("X-GoClaw-Tenant-Id", "acme")

	rec := httptest.NewRecorder()
	NewToolsInvokeHandler(registry, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !capture.called {
		t.Fatal("expected tool execution")
	}
	if capture.tenantID != tenantID {
		t.Fatalf("tenantID = %v, want %v", capture.tenantID, tenantID)
	}
}

func TestWakeHandlerBlocksBodyUserOverrideForBoundOwner(t *testing.T) {
	tenantID := uuid.New()
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey("owner-key"): {
			ID:       uuid.New(),
			TenantID: tenantID,
			OwnerID:  "owner-1",
			Scopes:   []string{"operator.write"},
		},
	})

	runCapture := &wakeRunCapture{}
	router := agent.NewRouter()
	router.SetResolver(func(context.Context, string) (agent.Agent, error) {
		return &fakeAgent{runCapture: runCapture}, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/agents/test-agent/wake", bytes.NewBufferString(`{"message":"hello","user_id":"spoofed-user"}`))
	req.Header.Set("Authorization", "Bearer owner-key")

	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	NewWakeHandler(router).RegisterRoutes(mux)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if runCapture.ctxUserID != "owner-1" {
		t.Fatalf("ctx userID = %q, want %q", runCapture.ctxUserID, "owner-1")
	}
	if runCapture.reqUserID != "owner-1" {
		t.Fatalf("run request userID = %q, want %q", runCapture.reqUserID, "owner-1")
	}
}

func TestWakeHandlerUsesTenantScopedCacheEntry(t *testing.T) {
	tenantID := uuid.New()
	ts := newMockTenantStore()
	ts.addTenant(tenantID, "acme")
	ts.setUserRole(tenantID, "user-1", store.TenantRoleAdmin)
	setupTestTenantStore(t, ts)
	setupTestToken(t, "gateway-token")

	runCapture := &wakeRunCapture{}
	resolverCalls := 0
	router := agent.NewRouter()
	router.SetResolver(func(ctx context.Context, agentKey string) (agent.Agent, error) {
		resolverCalls++
		if agentKey != "test-agent" {
			return nil, fmt.Errorf("unexpected agent key: %s", agentKey)
		}
		if store.TenantIDFromContext(ctx) != tenantID {
			return nil, fmt.Errorf("unexpected tenant scope: %v", store.TenantIDFromContext(ctx))
		}
		return &fakeAgent{runCapture: runCapture}, nil
	})

	if _, err := router.Get(store.WithTenantID(context.Background(), tenantID), "test-agent"); err != nil {
		t.Fatalf("prime cache: %v", err)
	}
	if resolverCalls != 1 {
		t.Fatalf("resolverCalls after priming = %d, want 1", resolverCalls)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/agents/test-agent/wake", bytes.NewBufferString(`{"message":"hello"}`))
	req.Header.Set("Authorization", "Bearer gateway-token")
	req.Header.Set("X-GoClaw-User-Id", "user-1")
	req.Header.Set("X-GoClaw-Tenant-Id", "acme")

	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	NewWakeHandler(router).RegisterRoutes(mux)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if resolverCalls != 1 {
		t.Fatalf("resolverCalls after request = %d, want cache hit with no extra resolve", resolverCalls)
	}
	if runCapture.tenantID != tenantID {
		t.Fatalf("run tenantID = %v, want %v", runCapture.tenantID, tenantID)
	}
}
