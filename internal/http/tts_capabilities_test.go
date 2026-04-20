package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"

	"github.com/google/uuid"
)

// capsDescribableProvider is a TTSProvider that implements DescribableProvider.
type capsDescribableProvider struct{ name string }

func (p *capsDescribableProvider) Name() string { return p.name }
func (p *capsDescribableProvider) Synthesize(_ context.Context, _ string, _ audio.TTSOptions) (*audio.SynthResult, error) {
	return nil, nil
}
func (p *capsDescribableProvider) Capabilities() audio.ProviderCapabilities {
	return audio.ProviderCapabilities{
		Provider:    p.name,
		DisplayName: p.name + " TTS",
		Models:      []string{"model-x"},
	}
}

// newCapsMux creates a ServeMux wired with a TTSHandler (which handles capabilities).
func newCapsMux(mgr *audio.Manager) *http.ServeMux {
	h := NewTTSHandler(mgr)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux
}

// TestCapabilitiesHandler_RequiresAuth verifies 401 when no token is provided
// and a gateway token is configured.
func TestCapabilitiesHandler_RequiresAuth(t *testing.T) {
	setupTestToken(t, "caps-test-token")

	mgr := audio.NewManager(audio.ManagerConfig{Primary: "mock"})
	mgr.RegisterTTS(&mockTTSProvider{name: "mock"})
	mux := newCapsMux(mgr)

	req := httptest.NewRequest("GET", "/v1/tts/capabilities", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestCapabilitiesHandler_RoleOperator verifies viewer gets 403, operator gets 200.
func TestCapabilitiesHandler_RoleOperator(t *testing.T) {
	setupTestToken(t, "caps-test-token")

	viewerRaw := "viewer-caps-key"
	operatorRaw := "operator-caps-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(viewerRaw): {
			ID:     uuid.New(),
			Scopes: []string{"operator.read"}, // viewer-level scope
		},
		crypto.HashAPIKey(operatorRaw): {
			ID:     uuid.New(),
			Scopes: []string{"operator.write"}, // operator scope
		},
	})

	mgr := audio.NewManager(audio.ManagerConfig{Primary: "mock"})
	mgr.RegisterTTS(&mockTTSProvider{name: "mock"})
	mux := newCapsMux(mgr)

	// Viewer → 403
	req := httptest.NewRequest("GET", "/v1/tts/capabilities", nil)
	req.Header.Set("Authorization", "Bearer "+viewerRaw)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("viewer: want 403, got %d: %s", rr.Code, rr.Body.String())
	}

	// Operator → 200
	req2 := httptest.NewRequest("GET", "/v1/tts/capabilities", nil)
	req2.Header.Set("Authorization", "Bearer "+operatorRaw)
	rr2 := httptest.NewRecorder()
	mux.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Errorf("operator: want 200, got %d: %s", rr2.Code, rr2.Body.String())
	}
}

// TestCapabilitiesHandler_JSONShape verifies response decodes into {providers: []ProviderCapabilities}.
func TestCapabilitiesHandler_JSONShape(t *testing.T) {
	setupTestToken(t, "caps-test-token")

	operatorRaw := "operator-shape-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(operatorRaw): {
			ID:     uuid.New(),
			Scopes: []string{"operator.write"},
		},
	})

	mgr := audio.NewManager(audio.ManagerConfig{Primary: "describable"})
	mgr.RegisterTTS(&capsDescribableProvider{name: "describable"})
	mux := newCapsMux(mgr)

	req := httptest.NewRequest("GET", "/v1/tts/capabilities", nil)
	req.Header.Set("Authorization", "Bearer "+operatorRaw)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Providers []audio.ProviderCapabilities `json:"providers"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Providers) == 0 {
		t.Error("expected at least 1 provider in response")
	}
	if resp.Providers[0].Provider == "" {
		t.Error("first provider has empty Provider field")
	}
}

// TestCapabilitiesHandler_TenantIsolation verifies capabilities are catalog-level
// (not tenant-scoped) — same response for two different tenant tokens.
func TestCapabilitiesHandler_TenantIsolation(t *testing.T) {
	setupTestToken(t, "caps-test-token")

	tenant1Key := "tenant1-caps-key"
	tenant2Key := "tenant2-caps-key"
	tid1 := uuid.New()
	tid2 := uuid.New()
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(tenant1Key): {
			ID:       uuid.New(),
			Scopes:   []string{"operator.write"},
			TenantID: tid1,
		},
		crypto.HashAPIKey(tenant2Key): {
			ID:       uuid.New(),
			Scopes:   []string{"operator.write"},
			TenantID: tid2,
		},
	})

	mgr := audio.NewManager(audio.ManagerConfig{Primary: "prov-a"})
	mgr.RegisterTTS(&capsDescribableProvider{name: "prov-a"})
	mux := newCapsMux(mgr)

	getProviders := func(token string) []audio.ProviderCapabilities {
		req := httptest.NewRequest("GET", "/v1/tts/capabilities", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("token %s: want 200, got %d", token, rr.Code)
		}
		var resp struct {
			Providers []audio.ProviderCapabilities `json:"providers"`
		}
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp.Providers
	}

	p1 := getProviders(tenant1Key)
	p2 := getProviders(tenant2Key)

	if len(p1) != len(p2) {
		t.Errorf("tenant isolation: different provider counts: %d vs %d", len(p1), len(p2))
	}
	for i := range p1 {
		if p1[i].Provider != p2[i].Provider {
			t.Errorf("tenant isolation: provider[%d] differs: %q vs %q", i, p1[i].Provider, p2[i].Provider)
		}
	}
}

// TestCapabilitiesHandler_BuiltinCatalog verifies that capabilities returns
// the full builtin catalog (5 providers) even when none are registered in the
// manager — frontend needs this so users can configure a fresh provider.
func TestCapabilitiesHandler_BuiltinCatalog(t *testing.T) {
	setupTestToken(t, "")

	mgr := audio.NewManager(audio.ManagerConfig{}) // no providers registered
	mux := newCapsMux(mgr)

	req := httptest.NewRequest("GET", "/v1/tts/capabilities", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Providers []audio.ProviderCapabilities `json:"providers"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := map[string]bool{"openai": true, "elevenlabs": true, "edge": true, "minimax": true, "gemini": true}
	got := make(map[string]bool, len(resp.Providers))
	for _, p := range resp.Providers {
		got[p.Provider] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("builtin catalog missing %q; got providers=%v", name, got)
		}
	}

	// Gemini specifically must expose static voices for the frontend voice picker.
	for _, p := range resp.Providers {
		if p.Provider == "gemini" {
			if len(p.Voices) == 0 {
				t.Error("gemini capabilities returned 0 voices — frontend will fall back to ElevenLabs picker")
			}
			break
		}
	}
}

// TestCapabilitiesHandler_RegisteredOverridesBuiltin verifies that a registered
// provider takes precedence over the builtin catalog entry of the same name.
func TestCapabilitiesHandler_RegisteredOverridesBuiltin(t *testing.T) {
	setupTestToken(t, "")

	mgr := audio.NewManager(audio.ManagerConfig{})
	// Register a stub that returns DisplayName "custom-openai" for provider "openai".
	mgr.RegisterTTS(&capsDescribableProvider{name: "openai"})
	mux := newCapsMux(mgr)

	req := httptest.NewRequest("GET", "/v1/tts/capabilities", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	var resp struct {
		Providers []audio.ProviderCapabilities `json:"providers"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&resp)

	var openaiCount int
	var openaiDisplay string
	for _, p := range resp.Providers {
		if p.Provider == "openai" {
			openaiCount++
			openaiDisplay = p.DisplayName
		}
	}
	if openaiCount != 1 {
		t.Errorf("want exactly 1 openai entry, got %d", openaiCount)
	}
	if openaiDisplay != "openai TTS" {
		t.Errorf("registered provider should override builtin, got DisplayName=%q", openaiDisplay)
	}
}
