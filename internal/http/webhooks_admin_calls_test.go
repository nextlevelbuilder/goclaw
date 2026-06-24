package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ---- stub WebhookCallStore for admin tests ----

type adminCallStore struct {
	mu    sync.Mutex
	rows  []store.WebhookCallData
	calls int // List invocation counter
}

func (s *adminCallStore) Create(_ context.Context, c *store.WebhookCallData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = append(s.rows, *c)
	return nil
}

func (s *adminCallStore) List(ctx context.Context, f store.WebhookCallListFilter) ([]store.WebhookCallData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	tid := store.TenantIDFromContext(ctx)
	var out []store.WebhookCallData
	for _, r := range s.rows {
		// Mirror real store tenant scoping.
		if tid != uuid.Nil && r.TenantID != tid && !store.IsOwnerRole(ctx) {
			continue
		}
		if f.WebhookID != nil && r.WebhookID != *f.WebhookID {
			continue
		}
		if f.Status != "" && r.Status != f.Status {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *adminCallStore) Count(ctx context.Context, f store.WebhookCallListFilter) (int, error) {
	rows, err := s.List(ctx, f)
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

func (s *adminCallStore) GetByID(ctx context.Context, id uuid.UUID) (*store.WebhookCallData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tid := store.TenantIDFromContext(ctx)
	for i := range s.rows {
		if s.rows[i].ID != id {
			continue
		}
		if tid != uuid.Nil && s.rows[i].TenantID != tid && !store.IsOwnerRole(ctx) {
			return nil, sql.ErrNoRows
		}
		cp := s.rows[i]
		return &cp, nil
	}
	return nil, sql.ErrNoRows
}
func (s *adminCallStore) GetByIdempotency(context.Context, uuid.UUID, string) (*store.WebhookCallData, error) {
	return nil, sql.ErrNoRows
}
func (s *adminCallStore) UpdateStatus(context.Context, uuid.UUID, map[string]any) error { return nil }
func (s *adminCallStore) UpdateStatusCAS(context.Context, uuid.UUID, string, map[string]any) error {
	return nil
}
func (s *adminCallStore) ClaimNext(context.Context, uuid.UUID, time.Time) (*store.WebhookCallData, error) {
	return nil, sql.ErrNoRows
}
func (s *adminCallStore) DeleteOlderThan(context.Context, uuid.UUID, time.Time) (int64, error) {
	return 0, nil
}
func (s *adminCallStore) ReclaimStale(context.Context, time.Time) (int64, error) { return 0, nil }

// ---- stub testers ----

type stubLLMTester struct {
	resp    *webhookLLMSyncResp
	err     error
	gotWh   *store.WebhookData
	gotIn   string
	gotMdl  string
	invoked bool
}

func (s *stubLLMTester) RunTest(_ context.Context, wh *store.WebhookData, input, model string) (*webhookLLMSyncResp, error) {
	s.invoked = true
	s.gotWh = wh
	s.gotIn = input
	s.gotMdl = model
	return s.resp, s.err
}

type stubMsgTester struct {
	resp    *webhookMessageResp
	err     error
	invoked bool
}

func (s *stubMsgTester) RunTest(_ context.Context, _ *store.WebhookData, _ webhookMessageReq) (*webhookMessageResp, error) {
	s.invoked = true
	return s.resp, s.err
}

// ---- helpers ----

func newAdminHandlerWithCalls(ws *adminWebhookStore, cs store.WebhookCallStore, ts *adminTenantStore) *WebhooksAdminHandler {
	h := NewWebhooksAdminHandler(ws, cs, ts, nil)
	h.SetEncKey(testAdminEncKey)
	return h
}

// ---- list-calls tests ----

func TestWebhookAdmin_ListCalls_Success(t *testing.T) {
	tid := uuid.New()
	wh := &store.WebhookData{ID: uuid.New(), TenantID: tid, Name: "wh", Kind: "llm"}
	cs := &adminCallStore{}
	now := time.Now()
	done := "done"
	cs.rows = []store.WebhookCallData{
		{ID: uuid.New(), TenantID: tid, WebhookID: wh.ID, DeliveryID: uuid.New(), Mode: "sync", Status: "done", Attempts: 1, CreatedAt: now, Response: []byte(`{"output":"hi"}`)},
		{ID: uuid.New(), TenantID: tid, WebhookID: wh.ID, DeliveryID: uuid.New(), Mode: "async", Status: "failed", Attempts: 3, CreatedAt: now, LastError: &done},
		{ID: uuid.New(), TenantID: uuid.New(), WebhookID: uuid.New(), DeliveryID: uuid.New(), Mode: "sync", Status: "done", CreatedAt: now}, // other tenant/webhook
	}
	h := newAdminHandlerWithCalls(newAdminWebhookStore(wh), cs, &adminTenantStore{roles: map[string]string{tid.String() + ":admin-user": "admin"}})

	ctx := webhookTenantAdminCtx(tid, "admin-user")
	w := doRequest(t, h, http.MethodGet, "/v1/webhooks/"+wh.ID.String()+"/calls", nil, ctx)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Items []webhookCallResp `json:"items"`
		Total int               `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 2 {
		t.Fatalf("want 2 calls for this webhook, got %d", len(body.Items))
	}
	if body.Total != 2 {
		t.Fatalf("want total 2, got %d", body.Total)
	}
}

func TestWebhookAdmin_ListCalls_StatusFilter(t *testing.T) {
	tid := uuid.New()
	wh := &store.WebhookData{ID: uuid.New(), TenantID: tid, Name: "wh", Kind: "llm"}
	cs := &adminCallStore{}
	now := time.Now()
	cs.rows = []store.WebhookCallData{
		{ID: uuid.New(), TenantID: tid, WebhookID: wh.ID, Status: "done", CreatedAt: now},
		{ID: uuid.New(), TenantID: tid, WebhookID: wh.ID, Status: "failed", CreatedAt: now},
	}
	h := newAdminHandlerWithCalls(newAdminWebhookStore(wh), cs, &adminTenantStore{roles: map[string]string{tid.String() + ":admin-user": "admin"}})

	ctx := webhookTenantAdminCtx(tid, "admin-user")
	w := doRequest(t, h, http.MethodGet, "/v1/webhooks/"+wh.ID.String()+"/calls?status=failed", nil, ctx)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var body struct {
		Items []webhookCallResp `json:"items"`
		Total int               `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if len(body.Items) != 1 || body.Items[0].Status != "failed" {
		t.Fatalf("status filter failed: %+v", body.Items)
	}
	if body.Total != 1 {
		t.Fatalf("want total 1, got %d", body.Total)
	}
}

func TestWebhookAdmin_ListCalls_TenantIsolation(t *testing.T) {
	ownerTid := uuid.New()
	otherTid := uuid.New()
	wh := &store.WebhookData{ID: uuid.New(), TenantID: ownerTid, Name: "wh", Kind: "llm"}
	cs := &adminCallStore{}
	h := newAdminHandlerWithCalls(newAdminWebhookStore(wh), cs, &adminTenantStore{roles: map[string]string{
		ownerTid.String() + ":owner-user": "admin",
		otherTid.String() + ":other":      "admin", // valid admin of a different tenant
	}})

	// A different tenant's admin must not see the webhook (tenant-scoped GetByID → 404).
	ctx := webhookTenantAdminCtx(otherTid, "other")
	w := doRequest(t, h, http.MethodGet, "/v1/webhooks/"+wh.ID.String()+"/calls", nil, ctx)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant must get 404, got %d", w.Code)
	}
}

func TestWebhookAdmin_ListCalls_NilStore503(t *testing.T) {
	tid := uuid.New()
	wh := &store.WebhookData{ID: uuid.New(), TenantID: tid, Name: "wh", Kind: "llm"}
	h := newAdminHandler(newAdminWebhookStore(wh), &adminTenantStore{roles: map[string]string{tid.String() + ":u": "admin"}}) // calls == nil
	ctx := webhookTenantAdminCtx(tid, "u")
	w := doRequest(t, h, http.MethodGet, "/v1/webhooks/"+wh.ID.String()+"/calls", nil, ctx)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil call store must get 503, got %d", w.Code)
	}
}

// ---- get-call (detail) tests ----

func TestWebhookAdmin_GetCall_Success(t *testing.T) {
	tid := uuid.New()
	wh := &store.WebhookData{ID: uuid.New(), TenantID: tid, Name: "wh", Kind: "llm"}
	callID := uuid.New()
	cb := "https://example.com/cb"
	cs := &adminCallStore{}
	cs.rows = []store.WebhookCallData{{
		ID: callID, TenantID: tid, WebhookID: wh.ID, DeliveryID: uuid.New(), Mode: "async",
		Status: "done", Attempts: 1, CallbackURL: &cb, CreatedAt: time.Now(),
		RequestPayload: []byte(`{"body_hash":"abc","meta":{}}`), Response: []byte(`{"output":"ok"}`),
	}}
	h := newAdminHandlerWithCalls(newAdminWebhookStore(wh), cs, &adminTenantStore{roles: map[string]string{tid.String() + ":u": "admin"}})

	ctx := webhookTenantAdminCtx(tid, "u")
	w := doRequest(t, h, http.MethodGet, "/v1/webhooks/"+wh.ID.String()+"/calls/"+callID.String(), nil, ctx)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var got webhookCallDetailResp
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != callID || got.RequestPayload == "" || got.Response == "" || got.CallbackURL == nil {
		t.Fatalf("detail missing fields: %+v", got)
	}
}

func TestWebhookAdmin_GetCall_WrongWebhook404(t *testing.T) {
	tid := uuid.New()
	wh := &store.WebhookData{ID: uuid.New(), TenantID: tid, Name: "wh", Kind: "llm"}
	other := &store.WebhookData{ID: uuid.New(), TenantID: tid, Name: "other", Kind: "llm"}
	callID := uuid.New()
	cs := &adminCallStore{}
	// Call belongs to `other`, not `wh`.
	cs.rows = []store.WebhookCallData{{ID: callID, TenantID: tid, WebhookID: other.ID, CreatedAt: time.Now()}}
	h := newAdminHandlerWithCalls(newAdminWebhookStore(wh, other), cs, &adminTenantStore{roles: map[string]string{tid.String() + ":u": "admin"}})

	ctx := webhookTenantAdminCtx(tid, "u")
	w := doRequest(t, h, http.MethodGet, "/v1/webhooks/"+wh.ID.String()+"/calls/"+callID.String(), nil, ctx)
	if w.Code != http.StatusNotFound {
		t.Fatalf("call of another webhook must be 404, got %d", w.Code)
	}
}

func TestWebhookAdmin_GetCall_TenantIsolation(t *testing.T) {
	ownerTid := uuid.New()
	otherTid := uuid.New()
	wh := &store.WebhookData{ID: uuid.New(), TenantID: ownerTid, Name: "wh", Kind: "llm"}
	callID := uuid.New()
	cs := &adminCallStore{}
	cs.rows = []store.WebhookCallData{{ID: callID, TenantID: ownerTid, WebhookID: wh.ID, CreatedAt: time.Now()}}
	h := newAdminHandlerWithCalls(newAdminWebhookStore(wh), cs, &adminTenantStore{roles: map[string]string{
		ownerTid.String() + ":owner": "admin",
		otherTid.String() + ":other": "admin",
	}})

	ctx := webhookTenantAdminCtx(otherTid, "other")
	w := doRequest(t, h, http.MethodGet, "/v1/webhooks/"+wh.ID.String()+"/calls/"+callID.String(), nil, ctx)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant must be 404, got %d", w.Code)
	}
}

// ---- test-endpoint tests ----

func TestWebhookAdmin_Test_LLM_Success(t *testing.T) {
	tid := uuid.New()
	agentID := uuid.New()
	wh := &store.WebhookData{ID: uuid.New(), TenantID: tid, Name: "wh", Kind: "llm", AgentID: &agentID}
	cs := &adminCallStore{}
	h := newAdminHandlerWithCalls(newAdminWebhookStore(wh), cs, &adminTenantStore{roles: map[string]string{tid.String() + ":u": "admin"}})
	llm := &stubLLMTester{resp: &webhookLLMSyncResp{CallID: "c1", AgentID: agentID.String(), Output: "pong", FinishReason: "stop"}}
	h.llmTester = llm

	ctx := webhookTenantAdminCtx(tid, "u")
	w := doRequest(t, h, http.MethodPost, "/v1/webhooks/"+wh.ID.String()+"/test", map[string]any{"input": "ping"}, ctx)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if !llm.invoked || llm.gotIn != "ping" {
		t.Fatalf("tester not invoked correctly: %+v", llm)
	}
}

func TestWebhookAdmin_Test_LLM_MissingInput(t *testing.T) {
	tid := uuid.New()
	agentID := uuid.New()
	wh := &store.WebhookData{ID: uuid.New(), TenantID: tid, Name: "wh", Kind: "llm", AgentID: &agentID}
	h := newAdminHandlerWithCalls(newAdminWebhookStore(wh), &adminCallStore{}, &adminTenantStore{roles: map[string]string{tid.String() + ":u": "admin"}})
	h.llmTester = &stubLLMTester{}
	ctx := webhookTenantAdminCtx(tid, "u")
	w := doRequest(t, h, http.MethodPost, "/v1/webhooks/"+wh.ID.String()+"/test", map[string]any{}, ctx)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing input must be 400, got %d", w.Code)
	}
}

func TestWebhookAdmin_Test_LLM_RunError(t *testing.T) {
	tid := uuid.New()
	agentID := uuid.New()
	wh := &store.WebhookData{ID: uuid.New(), TenantID: tid, Name: "wh", Kind: "llm", AgentID: &agentID}
	h := newAdminHandlerWithCalls(newAdminWebhookStore(wh), &adminCallStore{}, &adminTenantStore{roles: map[string]string{tid.String() + ":u": "admin"}})
	h.llmTester = &stubLLMTester{err: errors.New("boom")}
	ctx := webhookTenantAdminCtx(tid, "u")
	w := doRequest(t, h, http.MethodPost, "/v1/webhooks/"+wh.ID.String()+"/test", map[string]any{"input": "x"}, ctx)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("run error must be 502, got %d", w.Code)
	}
}

func TestWebhookAdmin_Test_Message_RequiresStandard(t *testing.T) {
	tid := uuid.New()
	wh := &store.WebhookData{ID: uuid.New(), TenantID: tid, Name: "wh", Kind: "message"}
	h := newAdminHandlerWithCalls(newAdminWebhookStore(wh), &adminCallStore{}, &adminTenantStore{roles: map[string]string{tid.String() + ":u": "admin"}})
	// msgTester left nil → message test must be rejected.
	ctx := webhookTenantAdminCtx(tid, "u")
	w := doRequest(t, h, http.MethodPost, "/v1/webhooks/"+wh.ID.String()+"/test", map[string]any{"chat_id": "123", "content": "hi"}, ctx)
	if w.Code != http.StatusForbidden {
		t.Fatalf("nil msgTester must be 403, got %d", w.Code)
	}
}

func TestWebhookAdmin_Test_Message_Success(t *testing.T) {
	tid := uuid.New()
	wh := &store.WebhookData{ID: uuid.New(), TenantID: tid, Name: "wh", Kind: "message"}
	h := newAdminHandlerWithCalls(newAdminWebhookStore(wh), &adminCallStore{}, &adminTenantStore{roles: map[string]string{tid.String() + ":u": "admin"}})
	msg := &stubMsgTester{resp: &webhookMessageResp{CallID: "c1", Status: "sent", ChannelName: "tg", ChatID: "123"}}
	h.msgTester = msg
	ctx := webhookTenantAdminCtx(tid, "u")
	w := doRequest(t, h, http.MethodPost, "/v1/webhooks/"+wh.ID.String()+"/test", map[string]any{"channel_name": "tg", "chat_id": "123", "content": "hi"}, ctx)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if !msg.invoked {
		t.Fatal("msg tester not invoked")
	}
}

func TestWebhookAdmin_Test_Message_MissingChannelName(t *testing.T) {
	tid := uuid.New()
	wh := &store.WebhookData{ID: uuid.New(), TenantID: tid, Name: "wh", Kind: "message"}
	h := newAdminHandlerWithCalls(newAdminWebhookStore(wh), &adminCallStore{}, &adminTenantStore{roles: map[string]string{tid.String() + ":u": "admin"}})
	msg := &stubMsgTester{resp: &webhookMessageResp{CallID: "c1", Status: "sent", ChannelName: "tg", ChatID: "123"}}
	h.msgTester = msg
	ctx := webhookTenantAdminCtx(tid, "u")
	w := doRequest(t, h, http.MethodPost, "/v1/webhooks/"+wh.ID.String()+"/test", map[string]any{"chat_id": "123", "content": "hi"}, ctx)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing channel_name must be 400, got %d", w.Code)
	}
	if msg.invoked {
		t.Fatal("msg tester must not run when channel_name is missing")
	}
}

func TestWebhookAdmin_Test_Message_MissingContent(t *testing.T) {
	tid := uuid.New()
	wh := &store.WebhookData{ID: uuid.New(), TenantID: tid, Name: "wh", Kind: "message"}
	h := newAdminHandlerWithCalls(newAdminWebhookStore(wh), &adminCallStore{}, &adminTenantStore{roles: map[string]string{tid.String() + ":u": "admin"}})
	msg := &stubMsgTester{resp: &webhookMessageResp{CallID: "c1", Status: "sent", ChannelName: "tg", ChatID: "123"}}
	h.msgTester = msg
	ctx := webhookTenantAdminCtx(tid, "u")
	w := doRequest(t, h, http.MethodPost, "/v1/webhooks/"+wh.ID.String()+"/test", map[string]any{"channel_name": "tg", "chat_id": "123"}, ctx)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing content must be 400, got %d", w.Code)
	}
	if msg.invoked {
		t.Fatal("msg tester must not run when content is missing")
	}
}

func TestWebhookAdmin_Test_Revoked409(t *testing.T) {
	tid := uuid.New()
	agentID := uuid.New()
	wh := &store.WebhookData{ID: uuid.New(), TenantID: tid, Name: "wh", Kind: "llm", AgentID: &agentID, Revoked: true}
	h := newAdminHandlerWithCalls(newAdminWebhookStore(wh), &adminCallStore{}, &adminTenantStore{roles: map[string]string{tid.String() + ":u": "admin"}})
	h.llmTester = &stubLLMTester{}
	ctx := webhookTenantAdminCtx(tid, "u")
	w := doRequest(t, h, http.MethodPost, "/v1/webhooks/"+wh.ID.String()+"/test", map[string]any{"input": "x"}, ctx)
	if w.Code != http.StatusConflict {
		t.Fatalf("revoked webhook test must be 409, got %d", w.Code)
	}
}
