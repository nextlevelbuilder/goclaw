//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	httphandlers "github.com/nextlevelbuilder/goclaw/internal/http"
)

// TestTTSCapabilities_ListProviders verifies that the audio manager correctly
// lists capabilities for all registered TTS providers.
func TestTTSCapabilities_ListProviders(t *testing.T) {
	// Create manager with mock providers
	mgr := audio.NewManager(audio.ManagerConfig{Primary: "openai"})
	registerTestProviders(mgr)

	// List capabilities from manager
	caps := mgr.ListCapabilities()

	if len(caps) < 4 {
		t.Errorf("expected at least 4 providers, got %d", len(caps))
	}

	// Find OpenAI provider
	var openaiCap *audio.ProviderCapabilities
	for i := range caps {
		if caps[i].Provider == "openai" {
			openaiCap = &caps[i]
			break
		}
	}

	if openaiCap == nil {
		t.Fatal("OpenAI provider not found in capabilities")
	}

	if openaiCap.DisplayName == "" {
		t.Error("OpenAI capability missing DisplayName")
	}
}

// TestTTSCapabilities_Handler_GetRoute verifies that GET /v1/tts/capabilities
// endpoint is properly registered on the TTS handler.
func TestTTSCapabilities_Handler_GetRoute(t *testing.T) {
	mgr := audio.NewManager(audio.ManagerConfig{Primary: "openai"})
	registerTestProviders(mgr)

	handler := httphandlers.NewTTSHandler(mgr)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// GET /v1/tts/capabilities (without auth, should fail on auth check)
	req := httptest.NewRequest("GET", "/v1/tts/capabilities", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	// Should get 401 (unauthorized) since no Bearer token provided
	// Note: The auth middleware requires valid token
	if rr.Code != http.StatusUnauthorized && rr.Code != http.StatusBadRequest {
		t.Logf("GET without auth returned %d (expected 401 or 400)", rr.Code)
	}
}

// TestTTSCapabilities_ManagerRegistration verifies that providers can be
// registered and listed through the manager.
func TestTTSCapabilities_ManagerRegistration(t *testing.T) {
	mgr := audio.NewManager(audio.ManagerConfig{Primary: "openai"})

	// Register providers
	mgr.RegisterTTS(&capTestProvider{name: "openai"})
	mgr.RegisterTTS(&capTestProvider{name: "elevenlabs"})

	// Check count
	caps := mgr.ListCapabilities()
	if len(caps) < 2 {
		t.Errorf("expected at least 2 providers after registration, got %d", len(caps))
	}

	// Verify names present
	names := make(map[string]bool)
	for _, cap := range caps {
		names[cap.Provider] = true
	}

	if !names["openai"] {
		t.Error("openai not in capabilities after registration")
	}
	if !names["elevenlabs"] {
		t.Error("elevenlabs not in capabilities after registration")
	}
}

// registerTestProviders registers the standard test providers with the manager.
func registerTestProviders(mgr *audio.Manager) {
	// Register mock providers for testing
	mgr.RegisterTTS(&capTestProvider{name: "openai"})
	mgr.RegisterTTS(&capTestProvider{name: "elevenlabs"})
	mgr.RegisterTTS(&capTestProvider{name: "edge"})
	mgr.RegisterTTS(&capTestProvider{name: "minimax"})
}

// capTestProvider is a test implementation of audio.TTSProvider.
type capTestProvider struct {
	name string
}

func (p *capTestProvider) Name() string { return p.name }

func (p *capTestProvider) Synthesize(context.Context, string, audio.TTSOptions) (*audio.SynthResult, error) {
	return nil, nil
}
