package http

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/audio"
	geminiPkg "github.com/nextlevelbuilder/goclaw/internal/audio/gemini"
	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestTestConnection_MissingProvider verifies 400 when provider field is missing.
func TestTestConnection_MissingProvider(t *testing.T) {
	setupTestToken(t, "") // dev mode

	mgr := audio.NewManager(audio.ManagerConfig{})
	mux := newTTSMux(mgr)

	req := httptest.NewRequest("POST", "/v1/tts/test-connection",
		ttsBody(t, map[string]string{}))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if errStr, _ := resp["error"].(string); errStr != "provider is required" {
		t.Errorf("want 'provider is required', got %q", errStr)
	}
}

// TestTestConnection_UnsupportedProvider verifies 400 for unknown provider.
func TestTestConnection_UnsupportedProvider(t *testing.T) {
	setupTestToken(t, "") // dev mode

	mgr := audio.NewManager(audio.ManagerConfig{})
	mux := newTTSMux(mgr)

	req := httptest.NewRequest("POST", "/v1/tts/test-connection",
		ttsBody(t, map[string]string{"provider": "unknown_provider"}))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if errStr, _ := resp["error"].(string); errStr != "unsupported provider: unknown_provider" {
		t.Errorf("want 'unsupported provider: unknown_provider', got %q", errStr)
	}
}

// TestTestConnection_MissingAPIKey verifies 400 when API key is required but missing.
func TestTestConnection_MissingAPIKey(t *testing.T) {
	setupTestToken(t, "") // dev mode

	mgr := audio.NewManager(audio.ManagerConfig{})
	mux := newTTSMux(mgr)

	req := httptest.NewRequest("POST", "/v1/tts/test-connection",
		ttsBody(t, map[string]string{"provider": "openai"}))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if errStr, _ := resp["error"].(string); errStr != "api_key is required for openai" {
		t.Errorf("want 'api_key is required for openai', got %q", errStr)
	}
}

// TestTestConnection_EdgeNoAPIKey verifies Edge provider does not require API key.
func TestTestConnection_EdgeNoAPIKey(t *testing.T) {
	setupTestToken(t, "") // dev mode

	mgr := audio.NewManager(audio.ManagerConfig{})
	mux := newTTSMux(mgr)

	req := httptest.NewRequest("POST", "/v1/tts/test-connection",
		ttsBody(t, map[string]string{"provider": "edge"}))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	// Edge TTS requires edge-tts CLI. In tests, may fail with 502 if CLI not present,
	// but should NOT return 400 "api_key is required".
	if rr.Code == http.StatusBadRequest {
		var resp map[string]any
		json.Unmarshal(rr.Body.Bytes(), &resp)
		if errStr, _ := resp["error"].(string); errStr == "api_key is required for edge" {
			t.Error("edge provider should not require api_key")
		}
	}
	// Either 200 (if edge-tts installed) or 502 (if not) is acceptable.
}

// TestTestConnection_BelowOperator verifies 403 for non-operator roles.
func TestTestConnection_BelowOperator(t *testing.T) {
	setupTestToken(t, ttsTestToken) // token required for auth

	viewerRaw := "test-conn-viewer-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(viewerRaw): {
			ID:     uuid.New(),
			Scopes: []string{"operator.read"},
		},
	})

	mgr := audio.NewManager(audio.ManagerConfig{})
	mux := newTTSMux(mgr)

	req := httptest.NewRequest("POST", "/v1/tts/test-connection",
		ttsBody(t, map[string]string{"provider": "openai", "api_key": "sk-test"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+viewerRaw)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("want 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestCreateEphemeralTTSProvider_Gemini verifies that a Gemini request creates a valid provider.
func TestCreateEphemeralTTSProvider_Gemini(t *testing.T) {
	req := testConnectionRequest{
		Provider: "gemini",
		APIKey:   "test-gemini-key",
		VoiceID:  "Kore",
		ModelID:  "gemini-3.1-flash-tts-preview",
	}
	p, err := createEphemeralTTSProvider(req)
	if err != nil {
		t.Fatalf("createEphemeralTTSProvider error: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if _, ok := p.(*geminiPkg.Provider); !ok {
		t.Errorf("expected *gemini.Provider, got %T", p)
	}
}

// TestCreateEphemeralTTSProvider_GeminiMissingKey verifies missing API key causes 400.
func TestCreateEphemeralTTSProvider_GeminiMissingKey(t *testing.T) {
	setupTestToken(t, "")

	mgr := audio.NewManager(audio.ManagerConfig{})
	mux := newTTSMux(mgr)

	req := httptest.NewRequest("POST", "/v1/tts/test-connection",
		ttsBody(t, map[string]string{"provider": "gemini"}))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestTestConnection_GeminiHappyPath verifies Gemini ephemeral provider synthesizes successfully.
// Uses createEphemeralTTSProvider directly (bypasses SSRF gate which blocks 127.0.0.1).
func TestTestConnection_GeminiHappyPath(t *testing.T) {
	// Spin up a mock Gemini API server.
	pcm := make([]byte, 128)
	b64 := base64.StdEncoding.EncodeToString(pcm)
	geminiResp, _ := json.Marshal(map[string]any{
		"candidates": []map[string]any{
			{"content": map[string]any{
				"parts": []map[string]any{
					{"inlineData": map[string]any{"data": b64}},
				},
			}},
		},
	})
	mockSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(geminiResp)
	}))
	defer mockSrv.Close()

	req := testConnectionRequest{
		Provider: "gemini",
		APIKey:   "test-key",
		APIBase:  mockSrv.URL,
		VoiceID:  "Kore",
		ModelID:  "gemini-2.5-flash-preview-tts",
	}
	p, err := createEphemeralTTSProvider(req)
	if err != nil {
		t.Fatalf("createEphemeralTTSProvider: %v", err)
	}
	result, err := p.Synthesize(t.Context(), "test", audio.TTSOptions{})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if result.MimeType != "audio/wav" {
		t.Errorf("MimeType = %q, want audio/wav", result.MimeType)
	}
}

// TestTestConnection_BlockedAPIBase verifies test-connection reuses provider URL SSRF validation.
func TestTestConnection_BlockedAPIBase(t *testing.T) {
	setupTestToken(t, ttsTestToken)

	operatorRaw := "test-conn-operator-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(operatorRaw): {
			ID:     uuid.New(),
			Scopes: []string{"operator.write"},
		},
	})

	mgr := audio.NewManager(audio.ManagerConfig{})
	mux := newTTSMux(mgr)

	req := httptest.NewRequest("POST", "/v1/tts/test-connection",
		ttsBody(t, map[string]string{
			"provider": "openai",
			"api_key":  "sk-test",
			"api_base": "http://127.0.0.1:8080/v1",
		}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+operatorRaw)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if errStr, _ := resp["error"].(string); errStr != "provider URL cannot point to 127.0.0.1" {
		t.Fatalf("unexpected error: %q", errStr)
	}
}

// Note: TestTestConnection_GeminiInvalidVoice_I18n (M2-b carry-over) is owned
// by Phase 04 — it lands alongside the i18n.T wiring at the HTTP response site.

// TestFillMissingTestSecrets_RestoresMaskedAPIKey verifies that a request with
// the masked sentinel "***" or empty api_key is filled from saved secrets, so
// the user can retest a previously saved provider without retyping the key.
func TestFillMissingTestSecrets_RestoresMaskedAPIKey(t *testing.T) {
	cs := &validationSecretsStore{data: map[string]string{
		"tts.openai.api_key":     "sk-saved",
		"tts.gemini.api_key":     "gm-saved",
		"tts.minimax.api_key":    "mm-saved",
		"tts.minimax.group_id":   "grp-saved",
		"tts.elevenlabs.api_key": "xi-saved",
	}}
	h := NewTTSHandler(audio.NewManager(audio.ManagerConfig{}))
	h.SetStores(&validationSystemConfigStore{data: map[string]string{}}, cs)

	cases := []struct {
		name        string
		req         testConnectionRequest
		wantAPIKey  string
		wantGroupID string
	}{
		{"openai masked", testConnectionRequest{Provider: "openai", APIKey: "***"}, "sk-saved", ""},
		{"openai empty", testConnectionRequest{Provider: "openai"}, "sk-saved", ""},
		{"gemini masked", testConnectionRequest{Provider: "gemini", APIKey: "***"}, "gm-saved", ""},
		{"elevenlabs empty", testConnectionRequest{Provider: "elevenlabs"}, "xi-saved", ""},
		{"minimax fills key+group", testConnectionRequest{Provider: "minimax", APIKey: "***"}, "mm-saved", "grp-saved"},
		{"non-empty key not overwritten", testConnectionRequest{Provider: "openai", APIKey: "sk-typed"}, "sk-typed", ""},
		{"edge no-op", testConnectionRequest{Provider: "edge"}, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := tc.req
			h.fillMissingTestSecrets(t.Context(), &req)
			if req.APIKey != tc.wantAPIKey {
				t.Errorf("APIKey = %q, want %q", req.APIKey, tc.wantAPIKey)
			}
			if req.GroupID != tc.wantGroupID {
				t.Errorf("GroupID = %q, want %q", req.GroupID, tc.wantGroupID)
			}
		})
	}
}

// TestTestConnection_MaskedAPIKey_FallsBackToSaved verifies the handler does
// not return "api_key is required" when the user submits "***" for a provider
// that already has a saved key.
func TestTestConnection_MaskedAPIKey_FallsBackToSaved(t *testing.T) {
	setupTestToken(t, "") // dev mode

	h := NewTTSHandler(audio.NewManager(audio.ManagerConfig{}))
	h.SetStores(
		&validationSystemConfigStore{data: map[string]string{}},
		&validationSecretsStore{data: map[string]string{"tts.openai.api_key": "sk-saved"}},
	)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/v1/tts/test-connection",
		ttsBody(t, map[string]string{"provider": "openai", "api_key": "***"}))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	// Whatever the upstream call returns (502 because api.openai.com unreachable
	// in tests), it must NOT be the 400 "api_key is required" preflight rejection.
	if rr.Code == http.StatusBadRequest {
		var resp map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		if errStr, _ := resp["error"].(string); errStr == "api_key is required for openai" {
			t.Fatalf("masked api_key should fall back to saved secret, got 400: %q", errStr)
		}
	}
}
