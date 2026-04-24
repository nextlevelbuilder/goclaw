package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/audio"
	geminiPkg "github.com/nextlevelbuilder/goclaw/internal/audio/gemini"
	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const ttsTestToken = "tts-test-token"

// --- Mock TTS provider ---

type mockTTSProvider struct {
	name         string
	capturedOpts audio.TTSOptions
	result       *audio.SynthResult
	err          error
	block        bool // if true, blocks until ctx is cancelled (timeout test)
	stateless    bool // if true, don't capture opts (for concurrent tests)
}

func (m *mockTTSProvider) Name() string { return m.name }

func (m *mockTTSProvider) Synthesize(ctx context.Context, text string, opts audio.TTSOptions) (*audio.SynthResult, error) {
	if !m.stateless {
		m.capturedOpts = opts
	}
	if m.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if m.err != nil {
		return nil, m.err
	}
	result := m.result
	if result == nil {
		result = &audio.SynthResult{
			Audio:     []byte("fake-mp3-bytes"),
			Extension: "mp3",
			MimeType:  "audio/mpeg",
		}
	}
	return result, nil
}

// newTTSMux creates a ServeMux wired with a TTSHandler backed by mgr.
func newTTSMux(mgr *audio.Manager) *http.ServeMux {
	h := NewTTSHandler(mgr)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux
}

// ttsBody builds the JSON body for POST /v1/tts/synthesize.
func ttsBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return bytes.NewBuffer(b)
}

// --- Tests ---

// TestSynthesize_Unauthenticated verifies 401 when a gateway token is configured
// but no Authorization header is sent.
func TestSynthesize_Unauthenticated(t *testing.T) {
	setupTestToken(t, ttsTestToken)

	mgr := audio.NewManager(audio.ManagerConfig{Primary: "mock"})
	mgr.RegisterTTS(&mockTTSProvider{name: "mock"})
	mux := newTTSMux(mgr)

	req := httptest.NewRequest("POST", "/v1/tts/synthesize",
		ttsBody(t, map[string]string{"text": "hello"}))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestSynthesize_BelowOperator verifies 403 when the caller has only Viewer role.
// Injects a viewer API key via setupTestCache so resolveAuth picks it up.
func TestSynthesize_BelowOperator(t *testing.T) {
	setupTestToken(t, ttsTestToken) // token set → dev-mode fallback disabled

	viewerRaw := "viewer-api-key-raw"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(viewerRaw): {
			ID:     uuid.New(),
			Scopes: []string{"operator.read"},
		},
	})

	mgr := audio.NewManager(audio.ManagerConfig{Primary: "mock"})
	mgr.RegisterTTS(&mockTTSProvider{name: "mock"})
	mux := newTTSMux(mgr)

	req := httptest.NewRequest("POST", "/v1/tts/synthesize",
		ttsBody(t, map[string]string{"text": "hello"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+viewerRaw)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("want 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestSynthesize_MissingText verifies 400 when text field is empty.
func TestSynthesize_MissingText(t *testing.T) {
	setupTestToken(t, "") // dev mode — everyone is admin

	mgr := audio.NewManager(audio.ManagerConfig{Primary: "mock"})
	mgr.RegisterTTS(&mockTTSProvider{name: "mock"})
	mux := newTTSMux(mgr)

	req := httptest.NewRequest("POST", "/v1/tts/synthesize",
		ttsBody(t, map[string]string{"text": ""}))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestSynthesize_TextTooLong verifies 400 when text exceeds 500 chars.
func TestSynthesize_TextTooLong(t *testing.T) {
	setupTestToken(t, "") // dev mode

	mgr := audio.NewManager(audio.ManagerConfig{Primary: "mock"})
	mgr.RegisterTTS(&mockTTSProvider{name: "mock"})
	mux := newTTSMux(mgr)

	longText := strings.Repeat("a", 501) // 501 runes — one over cap
	req := httptest.NewRequest("POST", "/v1/tts/synthesize",
		ttsBody(t, map[string]string{"text": longText}))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestSynthesize_UnknownProvider verifies 404 when the requested provider is not registered.
func TestSynthesize_UnknownProvider(t *testing.T) {
	setupTestToken(t, "") // dev mode

	mgr := audio.NewManager(audio.ManagerConfig{Primary: "mock"})
	mgr.RegisterTTS(&mockTTSProvider{name: "mock"})
	mux := newTTSMux(mgr)

	req := httptest.NewRequest("POST", "/v1/tts/synthesize",
		ttsBody(t, map[string]string{"text": "hello", "provider": "nonexistent"}))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestSynthesize_InvalidElevenLabsModel verifies 422 for an unknown ElevenLabs model.
func TestSynthesize_InvalidElevenLabsModel(t *testing.T) {
	setupTestToken(t, "") // dev mode

	mgr := audio.NewManager(audio.ManagerConfig{Primary: "elevenlabs"})
	mgr.RegisterTTS(&mockTTSProvider{name: "elevenlabs"})
	mux := newTTSMux(mgr)

	body := map[string]string{
		"text":     "hello",
		"provider": "elevenlabs",
		"model_id": "eleven_totally_fake_model",
	}
	req := httptest.NewRequest("POST", "/v1/tts/synthesize", ttsBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("want 422, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestSynthesize_Success verifies 200 with correct Content-Type and audio body.
func TestSynthesize_Success(t *testing.T) {
	setupTestToken(t, "") // dev mode

	expected := []byte("audio-data-bytes")
	p := &mockTTSProvider{
		name: "mock",
		result: &audio.SynthResult{
			Audio:     expected,
			Extension: "mp3",
			MimeType:  "audio/mpeg",
		},
	}
	mgr := audio.NewManager(audio.ManagerConfig{Primary: "mock"})
	mgr.RegisterTTS(p)
	mux := newTTSMux(mgr)

	req := httptest.NewRequest("POST", "/v1/tts/synthesize",
		ttsBody(t, map[string]string{"text": "hello world"}))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("Content-Type = %q, want audio/mpeg", ct)
	}
	if !bytes.Equal(rr.Body.Bytes(), expected) {
		t.Errorf("body = %q, want %q", rr.Body.Bytes(), expected)
	}
}

// TestSynthesize_Timeout verifies 504 when the provider's context is cancelled.
// We use a pre-cancelled context to trigger context.Canceled immediately from
// the mock, which the handler treats as a timeout (context.DeadlineExceeded or Canceled).
func TestSynthesize_Timeout(t *testing.T) {
	setupTestToken(t, "") // dev mode

	p := &mockTTSProvider{name: "mock", block: true}
	mgr := audio.NewManager(audio.ManagerConfig{Primary: "mock"})
	mgr.RegisterTTS(p)

	h := NewTTSHandler(mgr)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Pre-cancel so the mock's <-ctx.Done() fires immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest("POST", "/v1/tts/synthesize",
		ttsBody(t, map[string]string{"text": "hello"}))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusGatewayTimeout {
		t.Errorf("want 504, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestSynthesize_EdgeHonorsOpts verifies that voice_id is forwarded to the Edge provider,
// proving the C1 fix is wired through the HTTP handler end-to-end.
func TestSynthesize_EdgeHonorsOpts(t *testing.T) {
	setupTestToken(t, "") // dev mode

	edgeMock := &mockTTSProvider{name: "edge"}
	mgr := audio.NewManager(audio.ManagerConfig{Primary: "edge"})
	mgr.RegisterTTS(edgeMock)
	mux := newTTSMux(mgr)

	body := map[string]string{
		"text":     "xin chào",
		"provider": "edge",
		"voice_id": "vi-VN-HoaiMyNeural",
	}
	req := httptest.NewRequest("POST", "/v1/tts/synthesize", ttsBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if edgeMock.capturedOpts.Voice != "vi-VN-HoaiMyNeural" {
		t.Errorf("capturedOpts.Voice = %q, want %q (voice_id not forwarded to provider)",
			edgeMock.capturedOpts.Voice, "vi-VN-HoaiMyNeural")
	}
}

// TestResolveTenantProvider_Gemini verifies that a tenant configured for Gemini
// produces a non-nil provider with no error.
func TestResolveTenantProvider_Gemini(t *testing.T) {
	setupTestToken(t, "")

	sc := &validationSystemConfigStore{data: map[string]string{
		"tts.provider":    "gemini",
		"tts.gemini.voice": "Kore",
		"tts.gemini.model": "gemini-3.1-flash-tts-preview",
	}}
	cs := &validationSecretsStore{data: map[string]string{
		"tts.gemini.api_key": "test-gemini-key",
	}}

	mgr := audio.NewManager(audio.ManagerConfig{})
	h := NewTTSHandler(mgr)
	h.systemConfigs = sc
	h.configSecrets = cs

	provider, name, _, err := h.resolveTenantProvider(context.Background(), "")
	if err != nil {
		t.Fatalf("resolveTenantProvider error: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	if name != "gemini" {
		t.Errorf("name = %q, want gemini", name)
	}
}

// TestLoadSavedSpeakers_Gemini verifies persisted multi-speaker config reactivates
// at synthesize time (M1 fix) so saved Gemini setups don't silently fall through
// to single-voice mode.
func TestLoadSavedSpeakers_Gemini(t *testing.T) {
	ctx := context.Background()
	sc := &validationSystemConfigStore{data: map[string]string{
		"tts.gemini.speakers": `[{"speaker":"Joe","voiceId":"Kore"},{"speaker":"Jane","voiceId":"Puck"}]`,
	}}

	speakers := loadSavedSpeakers(ctx, sc, "gemini")
	if len(speakers) != 2 {
		t.Fatalf("speakers len = %d, want 2", len(speakers))
	}
	if speakers[0].Speaker != "Joe" || speakers[0].VoiceID != "Kore" {
		t.Errorf("speakers[0] = %+v, want {Joe Kore}", speakers[0])
	}
	if speakers[1].Speaker != "Jane" || speakers[1].VoiceID != "Puck" {
		t.Errorf("speakers[1] = %+v, want {Jane Puck}", speakers[1])
	}

	// Nil store → nil result.
	if got := loadSavedSpeakers(ctx, nil, "gemini"); got != nil {
		t.Errorf("nil store: got %v, want nil", got)
	}
	// Non-Gemini provider → nil (other providers don't persist multi-speaker).
	if got := loadSavedSpeakers(ctx, sc, "openai"); got != nil {
		t.Errorf("openai: got %v, want nil", got)
	}
	// Empty blob → nil.
	empty := &validationSystemConfigStore{data: map[string]string{}}
	if got := loadSavedSpeakers(ctx, empty, "gemini"); got != nil {
		t.Errorf("empty: got %v, want nil", got)
	}
	// Malformed JSON → nil (no panic, no error propagation).
	bad := &validationSystemConfigStore{data: map[string]string{"tts.gemini.speakers": "not json"}}
	if got := loadSavedSpeakers(ctx, bad, "gemini"); got != nil {
		t.Errorf("bad json: got %v, want nil", got)
	}
}

// TestSynthesize_ValidElevenLabsModel verifies that a known ElevenLabs model passes through.
func TestSynthesize_ValidElevenLabsModel(t *testing.T) {
	setupTestToken(t, "") // dev mode

	mgr := audio.NewManager(audio.ManagerConfig{Primary: "elevenlabs"})
	mgr.RegisterTTS(&mockTTSProvider{name: "elevenlabs"})
	mux := newTTSMux(mgr)

	body := map[string]string{
		"text":     "hello",
		"provider": "elevenlabs",
		"model_id": "eleven_multilingual_v2",
	}
	req := httptest.NewRequest("POST", "/v1/tts/synthesize", ttsBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestSynthesize_TextOnlyErrorMappedTo422 verifies that ErrTextOnlyResponse
// is mapped to HTTP 422 with the EN i18n message in the response body.
func TestSynthesize_TextOnlyErrorMappedTo422(t *testing.T) {
	setupTestToken(t, "") // dev mode

	mock := &mockTTSProvider{
		name:      "gemini",
		stateless: true,
		err:       fmt.Errorf("wrap: %w", geminiPkg.ErrTextOnlyResponse),
	}
	mgr := audio.NewManager(audio.ManagerConfig{Primary: "gemini"})
	mgr.RegisterProvider(mock)
	mux := newTTSMux(mgr)

	req := httptest.NewRequest("POST", "/v1/tts/synthesize",
		ttsBody(t, map[string]any{"text": "hello", "provider": "gemini"}))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got, _ := resp["error"].(string)
	want := i18n.T("en", i18n.MsgTtsGeminiTextOnly)
	if got != want {
		t.Errorf("want error %q, got %q", want, got)
	}
}

// TestSynthesize_TextOnly_LocaleVI verifies that the VI locale translation
// is returned when Accept-Language: vi is set.
func TestSynthesize_TextOnly_LocaleVI(t *testing.T) {
	setupTestToken(t, "") // dev mode

	mock := &mockTTSProvider{
		name:      "gemini",
		stateless: true,
		err:       fmt.Errorf("wrap: %w", geminiPkg.ErrTextOnlyResponse),
	}
	mgr := audio.NewManager(audio.ManagerConfig{Primary: "gemini"})
	mgr.RegisterProvider(mock)
	mux := newTTSMux(mgr)

	req := httptest.NewRequest("POST", "/v1/tts/synthesize",
		ttsBody(t, map[string]any{"text": "hello", "provider": "gemini"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "vi")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got, _ := resp["error"].(string)
	want := i18n.T("vi", i18n.MsgTtsGeminiTextOnly)
	if got != want {
		t.Errorf("want VI error %q, got %q", want, got)
	}
}

// TestSynthesize_GeminiInvalidVoice_I18n verifies that 422 responses for
// Gemini ErrInvalidVoice use i18n.T(locale, ...) — not err.Error() — so
// VI and ZH callers receive translated messages (M2-b carry-over).
func TestSynthesize_GeminiInvalidVoice_I18n(t *testing.T) {
	setupTestToken(t, "") // dev mode — no bearer token required

	for _, tc := range []struct {
		locale   string
		wantBody string
	}{
		{
			locale:   "en",
			wantBody: i18n.T("en", i18n.MsgTtsGeminiInvalidVoice, "bad-voice"),
		},
		{
			locale:   "vi",
			wantBody: i18n.T("vi", i18n.MsgTtsGeminiInvalidVoice, "bad-voice"),
		},
		{
			locale:   "zh",
			wantBody: i18n.T("zh", i18n.MsgTtsGeminiInvalidVoice, "bad-voice"),
		},
	} {
		t.Run("locale="+tc.locale, func(t *testing.T) {
			// Register a mock "gemini" provider that always returns ErrInvalidVoice.
			mock := &mockTTSProvider{
				name:      "gemini",
				stateless: true,
				err:       fmt.Errorf("wrap: %w", geminiPkg.ErrInvalidVoice),
			}
			mgr := audio.NewManager(audio.ManagerConfig{Primary: "gemini"})
			mgr.RegisterProvider(mock)
			mux := newTTSMux(mgr)

			body := ttsBody(t, map[string]any{
				"text":     "hello",
				"provider": "gemini",
				"voice_id": "bad-voice",
			})
			req := httptest.NewRequest("POST", "/v1/tts/synthesize", body)
			req.Header.Set("Content-Type", "application/json")
			// Locale is extracted from Accept-Language by requireAuth → enrichContext → extractLocale.
			// Setting it directly on the context is overwritten; set the header instead.
			req.Header.Set("Accept-Language", tc.locale)

			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			if rr.Code != http.StatusUnprocessableEntity {
				t.Fatalf("locale=%s: want 422, got %d: %s", tc.locale, rr.Code, rr.Body.String())
			}
			var resp map[string]any
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			got, _ := resp["error"].(string)
			if got != tc.wantBody {
				t.Errorf("locale=%s: want error %q, got %q", tc.locale, tc.wantBody, got)
			}
		})
	}
}
