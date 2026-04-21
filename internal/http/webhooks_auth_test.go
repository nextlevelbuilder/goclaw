package http

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// testEncKeyAuth is the AES-256-GCM key used for encrypted_secret in auth tests.
const testEncKeyAuth = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"

// ---- stub store implementations ----

type stubWebhookStore struct {
	byHash map[string]*store.WebhookData
	byID   map[uuid.UUID]*store.WebhookData
}

func newStubWebhookStore(rows ...*store.WebhookData) *stubWebhookStore {
	s := &stubWebhookStore{
		byHash: make(map[string]*store.WebhookData),
		byID:   make(map[uuid.UUID]*store.WebhookData),
	}
	for _, r := range rows {
		s.byHash[r.SecretHash] = r
		s.byID[r.ID] = r
	}
	return s
}

func (s *stubWebhookStore) GetByHash(_ context.Context, h string) (*store.WebhookData, error) {
	r, ok := s.byHash[h]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return r, nil
}
func (s *stubWebhookStore) GetByID(_ context.Context, id uuid.UUID) (*store.WebhookData, error) {
	r, ok := s.byID[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return r, nil
}

// GetByHashUnscoped and GetByIDUnscoped delegate to in-memory maps — same data,
// no tenant filter needed in stub (mirrors production semantics: globally unique hash).
func (s *stubWebhookStore) GetByHashUnscoped(_ context.Context, h string) (*store.WebhookData, error) {
	r, ok := s.byHash[h]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return r, nil
}
func (s *stubWebhookStore) GetByIDUnscoped(_ context.Context, id uuid.UUID) (*store.WebhookData, error) {
	r, ok := s.byID[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return r, nil
}

func (s *stubWebhookStore) Create(_ context.Context, _ *store.WebhookData) error { return nil }
func (s *stubWebhookStore) List(_ context.Context, _ store.WebhookListFilter) ([]store.WebhookData, error) {
	return nil, nil
}
func (s *stubWebhookStore) Update(_ context.Context, _ uuid.UUID, _ map[string]any) error {
	return nil
}
func (s *stubWebhookStore) RotateSecret(_ context.Context, _ uuid.UUID, _, _, _ string) error {
	return nil
}
func (s *stubWebhookStore) Revoke(_ context.Context, _ uuid.UUID) error       { return nil }
func (s *stubWebhookStore) TouchLastUsed(_ context.Context, _ uuid.UUID) error { return nil }

type stubWebhookCallStore struct {
	calls map[string]*store.WebhookCallData // key = idempotency_key
}

func newStubCallStore(calls ...*store.WebhookCallData) *stubWebhookCallStore {
	s := &stubWebhookCallStore{calls: make(map[string]*store.WebhookCallData)}
	for _, c := range calls {
		if c.IdempotencyKey != nil {
			s.calls[*c.IdempotencyKey] = c
		}
	}
	return s
}

func (s *stubWebhookCallStore) GetByIdempotency(_ context.Context, _ uuid.UUID, key string) (*store.WebhookCallData, error) {
	c, ok := s.calls[key]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return c, nil
}
func (s *stubWebhookCallStore) Create(_ context.Context, _ *store.WebhookCallData) error { return nil }
func (s *stubWebhookCallStore) GetByID(_ context.Context, _ uuid.UUID) (*store.WebhookCallData, error) {
	return nil, sql.ErrNoRows
}
func (s *stubWebhookCallStore) UpdateStatus(_ context.Context, _ uuid.UUID, _ map[string]any) error {
	return nil
}
func (s *stubWebhookCallStore) UpdateStatusCAS(_ context.Context, _ uuid.UUID, _ string, _ map[string]any) error {
	return nil
}
func (s *stubWebhookCallStore) ClaimNext(_ context.Context, _ uuid.UUID, _ time.Time) (*store.WebhookCallData, error) {
	return nil, sql.ErrNoRows
}
func (s *stubWebhookCallStore) List(_ context.Context, _ store.WebhookCallListFilter) ([]store.WebhookCallData, error) {
	return nil, nil
}
func (s *stubWebhookCallStore) DeleteOlderThan(_ context.Context, _ uuid.UUID, _ time.Time) (int64, error) {
	return 0, nil
}
func (s *stubWebhookCallStore) ReclaimStale(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

// ---- helpers ----

// makeSecret generates a raw bearer secret and its SHA-256 hash.
func makeSecret() (raw, hashHex string) {
	raw = "wh_testsecretvalue1234567890abcdef"
	h := sha256.Sum256([]byte(raw))
	hashHex = hex.EncodeToString(h[:])
	return
}

// makeHMACSecret returns a raw secret, its hash, an encrypted ciphertext, and the
// raw bytes for HMAC signing. Per K6: HMAC key = raw secret bytes (not hash bytes).
// encKey is the AES-256-GCM encryption key used to encrypt the raw secret at rest.
func makeHMACSecret(encKey string) (secretHash, encryptedSecret string, keyBytes []byte) {
	rawStr := "wh_hmac_raw_secret_for_testing_1234"
	keyBytes = []byte(rawStr)
	h := sha256.Sum256([]byte(rawStr))
	secretHash = hex.EncodeToString(h[:])
	var err error
	encryptedSecret, err = crypto.Encrypt(rawStr, encKey)
	if err != nil {
		panic("makeHMACSecret: encrypt failed: " + err.Error())
	}
	return
}

func signHMAC(keyBytes []byte, ts int64, body []byte) string {
	tsStr := strconv.FormatInt(ts, 10)
	signed := append([]byte(tsStr+"."), body...)
	mac := hmac.New(sha256.New, keyBytes)
	mac.Write(signed)
	return hex.EncodeToString(mac.Sum(nil))
}

func makeWebhook(kind string, opts ...func(*store.WebhookData)) *store.WebhookData {
	raw, hashHex := makeSecret()
	_ = raw
	w := &store.WebhookData{
		ID:              uuid.New(),
		TenantID:        uuid.New(),
		Kind:            kind,
		SecretPrefix:    "wh_test",
		SecretHash:      hashHex,
		RateLimitPerMin: 0, // unlimited by default
	}
	for _, o := range opts {
		o(w)
	}
	return w
}

func withRevoked(w *store.WebhookData) { w.Revoked = true }
func withRequireHMAC(w *store.WebhookData) { w.RequireHMAC = true }
func withLocalhostOnly(w *store.WebhookData) { w.LocalhostOnly = true }
func withRPM(rpm int) func(*store.WebhookData) {
	return func(w *store.WebhookData) { w.RateLimitPerMin = rpm }
}

func makeMiddleware(ws store.WebhookStore, calls store.WebhookCallStore, kind string, maxBody int64) http.Handler {
	return makeMiddlewareWithKey(ws, calls, "", kind, maxBody)
}

func makeMiddlewareWithKey(ws store.WebhookStore, calls store.WebhookCallStore, encKey, kind string, maxBody int64) http.Handler {
	limiter := newWebhookLimiter(0) // tenant limiter disabled
	mw := WebhookAuthMiddleware(ws, calls, limiter, encKey, kind, maxBody)
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return mw(ok)
}

func bearerReq(secret, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/webhooks/llm", bytes.NewBufferString(body))
	r.Header.Set("Authorization", "Bearer "+secret)
	r.Header.Set("Content-Type", "application/json")
	return r
}

func hmacReq(webhookID uuid.UUID, keyBytes []byte, body string, tsOffset int64) *http.Request {
	ts := time.Now().Unix() + tsOffset
	sig := signHMAC(keyBytes, ts, []byte(body))
	sigHeader := fmt.Sprintf("t=%d,v1=%s", ts, sig)
	r := httptest.NewRequest(http.MethodPost, "/v1/webhooks/llm", bytes.NewBufferString(body))
	r.Header.Set("X-GoClaw-Signature", sigHeader)
	r.Header.Set("X-Webhook-Id", webhookID.String())
	r.Header.Set("Content-Type", "application/json")
	return r
}

// ---- tests ----

func TestWebhookAuth_BearerHappyPath(t *testing.T) {
	raw, _ := makeSecret()
	wh := makeWebhook("llm")
	ws := newStubWebhookStore(wh)
	calls := newStubCallStore()

	handler := makeMiddleware(ws, calls, "llm", WebhookMaxBodyLLM)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerReq(raw, `{"input":"hello"}`))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestWebhookAuth_BearerRevoked(t *testing.T) {
	raw, _ := makeSecret()
	wh := makeWebhook("llm", withRevoked)
	ws := newStubWebhookStore(wh)
	calls := newStubCallStore()

	handler := makeMiddleware(ws, calls, "llm", WebhookMaxBodyLLM)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerReq(raw, `{}`))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for revoked, got %d", w.Code)
	}
}

func TestWebhookAuth_BearerRequireHMAC(t *testing.T) {
	raw, _ := makeSecret()
	wh := makeWebhook("llm", withRequireHMAC)
	ws := newStubWebhookStore(wh)
	calls := newStubCallStore()

	handler := makeMiddleware(ws, calls, "llm", WebhookMaxBodyLLM)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerReq(raw, `{}`))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when require_hmac=true but bearer used, got %d", w.Code)
	}
}

func TestWebhookAuth_HMACHappyPath(t *testing.T) {
	secretHash, encSecret, keyBytes := makeHMACSecret(testEncKeyAuth)
	wh := makeWebhook("llm")
	wh.SecretHash = secretHash
	wh.EncryptedSecret = encSecret
	ws := newStubWebhookStore(wh)
	calls := newStubCallStore()

	body := `{"input":"hi"}`
	handler := makeMiddlewareWithKey(ws, calls, testEncKeyAuth, "llm", WebhookMaxBodyLLM)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, hmacReq(wh.ID, keyBytes, body, 0))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid HMAC, got %d: %s", w.Code, w.Body.String())
	}
}

func TestWebhookAuth_HMACTamperedBody(t *testing.T) {
	secretHash, encSecret, keyBytes := makeHMACSecret(testEncKeyAuth)
	wh := makeWebhook("llm")
	wh.SecretHash = secretHash
	wh.EncryptedSecret = encSecret
	ws := newStubWebhookStore(wh)
	calls := newStubCallStore()

	body := `{"input":"legitimate"}`
	ts := time.Now().Unix()
	sig := signHMAC(keyBytes, ts, []byte(body))

	// Send tampered body — signature won't match.
	tamperedBody := `{"input":"tampered"}`
	sigHeader := fmt.Sprintf("t=%d,v1=%s", ts, sig)
	r := httptest.NewRequest(http.MethodPost, "/v1/webhooks/llm", bytes.NewBufferString(tamperedBody))
	r.Header.Set("X-GoClaw-Signature", sigHeader)
	r.Header.Set("X-Webhook-Id", wh.ID.String())

	handler := makeMiddlewareWithKey(ws, calls, testEncKeyAuth, "llm", WebhookMaxBodyLLM)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for tampered body, got %d", w.Code)
	}
}

func TestWebhookAuth_HMACSkewBoundary(t *testing.T) {
	secretHash, encSecret, keyBytes := makeHMACSecret(testEncKeyAuth)
	wh := makeWebhook("llm")
	wh.SecretHash = secretHash
	wh.EncryptedSecret = encSecret
	ws := newStubWebhookStore(wh)
	calls := newStubCallStore()

	body := `{}`
	handler := makeMiddlewareWithKey(ws, calls, testEncKeyAuth, "llm", WebhookMaxBodyLLM)

	// t = now-299 → within window → should pass.
	t.Run("within_skew", func(t *testing.T) {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, hmacReq(wh.ID, keyBytes, body, -299))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 at -299s skew, got %d", w.Code)
		}
	})

	// t = now-301 → outside window → should fail.
	t.Run("outside_skew", func(t *testing.T) {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, hmacReq(wh.ID, keyBytes, body, -301))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 at -301s skew, got %d", w.Code)
		}
	})
}

func TestWebhookAuth_KindMismatch(t *testing.T) {
	raw, _ := makeSecret()
	wh := makeWebhook("message") // webhook is "message" kind
	ws := newStubWebhookStore(wh)
	calls := newStubCallStore()

	// But middleware is configured for "llm" — mismatch.
	handler := makeMiddleware(ws, calls, "llm", WebhookMaxBodyLLM)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, bearerReq(raw, `{}`))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for kind mismatch, got %d", w.Code)
	}
}

func TestWebhookAuth_LocalhostOnlyRemoteIP(t *testing.T) {
	raw, _ := makeSecret()
	wh := makeWebhook("llm", withLocalhostOnly)
	ws := newStubWebhookStore(wh)
	calls := newStubCallStore()

	handler := makeMiddleware(ws, calls, "llm", WebhookMaxBodyLLM)
	w := httptest.NewRecorder()
	r := bearerReq(raw, `{}`)
	r.RemoteAddr = "203.0.113.42:12345" // non-loopback
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-loopback with localhost_only, got %d", w.Code)
	}
}

func TestWebhookAuth_LocalhostOnlyLoopback(t *testing.T) {
	raw, _ := makeSecret()
	wh := makeWebhook("llm", withLocalhostOnly)
	ws := newStubWebhookStore(wh)
	calls := newStubCallStore()

	handler := makeMiddleware(ws, calls, "llm", WebhookMaxBodyLLM)
	w := httptest.NewRecorder()
	r := bearerReq(raw, `{}`)
	r.RemoteAddr = "127.0.0.1:55000" // loopback — should pass
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for loopback with localhost_only, got %d", w.Code)
	}
}

func TestWebhookAuth_RateLimitExceeded(t *testing.T) {
	raw, _ := makeSecret()
	wh := makeWebhook("llm", withRPM(1)) // 1 req/min → burst=1
	ws := newStubWebhookStore(wh)
	calls := newStubCallStore()

	limiter := newWebhookLimiter(0)
	mw := WebhookAuthMiddleware(ws, calls, limiter, "", "llm", WebhookMaxBodyLLM)
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := mw(ok)

	// First request — should pass (burst=1).
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, bearerReq(raw, `{}`))
	if w1.Code != http.StatusOK {
		t.Fatalf("expected first request to pass, got %d", w1.Code)
	}

	// Second request immediately — should be rate limited.
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, bearerReq(raw, `{}`))
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on second request within 1 rpm, got %d", w2.Code)
	}
}

func TestWebhookAuth_BodyTooLarge(t *testing.T) {
	raw, _ := makeSecret()
	wh := makeWebhook("message")
	ws := newStubWebhookStore(wh)
	calls := newStubCallStore()

	// Cap at 256 KB; send 257 KB.
	bigBody := make([]byte, 257*1024)
	for i := range bigBody {
		bigBody[i] = 'x'
	}

	handler := makeMiddleware(ws, calls, "message", WebhookMaxBodyMessage)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/webhooks/message", bytes.NewReader(bigBody))
	r.Header.Set("Authorization", "Bearer "+raw)
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized body, got %d", w.Code)
	}
}

func TestWebhookAuth_IdempotencyReplay(t *testing.T) {
	raw, _ := makeSecret()
	wh := makeWebhook("llm")
	ws := newStubWebhookStore(wh)

	// Pre-load a completed call with matching body hash in canonical JSON format.
	// Post-K2: request_payload is {"body_hash":"<sha256-hex>","meta":{...}} — not the old hex-prefix format.
	body := `{"input":"idempotent"}`
	payload, err := buildAuditPayload([]byte(body), map[string]string{"kind": "llm"})
	if err != nil {
		t.Fatalf("buildAuditPayload: %v", err)
	}
	idKey := "idem-key-abc123"
	existingCall := &store.WebhookCallData{
		ID:             uuid.New(),
		WebhookID:      wh.ID,
		IdempotencyKey: &idKey,
		Status:         "done",
		Response:       []byte(`{"result":"cached"}`),
		RequestPayload: payload,
	}
	calls := newStubCallStore(existingCall)

	handler := makeMiddleware(ws, calls, "llm", WebhookMaxBodyLLM)
	w := httptest.NewRecorder()
	r := bearerReq(raw, body)
	r.Header.Set("Idempotency-Key", idKey)
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 replay, got %d", w.Code)
	}
	got := w.Body.String()
	if got != `{"result":"cached"}` {
		t.Fatalf("expected cached response body, got %q", got)
	}
	if w.Header().Get("X-Idempotency-Replayed") != "true" {
		t.Fatal("expected X-Idempotency-Replayed: true header")
	}
}

func TestWebhookAuth_NoAuthHeader(t *testing.T) {
	wh := makeWebhook("llm")
	ws := newStubWebhookStore(wh)
	calls := newStubCallStore()

	handler := makeMiddleware(ws, calls, "llm", WebhookMaxBodyLLM)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/webhooks/llm", bytes.NewBufferString(`{}`))
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with no auth header, got %d", w.Code)
	}
}

func TestReadLimitedBody_WithinLimit(t *testing.T) {
	body := `{"hello":"world"}`
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	buf, err := readLimitedBody(r, 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(buf) != body {
		t.Fatalf("body mismatch: got %q want %q", buf, body)
	}
	// Verify body is restored.
	restored, _ := io.ReadAll(r.Body)
	if string(restored) != body {
		t.Fatalf("restored body mismatch: got %q", restored)
	}
}

func TestParseHMACHeader(t *testing.T) {
	ts, sig, err := parseHMACHeader("t=1700000000,v1=abcdef1234")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts != 1700000000 {
		t.Fatalf("ts mismatch: %d", ts)
	}
	if sig != "abcdef1234" {
		t.Fatalf("sig mismatch: %q", sig)
	}
}

func TestParseHMACHeader_MissingFields(t *testing.T) {
	cases := []string{
		"",
		"t=1700000000",
		"v1=abcdef",
		"t=bad,v1=abc",
	}
	for _, c := range cases {
		_, _, err := parseHMACHeader(c)
		if err == nil {
			t.Errorf("expected error for header %q, got nil", c)
		}
	}
}

func TestIsLoopback(t *testing.T) {
	cases := []struct {
		addr     string
		loopback bool
	}{
		{"127.0.0.1:8080", true},
		{"[::1]:8080", true},
		{"203.0.113.1:8080", false},
		{"10.0.0.1:8080", false},
		{"", false},
	}
	for _, c := range cases {
		got := isLoopback(c.addr)
		if got != c.loopback {
			t.Errorf("isLoopback(%q) = %v, want %v", c.addr, got, c.loopback)
		}
	}
}

func TestWebhookRateLimiter_TwoTier(t *testing.T) {
	wl := newWebhookLimiter(2) // tenant: 2 rpm

	id := uuid.New().String()
	tid := uuid.New().String()

	// webhook tier unlimited (rpm=0) — passes always.
	if !wl.AllowWebhook(id, 0) {
		t.Fatal("unlimited webhook tier should always allow")
	}

	// Tenant tier: first two pass, third fails.
	if !wl.AllowTenant(tid) {
		t.Fatal("first tenant request should pass")
	}
	if !wl.AllowTenant(tid) {
		t.Fatal("second tenant request (burst=2) should pass")
	}
	if wl.AllowTenant(tid) {
		t.Fatal("third tenant request should be rate limited")
	}
}

// ---- K1: bearer/HMAC succeed without pre-existing tenant in context ----

// TestWebhookAuth_BearerSucceedsWithoutTenantInCtx verifies that bearer auth
// works even when no tenant is present in the incoming request context.
// K1 root-cause: old code called GetByHash (tenant-scoped) before injecting tenant.
func TestWebhookAuth_BearerSucceedsWithoutTenantInCtx(t *testing.T) {
	raw, _ := makeSecret()
	wh := makeWebhook("llm")
	ws := newStubWebhookStore(wh)
	calls := newStubCallStore()

	handler := makeMiddleware(ws, calls, "llm", WebhookMaxBodyLLM)
	w := httptest.NewRecorder()

	// Request context has no tenant — simulates unauthenticated incoming HTTP
	// request (normal case for an inbound webhook from an external caller).
	r := bearerReq(raw, `{"input":"hello"}`)
	if tid := store.TenantIDFromContext(r.Context()); tid != (uuid.UUID{}) {
		t.Skip("context unexpectedly has a tenant — test premise invalid")
	}
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for bearer auth without prior tenant in ctx, got %d: %s", w.Code, w.Body.String())
	}
}

// TestWebhookAuth_HMACSucceedsWithoutTenantInCtx verifies HMAC auth works
// without a pre-existing tenant in context (K1 fix — GetByIDUnscoped).
func TestWebhookAuth_HMACSucceedsWithoutTenantInCtx(t *testing.T) {
	secretHash, encSecret, keyBytes := makeHMACSecret(testEncKeyAuth)
	wh := makeWebhook("llm")
	wh.SecretHash = secretHash
	wh.EncryptedSecret = encSecret
	ws := newStubWebhookStore(wh)
	calls := newStubCallStore()

	body := `{"input":"hi"}`
	handler := makeMiddlewareWithKey(ws, calls, testEncKeyAuth, "llm", WebhookMaxBodyLLM)
	w := httptest.NewRecorder()

	r := hmacReq(wh.ID, keyBytes, body, 0)
	if tid := store.TenantIDFromContext(r.Context()); tid != (uuid.UUID{}) {
		t.Skip("context unexpectedly has a tenant")
	}
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for HMAC auth without prior tenant in ctx, got %d: %s", w.Code, w.Body.String())
	}
}

// ---- K8: HMAC replay-nonce rejection ----

// TestWebhookAuth_HMACReplayRejected verifies that replaying the same HMAC
// signature within the nonce TTL window returns 401.
func TestWebhookAuth_HMACReplayRejected(t *testing.T) {
	secretHash, encSecret, keyBytes := makeHMACSecret(testEncKeyAuth)
	wh := makeWebhook("llm")
	wh.SecretHash = secretHash
	wh.EncryptedSecret = encSecret
	ws := newStubWebhookStore(wh)
	calls := newStubCallStore()

	body := `{"input":"replay-test"}`
	handler := makeMiddlewareWithKey(ws, calls, testEncKeyAuth, "llm", WebhookMaxBodyLLM)

	// Build a single signed request — both calls reuse the same ts+sig.
	ts := time.Now().Unix()
	sig := signHMAC(keyBytes, ts, []byte(body))
	sigHeader := fmt.Sprintf("t=%d,v1=%s", ts, sig)

	makeReq := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/v1/webhooks/llm", bytes.NewBufferString(body))
		r.Header.Set("X-GoClaw-Signature", sigHeader)
		r.Header.Set("X-Webhook-Id", wh.ID.String())
		r.Header.Set("Content-Type", "application/json")
		return r
	}

	// First request — must succeed.
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, makeReq())
	if w1.Code != http.StatusOK {
		t.Fatalf("first HMAC request should succeed, got %d: %s", w1.Code, w1.Body.String())
	}

	// Second request with identical signature — must be rejected as replay.
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, makeReq())
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("replayed HMAC request should return 401, got %d", w2.Code)
	}
}

// ---- K7: IP allowlist enforcement ----

func withIPAllowlist(entries ...string) func(*store.WebhookData) {
	return func(w *store.WebhookData) { w.IPAllowlist = entries }
}

// TestWebhookAuth_IPAllowlistCIDRPass verifies a request from an IP inside a
// CIDR range is allowed.
func TestWebhookAuth_IPAllowlistCIDRPass(t *testing.T) {
	raw, _ := makeSecret()
	wh := makeWebhook("llm", withIPAllowlist("10.0.0.0/8"))
	ws := newStubWebhookStore(wh)
	calls := newStubCallStore()

	handler := makeMiddleware(ws, calls, "llm", WebhookMaxBodyLLM)
	w := httptest.NewRecorder()
	r := bearerReq(raw, `{}`)
	r.RemoteAddr = "10.1.2.3:54321"
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for IP inside CIDR allowlist, got %d: %s", w.Code, w.Body.String())
	}
}

// TestWebhookAuth_IPAllowlistCIDRDeny verifies a request from an IP outside all
// CIDR ranges is rejected with 403.
func TestWebhookAuth_IPAllowlistCIDRDeny(t *testing.T) {
	raw, _ := makeSecret()
	wh := makeWebhook("llm", withIPAllowlist("10.0.0.0/8"))
	ws := newStubWebhookStore(wh)
	calls := newStubCallStore()

	handler := makeMiddleware(ws, calls, "llm", WebhookMaxBodyLLM)
	w := httptest.NewRecorder()
	r := bearerReq(raw, `{}`)
	r.RemoteAddr = "1.2.3.4:54321"
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for IP outside CIDR allowlist, got %d", w.Code)
	}
}

// TestWebhookAuth_IPAllowlistExactMatch verifies single-IP allowlist entries.
func TestWebhookAuth_IPAllowlistExactMatch(t *testing.T) {
	raw, _ := makeSecret()
	wh := makeWebhook("llm", withIPAllowlist("192.168.1.100"))
	ws := newStubWebhookStore(wh)
	calls := newStubCallStore()

	handler := makeMiddleware(ws, calls, "llm", WebhookMaxBodyLLM)

	t.Run("exact_match_pass", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := bearerReq(raw, `{}`)
		r.RemoteAddr = "192.168.1.100:54321"
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for exact IP match, got %d", w.Code)
		}
	})

	t.Run("exact_match_miss", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := bearerReq(raw, `{}`)
		r.RemoteAddr = "192.168.1.101:54321"
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for non-matching IP, got %d", w.Code)
		}
	})
}

// TestWebhookAuth_IPAllowlistEmptyAllowsAll verifies back-compat: empty
// allowlist allows all source IPs.
func TestWebhookAuth_IPAllowlistEmptyAllowsAll(t *testing.T) {
	raw, _ := makeSecret()
	wh := makeWebhook("llm") // no IPAllowlist set
	ws := newStubWebhookStore(wh)
	calls := newStubCallStore()

	handler := makeMiddleware(ws, calls, "llm", WebhookMaxBodyLLM)
	w := httptest.NewRecorder()
	r := bearerReq(raw, `{}`)
	r.RemoteAddr = "203.0.113.99:54321"
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty allowlist (allow-all), got %d", w.Code)
	}
}

// ---- Unit tests for ipAllowed helper ----

func TestIPAllowed(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		allowlist  []string
		want       bool
	}{
		{"cidr_match", "10.1.2.3:8080", []string{"10.0.0.0/8"}, true},
		{"cidr_miss", "1.2.3.4:8080", []string{"10.0.0.0/8"}, false},
		{"exact_match", "192.168.1.5:8080", []string{"192.168.1.5"}, true},
		{"exact_miss", "192.168.1.6:8080", []string{"192.168.1.5"}, false},
		{"multi_second_matches", "172.16.0.1:8080", []string{"10.0.0.0/8", "172.16.0.0/12"}, true},
		{"invalid_cidr_skipped_second_matches", "1.2.3.4:8080", []string{"bad/cidr", "1.2.3.4"}, true},
		{"ipv6_cidr", "[::1]:8080", []string{"::1/128"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ipAllowed(c.remoteAddr, c.allowlist)
			if got != c.want {
				t.Errorf("ipAllowed(%q, %v) = %v, want %v", c.remoteAddr, c.allowlist, got, c.want)
			}
		})
	}
}

// ---- Unit tests for nonce cache ----

func TestWebhookNonceCache_FirstSeenReturnsFalse(t *testing.T) {
	c := newWebhookNonceCache()
	defer c.Stop()
	if c.Seen("key1") {
		t.Fatal("first Seen() call should return false (not a replay)")
	}
}

func TestWebhookNonceCache_SecondSeenReturnsTrue(t *testing.T) {
	c := newWebhookNonceCache()
	defer c.Stop()
	c.Seen("key1")
	if !c.Seen("key1") {
		t.Fatal("second Seen() call with same key should return true (replay)")
	}
}

func TestWebhookNonceCache_DifferentKeysIndependent(t *testing.T) {
	c := newWebhookNonceCache()
	defer c.Stop()
	c.Seen("key1")
	if c.Seen("key2") {
		t.Fatal("different keys should be independent")
	}
}
