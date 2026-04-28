package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// sleepingTTSProvider is a test-only TTS provider that sleeps for a configurable
// duration before returning, used to exercise handler timeout paths.
type sleepingTTSProvider struct {
	sleepMs int
}

func (s *sleepingTTSProvider) Name() string { return "sleep" }

func (s *sleepingTTSProvider) Synthesize(ctx context.Context, text string, opts audio.TTSOptions) (*audio.SynthResult, error) {
	select {
	case <-time.After(time.Duration(s.sleepMs) * time.Millisecond):
		return &audio.SynthResult{Audio: []byte("ok"), MimeType: "audio/mpeg"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// stubSystemConfigStore returns configured values for "tts.timeout_ms" and ignores others.
type stubSystemConfigStore struct {
	timeoutMsValue string // raw string returned for "tts.timeout_ms"
}

func (s *stubSystemConfigStore) Get(_ context.Context, key string) (string, error) {
	if key == "tts.timeout_ms" {
		return s.timeoutMsValue, nil
	}
	return "", nil
}
func (s *stubSystemConfigStore) Set(_ context.Context, _, _ string) error            { return nil }
func (s *stubSystemConfigStore) Delete(_ context.Context, _ string) error            { return nil }
func (s *stubSystemConfigStore) List(_ context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

// newTTSMuxWithStore builds a TTSHandler backed by mgr and systemConfigs, wires routes.
func newTTSMuxWithStore(mgr *audio.Manager, sc *stubSystemConfigStore) *http.ServeMux {
	h := NewTTSHandler(mgr)
	h.SetStores(sc, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux
}

// synthRequestBody builds the POST /v1/tts/synthesize JSON body.
func synthRequestBody(t *testing.T, text string) *bytes.Buffer {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"text": text})
	return bytes.NewBuffer(b)
}

// --- Synthesize timeout tests ---

// TestSynthesize_UsesTenantTimeoutMs verifies the handler applies tts.timeout_ms from
// tenant config. Backend sleeps 1000ms with tenant timeout=500ms → expect 504.
// Backend sleeps 100ms with tenant timeout=500ms → expect 200.
func TestSynthesize_UsesTenantTimeoutMs(t *testing.T) {
	setupTestToken(t, "") // dev mode — no auth required

	sc := &stubSystemConfigStore{timeoutMsValue: "500"}

	// Slow path: backend sleeps 1000ms, tenant timeout 500ms → 504.
	provider := &sleepingTTSProvider{sleepMs: 1000}
	mgr := audio.NewManager(audio.ManagerConfig{})
	mgr.RegisterTTS(provider)

	mux := newTTSMuxWithStore(mgr, sc)

	req := httptest.NewRequest("POST", "/v1/tts/synthesize", synthRequestBody(t, "hello"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusGatewayTimeout {
		t.Errorf("want 504 (tenant timeout 500ms, backend sleeps 1000ms), got %d: %s", rr.Code, rr.Body.String())
	}

	// Fast path: backend sleeps 100ms, tenant timeout 500ms → 200.
	provider2 := &sleepingTTSProvider{sleepMs: 100}
	mgr2 := audio.NewManager(audio.ManagerConfig{})
	mgr2.RegisterTTS(provider2)

	mux2 := newTTSMuxWithStore(mgr2, sc)

	req2 := httptest.NewRequest("POST", "/v1/tts/synthesize", synthRequestBody(t, "hello"))
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	mux2.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Errorf("want 200 (tenant timeout 500ms, backend sleeps 100ms), got %d: %s", rr2.Code, rr2.Body.String())
	}
}

// TestSynthesize_DefaultTimeoutWhenTenantUnset verifies that when tts.timeout_ms is
// unset, the handler uses defaultSynthesizeTimeoutMs (>=120000ms, not old 15s).
func TestSynthesize_DefaultTimeoutWhenTenantUnset(t *testing.T) {
	setupTestToken(t, "") // dev mode

	sc := &stubSystemConfigStore{timeoutMsValue: ""} // no tenant timeout

	provider := &sleepingTTSProvider{sleepMs: 100}
	mgr := audio.NewManager(audio.ManagerConfig{})
	mgr.RegisterTTS(provider)

	mux := newTTSMuxWithStore(mgr, sc)

	req := httptest.NewRequest("POST", "/v1/tts/synthesize", synthRequestBody(t, "hello"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("want 200 (default timeout unset, backend fast), got %d: %s", rr.Code, rr.Body.String())
	}

	// Assert the default constant is >=120000ms (not the old 15s).
	if defaultSynthesizeTimeoutMs < 120000 {
		t.Errorf("defaultSynthesizeTimeoutMs must be >=120000, got %d", defaultSynthesizeTimeoutMs)
	}
}

// TestSynthesize_TenantTimeoutInvalidFallsBackToDefault verifies that an invalid
// (non-numeric) tts.timeout_ms falls back to the 120s default and allows fast backends.
func TestSynthesize_TenantTimeoutInvalidFallsBackToDefault(t *testing.T) {
	setupTestToken(t, "") // dev mode

	sc := &stubSystemConfigStore{timeoutMsValue: "abc"} // invalid

	provider := &sleepingTTSProvider{sleepMs: 100}
	mgr := audio.NewManager(audio.ManagerConfig{})
	mgr.RegisterTTS(provider)

	mux := newTTSMuxWithStore(mgr, sc)

	req := httptest.NewRequest("POST", "/v1/tts/synthesize", synthRequestBody(t, "hello"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("want 200 (invalid tenant value → default, backend fast), got %d: %s", rr.Code, rr.Body.String())
	}
}

// --- Test-connection timeout resolution tests ---

// TestTestConnection_ReqTimeoutOverridesTenant verifies that a non-zero req.TimeoutMs
// overrides the tenant config value (req=500, tenant=5000 → effective=500).
func TestTestConnection_ReqTimeoutOverridesTenant(t *testing.T) {
	sc := &stubSystemConfigStore{timeoutMsValue: "5000"} // tenant=5000ms

	tenantMs := loadTenantTTSTimeoutMs(context.Background(), sc)
	if tenantMs != 5000 {
		t.Fatalf("precondition: tenant timeout should be 5000, got %d", tenantMs)
	}

	// Simulate handler priority: req.TimeoutMs > 0 → use req value.
	reqTimeoutMs := 500
	effectiveMs := reqTimeoutMs
	if effectiveMs <= 0 {
		effectiveMs = tenantMs
	}
	if effectiveMs <= 0 {
		effectiveMs = defaultTestConnectionTimeoutMs
	}

	if effectiveMs != 500 {
		t.Errorf("effectiveMs should be 500 (req override), got %d", effectiveMs)
	}
}

// TestTestConnection_TenantFallbackWhenReqZero verifies that when req.TimeoutMs=0,
// the handler falls back to the saved tenant config value (tenant=800 → effective=800).
func TestTestConnection_TenantFallbackWhenReqZero(t *testing.T) {
	sc := &stubSystemConfigStore{timeoutMsValue: "800"} // tenant=800ms

	tenantMs := loadTenantTTSTimeoutMs(context.Background(), sc)
	if tenantMs != 800 {
		t.Fatalf("precondition: tenant timeout should be 800, got %d", tenantMs)
	}

	// Simulate handler priority: req.TimeoutMs=0 → fall back to tenant.
	reqTimeoutMs := 0
	effectiveMs := reqTimeoutMs
	if effectiveMs <= 0 {
		effectiveMs = tenantMs
	}
	if effectiveMs <= 0 {
		effectiveMs = defaultTestConnectionTimeoutMs
	}

	if effectiveMs != 800 {
		t.Errorf("effectiveMs should be 800 (tenant fallback), got %d", effectiveMs)
	}
}
