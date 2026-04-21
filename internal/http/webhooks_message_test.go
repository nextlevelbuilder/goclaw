package http

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ---- stub: channelDispatcher ----

// stubDispatcher implements channelDispatcher. Configured per-test.
type stubDispatcher struct {
	// tenantsByName maps channel name → tenant UUID.
	// uuid.Nil = legacy (no tenant scope). Use missingChannelName to simulate not found.
	tenantsByName   map[string]uuid.UUID
	typeByName      map[string]string
	missingChannels map[string]bool // channels to report as non-existent

	sentTo    []bus.OutboundMessage // captured by SendToChannel
	sentMedia []bus.OutboundMessage // captured by SendMediaToChannel
	sendErr   error                 // optional error to inject on send
}

func newStubDispatcher() *stubDispatcher {
	return &stubDispatcher{
		tenantsByName:   make(map[string]uuid.UUID),
		typeByName:      make(map[string]string),
		missingChannels: make(map[string]bool),
	}
}

func (s *stubDispatcher) addChannel(name, chType string, tenantID uuid.UUID) {
	s.tenantsByName[name] = tenantID
	s.typeByName[name] = chType
}

func (s *stubDispatcher) ChannelTenantID(name string) (uuid.UUID, bool) {
	if s.missingChannels[name] {
		return uuid.Nil, false
	}
	tid, ok := s.tenantsByName[name]
	return tid, ok
}

func (s *stubDispatcher) ChannelTypeForName(name string) string {
	return s.typeByName[name]
}

func (s *stubDispatcher) SendToChannel(_ context.Context, channelName, chatID, content string) error {
	if s.sendErr != nil {
		return s.sendErr
	}
	s.sentTo = append(s.sentTo, bus.OutboundMessage{
		Channel: channelName,
		ChatID:  chatID,
		Content: content,
	})
	return nil
}

func (s *stubDispatcher) SendMediaToChannel(_ context.Context, channelName, chatID, content string, media []bus.MediaAttachment) error {
	if s.sendErr != nil {
		return s.sendErr
	}
	s.sentMedia = append(s.sentMedia, bus.OutboundMessage{
		Channel: channelName,
		ChatID:  chatID,
		Content: content,
		Media:   media,
	})
	return nil
}

// ---- stub: store.WebhookCallStore (message handler tests) ----

// msgCallStore records WebhookCallData rows created by the handler for assertion.
type msgCallStore struct {
	created []*store.WebhookCallData
}

func (s *msgCallStore) Create(_ context.Context, c *store.WebhookCallData) error {
	s.created = append(s.created, c)
	return nil
}
func (s *msgCallStore) GetByID(_ context.Context, _ uuid.UUID) (*store.WebhookCallData, error) {
	return nil, sql.ErrNoRows
}
func (s *msgCallStore) GetByIdempotency(_ context.Context, _ uuid.UUID, _ string) (*store.WebhookCallData, error) {
	return nil, sql.ErrNoRows
}
func (s *msgCallStore) UpdateStatusCAS(_ context.Context, _ uuid.UUID, _ string, _ map[string]any) error {
	return nil
}
func (s *msgCallStore) UpdateStatus(_ context.Context, _ uuid.UUID, _ map[string]any) error {
	return nil
}
func (s *msgCallStore) ClaimNext(_ context.Context, _ uuid.UUID, _ time.Time) (*store.WebhookCallData, error) {
	return nil, sql.ErrNoRows
}
func (s *msgCallStore) List(_ context.Context, _ store.WebhookCallListFilter) ([]store.WebhookCallData, error) {
	return nil, nil
}
func (s *msgCallStore) DeleteOlderThan(_ context.Context, _ uuid.UUID, _ time.Time) (int64, error) {
	return 0, nil
}
func (s *msgCallStore) ReclaimStale(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

// ---- stub: store.WebhookStore (message handler tests — minimal no-op) ----

// msgWebhookStore is a no-op WebhookStore used when the handler under test
// doesn't exercise webhook store lookups (auth is bypassed in unit tests).
type msgWebhookStore struct{}

func (s *msgWebhookStore) Create(_ context.Context, _ *store.WebhookData) error { return nil }
func (s *msgWebhookStore) GetByID(_ context.Context, _ uuid.UUID) (*store.WebhookData, error) {
	return nil, sql.ErrNoRows
}
func (s *msgWebhookStore) GetByHash(_ context.Context, _ string) (*store.WebhookData, error) {
	return nil, sql.ErrNoRows
}
func (s *msgWebhookStore) List(_ context.Context, _ store.WebhookListFilter) ([]store.WebhookData, error) {
	return nil, nil
}
func (s *msgWebhookStore) Update(_ context.Context, _ uuid.UUID, _ map[string]any) error {
	return nil
}
func (s *msgWebhookStore) RotateSecret(_ context.Context, _ uuid.UUID, _, _, _ string) error {
	return nil
}
func (s *msgWebhookStore) Revoke(_ context.Context, _ uuid.UUID) error        { return nil }
func (s *msgWebhookStore) TouchLastUsed(_ context.Context, _ uuid.UUID) error { return nil }
func (s *msgWebhookStore) GetByHashUnscoped(_ context.Context, _ string) (*store.WebhookData, error) {
	return nil, sql.ErrNoRows
}
func (s *msgWebhookStore) GetByIDUnscoped(_ context.Context, _ uuid.UUID) (*store.WebhookData, error) {
	return nil, sql.ErrNoRows
}

// ---- stub: store.ChannelInstanceStore ----

type stubChannelInstanceStore struct {
	inst *store.ChannelInstanceData
}

func (s *stubChannelInstanceStore) Create(_ context.Context, _ *store.ChannelInstanceData) error {
	return nil
}
func (s *stubChannelInstanceStore) Get(_ context.Context, _ uuid.UUID) (*store.ChannelInstanceData, error) {
	if s.inst != nil {
		return s.inst, nil
	}
	return nil, sql.ErrNoRows
}
func (s *stubChannelInstanceStore) GetByName(_ context.Context, _ string) (*store.ChannelInstanceData, error) {
	if s.inst != nil {
		return s.inst, nil
	}
	return nil, sql.ErrNoRows
}
func (s *stubChannelInstanceStore) Update(_ context.Context, _ uuid.UUID, _ map[string]any) error {
	return nil
}
func (s *stubChannelInstanceStore) Delete(_ context.Context, _ uuid.UUID) error { return nil }
func (s *stubChannelInstanceStore) ListEnabled(_ context.Context) ([]store.ChannelInstanceData, error) {
	return nil, nil
}
func (s *stubChannelInstanceStore) ListAll(_ context.Context) ([]store.ChannelInstanceData, error) {
	return nil, nil
}
func (s *stubChannelInstanceStore) ListAllInstances(_ context.Context) ([]store.ChannelInstanceData, error) {
	return nil, nil
}
func (s *stubChannelInstanceStore) ListAllEnabled(_ context.Context) ([]store.ChannelInstanceData, error) {
	return nil, nil
}
func (s *stubChannelInstanceStore) ListPaged(_ context.Context, _ store.ChannelInstanceListOpts) ([]store.ChannelInstanceData, error) {
	return nil, nil
}
func (s *stubChannelInstanceStore) CountInstances(_ context.Context, _ store.ChannelInstanceListOpts) (int, error) {
	return 0, nil
}

// ---- helper: build handler ----

// tenantA and tenantB are stable UUIDs for cross-tenant tests.
var (
	tenantA = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	tenantB = uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
)

// buildHandler wires a WebhookMessageHandler with the given dispatcher stub.
func buildHandler(t *testing.T, disp channelDispatcher, calls *msgCallStore) *WebhookMessageHandler {
	t.Helper()
	if calls == nil {
		calls = &msgCallStore{}
	}
	h := &WebhookMessageHandler{
		channelMgr:       disp,
		channelInstances: &stubChannelInstanceStore{},
		callStore:        calls,
		webhooks:         &msgWebhookStore{},
		limiter:          newWebhookLimiter(0),
	}
	return h
}

// invokeHandle fires h.handle directly with the webhook injected into context.
func invokeHandle(t *testing.T, h *WebhookMessageHandler, webhook *store.WebhookData, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/message", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")

	ctx := store.WithTenantID(req.Context(), webhook.TenantID)
	ctx = WithWebhookData(ctx, webhook)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.handle(rr, req)
	return rr
}

func newWebhook(tenantID uuid.UUID, channelID *uuid.UUID) *store.WebhookData {
	return &store.WebhookData{
		ID:        store.GenNewID(),
		TenantID:  tenantID,
		Kind:      "message",
		ChannelID: channelID,
	}
}

// ---- tests ----

// TestWebhookMessage_PlainText_HappyPath verifies a text-only message delivers 200 with
// status="sent" and writes a done audit record.
func TestWebhookMessage_PlainText_HappyPath(t *testing.T) {
	disp := newStubDispatcher()
	disp.addChannel("tg-main", channels.TypeTelegram, tenantA)

	calls := &msgCallStore{}
	h := buildHandler(t, disp, calls)
	wh := newWebhook(tenantA, nil)

	rr := invokeHandle(t, h, wh, map[string]any{
		"channel_name": "tg-main",
		"chat_id":      "123",
		"content":      "hello world",
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp webhookMessageResp
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != "sent" {
		t.Errorf("want status=sent, got %q", resp.Status)
	}
	if resp.Warning != "" {
		t.Errorf("want no warning, got %q", resp.Warning)
	}
	// Audit record must be done.
	if len(calls.created) != 1 || calls.created[0].Status != "done" {
		t.Errorf("want 1 done audit record, got %d records", len(calls.created))
	}
	// Text must have been dispatched.
	if len(disp.sentTo) != 1 {
		t.Errorf("want 1 SendToChannel call, got %d", len(disp.sentTo))
	}
}

// TestWebhookMessage_CrossTenant_Deny validates the P0 isolation invariant:
// a webhook from tenantA must not be able to send through a channel owned by tenantB.
func TestWebhookMessage_CrossTenant_Deny(t *testing.T) {
	disp := newStubDispatcher()
	disp.addChannel("discord-b", channels.TypeDiscord, tenantB) // owned by tenantB

	calls := &msgCallStore{}
	h := buildHandler(t, disp, calls)
	wh := newWebhook(tenantA, nil) // webhook belongs to tenantA

	rr := invokeHandle(t, h, wh, map[string]any{
		"channel_name": "discord-b",
		"chat_id":      "456",
		"content":      "cross-tenant attempt",
	})

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
	// Nothing must have been sent.
	if len(disp.sentTo)+len(disp.sentMedia) > 0 {
		t.Error("no message must be delivered on tenant mismatch")
	}
	// No done audit record.
	for _, c := range calls.created {
		if c.Status == "done" {
			t.Errorf("unexpected done audit record on cross-tenant attempt")
		}
	}
}

// TestWebhookMessage_SSRFBlock_RFC1918 validates that a RFC1918 media_url is rejected
// with 400 before any channel send.
func TestWebhookMessage_SSRFBlock_RFC1918(t *testing.T) {
	disp := newStubDispatcher()
	disp.addChannel("tg-main", channels.TypeTelegram, tenantA)

	calls := &msgCallStore{}
	h := buildHandler(t, disp, calls)
	wh := newWebhook(tenantA, nil)

	rr := invokeHandle(t, h, wh, map[string]any{
		"channel_name": "tg-main",
		"chat_id":      "123",
		"content":      "text",
		"media_url":    "http://192.168.1.1/secret.jpg", // RFC1918 — blocked
	})

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for RFC1918 media_url, got %d: %s", rr.Code, rr.Body.String())
	}
	if len(disp.sentTo)+len(disp.sentMedia) > 0 {
		t.Error("no message must be sent when media URL is SSRF-blocked")
	}
	// Must record a failed audit call.
	if len(calls.created) == 0 || calls.created[0].Status != "failed" {
		t.Errorf("expected failed audit record, got %+v", calls.created)
	}
}

// TestWebhookMessage_MediaUnsupported_FallbackOn verifies that when the channel
// doesn't support media and fallback_to_text=true, a 200 is returned with warning
// and text-only delivery is performed (no media sent).
func TestWebhookMessage_MediaUnsupported_FallbackOn(t *testing.T) {
	disp := newStubDispatcher()
	disp.addChannel("zalo-main", channels.TypeZaloOA, tenantA) // zalo_oa: not media capable

	calls := &msgCallStore{}
	h := buildHandler(t, disp, calls)
	wh := newWebhook(tenantA, nil)

	// Allow loopback so httptest.Server passes SSRF validation.
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	mediaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", "1024")
		w.WriteHeader(http.StatusOK)
	}))
	defer mediaServer.Close()

	rr := invokeHandle(t, h, wh, map[string]any{
		"channel_name":     "zalo-main",
		"chat_id":          "789",
		"content":          "fallback text",
		"media_url":        mediaServer.URL + "/image.jpg",
		"fallback_to_text": true,
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 with fallback, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp webhookMessageResp
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Warning != "media_not_supported_fallback_text" {
		t.Errorf("expected fallback warning, got %q", resp.Warning)
	}
	// Text must have been sent; no media dispatch.
	if len(disp.sentTo) != 1 {
		t.Errorf("expected 1 text send, got %d", len(disp.sentTo))
	}
	if len(disp.sentMedia) != 0 {
		t.Errorf("expected no media send, got %d", len(disp.sentMedia))
	}
}

// TestWebhookMessage_MediaUnsupported_FallbackOff verifies that when the channel
// doesn't support media and fallback_to_text is false (default), a 501 is returned.
func TestWebhookMessage_MediaUnsupported_FallbackOff(t *testing.T) {
	disp := newStubDispatcher()
	disp.addChannel("zalo-main", channels.TypeZaloOA, tenantA)

	calls := &msgCallStore{}
	h := buildHandler(t, disp, calls)
	wh := newWebhook(tenantA, nil)

	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	mediaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", "512")
		w.WriteHeader(http.StatusOK)
	}))
	defer mediaServer.Close()

	rr := invokeHandle(t, h, wh, map[string]any{
		"channel_name": "zalo-main",
		"chat_id":      "789",
		"content":      "text",
		"media_url":    mediaServer.URL + "/image.jpg",
		// fallback_to_text omitted → defaults false
	})

	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", rr.Code, rr.Body.String())
	}
	if len(disp.sentTo)+len(disp.sentMedia) > 0 {
		t.Error("no message must be sent when media is unsupported and fallback is off")
	}
	if len(calls.created) == 0 || calls.created[0].Status != "failed" {
		t.Errorf("expected failed audit record, got %+v", calls.created)
	}
}

// ---- probeMediaURL unit tests ----

// TestProbeMediaURL_SSRFBlock verifies RFC1918 / link-local addresses are blocked.
func TestProbeMediaURL_SSRFBlock(t *testing.T) {
	blocked := []string{
		"http://127.0.0.1/secret",
		"http://10.0.0.1/secret",
		"http://192.168.1.1/secret",
		"http://169.254.169.254/latest/meta-data/",
	}
	for _, u := range blocked {
		t.Run(u, func(t *testing.T) {
			_, err := probeMediaURL(u)
			if err == nil {
				t.Fatalf("expected SSRF block, got nil error")
			}
			var mve *mediaValidateError
			if !errors.As(err, &mve) || mve.code != "ssrf" {
				t.Errorf("expected ssrf error, got %T: %v", err, err)
			}
		})
	}
}

// TestProbeMediaURL_MIMEDenied verifies non-allowlisted MIME types return mime_denied.
func TestProbeMediaURL_MIMEDenied(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, err := probeMediaURL(srv.URL + "/page.html")
	if err == nil {
		t.Fatal("expected error for denied MIME, got nil")
	}
	var mve *mediaValidateError
	if !errors.As(err, &mve) || mve.code != "mime_denied" {
		t.Errorf("expected mime_denied, got code=%q err=%v", mve.code, err)
	}
}

// TestProbeMediaURL_TooLarge verifies Content-Length > 25 MB returns too_large.
func TestProbeMediaURL_TooLarge(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	const tooBig = webhookMediaMaxBytes + 1 // 25 MB + 1 byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", "26214401")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	_ = tooBig

	_, err := probeMediaURL(srv.URL + "/big.jpg")
	if err == nil {
		t.Fatal("expected error for oversized media, got nil")
	}
	var mve *mediaValidateError
	if !errors.As(err, &mve) || mve.code != "too_large" {
		t.Errorf("expected too_large, got code=%q err=%v", mve.code, err)
	}
}

// TestProbeMediaURL_HappyPath verifies a valid probe returns ContentType and non-nil PinnedIP.
func TestProbeMediaURL_HappyPath(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png; charset=utf-8")
		w.Header().Set("Content-Length", "2048")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	result, err := probeMediaURL(srv.URL + "/photo.png")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if result.ContentType != "image/png" {
		t.Errorf("expected image/png (params stripped), got %q", result.ContentType)
	}
	if result.PinnedIP == nil {
		t.Error("expected non-nil pinned IP")
	}
	if !net.IP(result.PinnedIP).IsLoopback() {
		t.Errorf("expected loopback pinned IP for httptest server, got %s", result.PinnedIP)
	}
}
