package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/audio"
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
