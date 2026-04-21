package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ---- stub: agent.Agent ----

// stubAgent implements agent.Agent for unit tests.
// Run behaviour is controlled by the runFn field.
type stubLLMAgent struct {
	id      string
	agentID uuid.UUID
	runFn   func(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error)
}

func (a *stubLLMAgent) ID() string            { return a.id }
func (a *stubLLMAgent) UUID() uuid.UUID        { return a.agentID }
func (a *stubLLMAgent) OtherConfig() json.RawMessage { return nil }
func (a *stubLLMAgent) Run(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error) {
	return a.runFn(ctx, req)
}
func (a *stubLLMAgent) IsRunning() bool            { return false }
func (a *stubLLMAgent) Model() string               { return "test-model" }
func (a *stubLLMAgent) ProviderName() string        { return "test" }
func (a *stubLLMAgent) Provider() providers.Provider { return nil }

// ---- stub: store.WebhookCallStore for LLM tests ----

// llmCallStore captures Create calls for assertion.
type llmCallStore struct {
	created []*store.WebhookCallData
	createErr error
}

func (s *llmCallStore) Create(_ context.Context, c *store.WebhookCallData) error {
	if s.createErr != nil {
		return s.createErr
	}
	cp := *c
	s.created = append(s.created, &cp)
	return nil
}
func (s *llmCallStore) GetByID(_ context.Context, _ uuid.UUID) (*store.WebhookCallData, error) {
	return nil, nil
}
func (s *llmCallStore) GetByIdempotency(_ context.Context, _ uuid.UUID, _ string) (*store.WebhookCallData, error) {
	return nil, nil
}
func (s *llmCallStore) UpdateStatus(_ context.Context, _ uuid.UUID, _ map[string]any) error {
	return nil
}
func (s *llmCallStore) UpdateStatusCAS(_ context.Context, _ uuid.UUID, _ string, _ map[string]any) error {
	return nil
}
func (s *llmCallStore) ClaimNext(_ context.Context, _ uuid.UUID, _ time.Time) (*store.WebhookCallData, error) {
	return nil, nil
}
func (s *llmCallStore) List(_ context.Context, _ store.WebhookCallListFilter) ([]store.WebhookCallData, error) {
	return nil, nil
}
func (s *llmCallStore) DeleteOlderThan(_ context.Context, _ uuid.UUID, _ time.Time) (int64, error) {
	return 0, nil
}
func (s *llmCallStore) ReclaimStale(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

// ---- helpers ----

// newTestLLMHandler builds a WebhookLLMHandler with no real agent router.
// The handler's handle() is invoked directly (bypassing RegisterRoutes auth middleware).
// agentRouter is nil — tests inject the webhook data into context directly.
func newTestLLMHandler(callStore *llmCallStore, webhookStore store.WebhookStore, lane *scheduler.Lane) *WebhookLLMHandler {
	if lane == nil {
		lane = scheduler.NewLane("webhook-test", 4)
	}
	return &WebhookLLMHandler{
		agentRouter: nil, // not used when tests inject via context
		callStore:   callStore,
		webhooks:    webhookStore,
		limiter:     NewWebhookLimiter(),
		lane:        lane,
	}
}

// buildLLMReq serializes a webhookLLMReq to an *http.Request body.
func buildLLMReq(t *testing.T, body any) *http.Request {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/webhooks/llm", bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	return r
}

// injectWebhook sets webhook + tenant in request context (simulates WebhookAuthMiddleware).
func injectWebhook(r *http.Request, wh *store.WebhookData) *http.Request {
	ctx := r.Context()
	ctx = WithWebhookData(ctx, wh)
	ctx = store.WithTenantID(ctx, wh.TenantID)
	if wh.AgentID != nil {
		ctx = store.WithAgentID(ctx, *wh.AgentID)
	}
	return r.WithContext(ctx)
}

// ---- tests for buildInput ----

func TestBuildInput_PlainString(t *testing.T) {
	raw, _ := json.Marshal("hello world")
	msg, extra, err := buildInput(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != "hello world" {
		t.Errorf("got msg=%q, want %q", msg, "hello world")
	}
	if extra != "" {
		t.Errorf("got extra=%q, want empty", extra)
	}
}

func TestBuildInput_MessageArray(t *testing.T) {
	msgs := []webhookInputMessage{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "What is 2+2?"},
		{Role: "assistant", Content: "4"},
	}
	raw, _ := json.Marshal(msgs)
	msg, extra, err := buildInput(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "4" from assistant is concatenated as user content (v1 simplification).
	if msg == "" {
		t.Error("expected non-empty user message from array input")
	}
	if extra == "" {
		t.Error("expected non-empty extraSystemPrompt from system role")
	}
}

func TestBuildInput_InvalidJSON(t *testing.T) {
	raw := json.RawMessage(`{invalid}`)
	_, _, err := buildInput(raw)
	if err == nil {
		t.Error("expected error for invalid input, got nil")
	}
}

func TestBuildInput_EmptyArray(t *testing.T) {
	raw, _ := json.Marshal([]webhookInputMessage{})
	msg, extra, err := buildInput(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg != "" || extra != "" {
		t.Errorf("expected empty result for empty array, got msg=%q extra=%q", msg, extra)
	}
}

// ---- tests: resolveWebhookSessionKey ----

func TestResolveWebhookSessionKey_CallerProvided(t *testing.T) {
	key := resolveWebhookSessionKey("my-session", "agent1", uuid.New(), uuid.NewString())
	if key != "my-session" {
		t.Errorf("expected caller key to pass through verbatim, got %q", key)
	}
}

func TestResolveWebhookSessionKey_Ephemeral(t *testing.T) {
	runID := uuid.NewString()
	key := resolveWebhookSessionKey("", "agent1", uuid.New(), runID)
	if key == "" {
		t.Error("expected non-empty ephemeral key")
	}
	// Must contain "webhook:" prefix.
	if len(key) < 8 || key[:8] != "webhook:" {
		t.Errorf("expected 'webhook:' prefix, got %q", key)
	}
}

// ---- sync happy path ----

func TestWebhookLLMHandler_SyncHappyPath(t *testing.T) {
	agentUUID := uuid.New()
	tenantID := uuid.New()
	webhookID := uuid.New()

	// Agent stub returns a successful result.
	ag := &stubLLMAgent{
		id:      agentUUID.String(),
		agentID: agentUUID,
		runFn: func(_ context.Context, _ agent.RunRequest) (*agent.RunResult, error) {
			return &agent.RunResult{
				Content: "42",
				RunID:   "run-1",
				Usage:   &providers.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
			}, nil
		},
	}

	callStore := &llmCallStore{}
	wh := &store.WebhookData{
		ID:       webhookID,
		TenantID: tenantID,
		AgentID:  &agentUUID,
		Kind:     "llm",
	}

	h := newTestLLMHandler(callStore, &msgWebhookStore{}, nil)
	// Override agentRouter with a stub that returns ag.
	h.agentRouter = stubRouterFor(agentUUID, ag)

	r := injectWebhook(buildLLMReq(t, map[string]any{
		"input": "What is 2+2?",
	}), wh)

	w := httptest.NewRecorder()
	h.handle(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp webhookLLMSyncResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Output != "42" {
		t.Errorf("expected output '42', got %q", resp.Output)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 15 {
		t.Errorf("unexpected usage: %+v", resp.Usage)
	}
	if resp.AgentID != agentUUID.String() {
		t.Errorf("expected agent_id %s, got %s", agentUUID, resp.AgentID)
	}

	// Audit row must be written with status=done.
	if len(callStore.created) != 1 {
		t.Fatalf("expected 1 audit row, got %d", len(callStore.created))
	}
	if callStore.created[0].Status != "done" {
		t.Errorf("expected audit status='done', got %q", callStore.created[0].Status)
	}
	if callStore.created[0].Mode != "sync" {
		t.Errorf("expected audit mode='sync', got %q", callStore.created[0].Mode)
	}
}

// ---- sync timeout → 504 ----

func TestWebhookLLMHandler_SyncTimeout(t *testing.T) {
	agentUUID := uuid.New()
	tenantID := uuid.New()

	// Agent stub blocks until its context is cancelled (simulates a long-running LLM call).
	ag := &stubLLMAgent{
		id:      agentUUID.String(),
		agentID: agentUUID,
		runFn: func(ctx context.Context, _ agent.RunRequest) (*agent.RunResult, error) {
			<-ctx.Done()
			return nil, context.DeadlineExceeded
		},
	}

	callStore := &llmCallStore{}
	wh := &store.WebhookData{
		ID:       uuid.New(),
		TenantID: tenantID,
		AgentID:  &agentUUID,
		Kind:     "llm",
	}

	h := newTestLLMHandler(callStore, &msgWebhookStore{}, nil)
	h.agentRouter = stubRouterFor(agentUUID, ag)
	// Override timeout to 1ms so the test completes immediately.
	h.syncTimeout = 1 * time.Millisecond

	r := injectWebhook(buildLLMReq(t, map[string]any{
		"input": "blocking prompt",
	}), wh)

	w := httptest.NewRecorder()
	h.handle(w, r)

	// 504 Gateway Timeout is the expected response when the agent run exceeds the deadline.
	if w.Code != http.StatusGatewayTimeout {
		t.Errorf("expected 504, got %d: %s", w.Code, w.Body.String())
	}

	// Audit row must be written with status=failed.
	if len(callStore.created) != 1 {
		t.Fatalf("expected 1 audit row on timeout, got %d", len(callStore.created))
	}
	if callStore.created[0].Status != "failed" {
		t.Errorf("expected audit status='failed', got %q", callStore.created[0].Status)
	}
	if callStore.created[0].LastError == nil {
		t.Error("expected LastError set on timeout audit row")
	}
}

// ---- async enqueue ----

func TestWebhookLLMHandler_AsyncEnqueue(t *testing.T) {
	agentUUID := uuid.New()
	tenantID := uuid.New()

	ag := &stubLLMAgent{
		id:      agentUUID.String(),
		agentID: agentUUID,
		runFn: func(_ context.Context, _ agent.RunRequest) (*agent.RunResult, error) {
			return &agent.RunResult{Content: "ok"}, nil
		},
	}

	callStore := &llmCallStore{}
	wh := &store.WebhookData{
		ID:       uuid.New(),
		TenantID: tenantID,
		AgentID:  &agentUUID,
		Kind:     "llm",
	}

	h := newTestLLMHandler(callStore, &msgWebhookStore{}, nil)
	h.agentRouter = stubRouterFor(agentUUID, ag)

	// Use a real public HTTPS URL that passes SSRF validation as callback_url.
	// We use a domain that resolves to a public IP (not RFC1918/loopback).
	// In CI without network, security.Validate still accepts syntax-valid HTTPS public URLs.
	// We use a well-known public IP that is not RFC1918/loopback.
	r := injectWebhook(buildLLMReq(t, map[string]any{
		"input":        "test",
		"mode":         "async",
		"callback_url": "https://93.184.216.34/webhook",
	}), wh)

	w := httptest.NewRecorder()
	h.handle(w, r)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var resp webhookLLMAsyncResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "queued" {
		t.Errorf("expected status='queued', got %q", resp.Status)
	}
	if resp.CallID == "" {
		t.Error("expected non-empty call_id")
	}

	// Audit row must be written with status=queued, mode=async, non-nil delivery_id and callback_url.
	if len(callStore.created) != 1 {
		t.Fatalf("expected 1 queued row, got %d", len(callStore.created))
	}
	row := callStore.created[0]
	if row.Status != "queued" {
		t.Errorf("expected status='queued', got %q", row.Status)
	}
	if row.Mode != "async" {
		t.Errorf("expected mode='async', got %q", row.Mode)
	}
	if row.DeliveryID == uuid.Nil {
		t.Error("expected non-nil delivery_id")
	}
	if row.CallbackURL == nil || *row.CallbackURL == "" {
		t.Error("expected non-empty callback_url in audit row")
	}
	if row.NextAttemptAt == nil {
		t.Error("expected next_attempt_at set for queued row")
	}
}

// ---- cross-tenant agent → 403 ----

func TestWebhookLLMHandler_CrossTenantAgent_Returns403(t *testing.T) {
	agentUUID := uuid.New()
	webhookTenantID := uuid.New()

	// Agent UUID does not match webhook.AgentID — simulates cross-tenant agent.
	differentAgentUUID := uuid.New()
	ag := &stubLLMAgent{
		id:      differentAgentUUID.String(),
		agentID: differentAgentUUID, // UUID() returns a different UUID
		runFn: func(_ context.Context, _ agent.RunRequest) (*agent.RunResult, error) {
			t.Fatal("Run should not be called on cross-tenant agent")
			return nil, nil
		},
	}

	callStore := &llmCallStore{}
	wh := &store.WebhookData{
		ID:       uuid.New(),
		TenantID: webhookTenantID,
		AgentID:  &agentUUID, // webhook bound to agentUUID
		Kind:     "llm",
	}

	h := newTestLLMHandler(callStore, &msgWebhookStore{}, nil)
	// Router returns agent with differentAgentUUID — UUID() != *webhook.AgentID.
	h.agentRouter = stubRouterFor(agentUUID, ag)

	r := injectWebhook(buildLLMReq(t, map[string]any{
		"input": "hello",
	}), wh)

	w := httptest.NewRecorder()
	h.handle(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// ---- missing input → 400 ----

func TestWebhookLLMHandler_MissingInput_Returns400(t *testing.T) {
	agentUUID := uuid.New()
	wh := &store.WebhookData{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		AgentID:  &agentUUID,
		Kind:     "llm",
	}

	h := newTestLLMHandler(&llmCallStore{}, &msgWebhookStore{}, nil)
	h.agentRouter = stubRouterFor(agentUUID, &stubLLMAgent{id: agentUUID.String(), agentID: agentUUID,
		runFn: func(_ context.Context, _ agent.RunRequest) (*agent.RunResult, error) {
			return &agent.RunResult{Content: "ok"}, nil
		},
	})

	r := injectWebhook(buildLLMReq(t, map[string]any{
		// input deliberately omitted
	}), wh)

	w := httptest.NewRecorder()
	h.handle(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// ---- async missing callback_url → 400 ----

func TestWebhookLLMHandler_AsyncMissingCallbackURL_Returns400(t *testing.T) {
	agentUUID := uuid.New()
	ag := &stubLLMAgent{id: agentUUID.String(), agentID: agentUUID,
		runFn: func(_ context.Context, _ agent.RunRequest) (*agent.RunResult, error) {
			return &agent.RunResult{Content: "ok"}, nil
		},
	}

	wh := &store.WebhookData{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		AgentID:  &agentUUID,
		Kind:     "llm",
	}

	h := newTestLLMHandler(&llmCallStore{}, &msgWebhookStore{}, nil)
	h.agentRouter = stubRouterFor(agentUUID, ag)

	r := injectWebhook(buildLLMReq(t, map[string]any{
		"input": "hi",
		"mode":  "async",
		// callback_url missing
	}), wh)

	w := httptest.NewRecorder()
	h.handle(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// ---- invalid mode → 400 ----

func TestWebhookLLMHandler_InvalidMode_Returns400(t *testing.T) {
	agentUUID := uuid.New()
	ag := &stubLLMAgent{id: agentUUID.String(), agentID: agentUUID,
		runFn: func(_ context.Context, _ agent.RunRequest) (*agent.RunResult, error) {
			return &agent.RunResult{Content: "ok"}, nil
		},
	}

	wh := &store.WebhookData{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		AgentID:  &agentUUID,
		Kind:     "llm",
	}

	h := newTestLLMHandler(&llmCallStore{}, &msgWebhookStore{}, nil)
	h.agentRouter = stubRouterFor(agentUUID, ag)

	r := injectWebhook(buildLLMReq(t, map[string]any{
		"input": "hi",
		"mode":  "invalid-mode",
	}), wh)

	w := httptest.NewRecorder()
	h.handle(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// ---- agent not found → 404 ----

func TestWebhookLLMHandler_AgentNotFound_Returns404(t *testing.T) {
	agentUUID := uuid.New()
	wh := &store.WebhookData{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		AgentID:  &agentUUID,
		Kind:     "llm",
	}

	h := newTestLLMHandler(&llmCallStore{}, &msgWebhookStore{}, nil)
	// Router returns error for all agents.
	h.agentRouter = stubRouterError(errors.New("agent not found"))

	r := injectWebhook(buildLLMReq(t, map[string]any{
		"input": "hi",
	}), wh)

	w := httptest.NewRecorder()
	h.handle(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// ---- helpers: stub agent router ----

// stubRouterFor creates a *agent.Router that resolves one agent by any ID.
// Since Router.Get does a DB resolver call when not cached, we use a custom
// approach: set the resolver function to return the stub agent.
func stubRouterFor(agentUUID uuid.UUID, ag agent.Agent) *agent.Router {
	r := agent.NewRouter()
	r.SetResolver(func(_ context.Context, _ string) (agent.Agent, error) {
		return ag, nil
	})
	return r
}

// stubRouterError creates a *agent.Router whose resolver always returns an error.
func stubRouterError(err error) *agent.Router {
	r := agent.NewRouter()
	r.SetResolver(func(_ context.Context, _ string) (agent.Agent, error) {
		return nil, err
	})
	return r
}
