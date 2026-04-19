package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/crypto"
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
