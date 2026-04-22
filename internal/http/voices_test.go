package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/elevenlabs"
	httpapi "github.com/nextlevelbuilder/goclaw/internal/http"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const voicesTestToken = "voices-test-token"

func TestVoicesHandler_Unauthenticated(t *testing.T) {
	httpapi.InitGatewayToken(voicesTestToken)
	t.Cleanup(func() { httpapi.InitGatewayToken("") })

	cache := audio.NewVoiceCache(time.Hour, 100)
	h := httpapi.NewVoicesHandler(cache, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/v1/voices", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestVoicesHandler_CachedResponse verifies that a cache hit skips the upstream call.
func TestVoicesHandler_CachedResponse(t *testing.T) {
	// No gateway token → dev mode, everyone is admin, MasterTenantID assigned.
	httpapi.InitGatewayToken("")
	t.Cleanup(func() { httpapi.InitGatewayToken("") })

	cache := audio.NewVoiceCache(time.Hour, 100)
	voices := []audio.Voice{{ID: "v1", Name: "Bella", Category: "premade"}}
	// Seed for MasterTenantID — that's what dev-mode auth injects.
	cache.Set(store.MasterTenantID, voices)

	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	p := elevenlabs.NewTTSProvider(elevenlabs.Config{APIKey: "k", BaseURL: upstream.URL})
	h := httpapi.NewVoicesHandlerWithProvider(cache, p)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/v1/voices", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if called {
		t.Error("upstream ElevenLabs should NOT be called on cache hit")
	}
	var resp struct {
		Voices []audio.Voice `json:"voices"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Voices) != 1 || resp.Voices[0].ID != "v1" {
		t.Errorf("unexpected voices: %+v", resp.Voices)
	}
}

// TestVoicesHandler_LiveFetch verifies a cache miss triggers live fetch and caches result.
func TestVoicesHandler_LiveFetch(t *testing.T) {
	httpapi.InitGatewayToken("")
	t.Cleanup(func() { httpapi.InitGatewayToken("") })

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"voices":[{"voice_id":"v2","name":"Adam","category":"premade"}]}`))
	}))
	defer upstream.Close()

	cache := audio.NewVoiceCache(time.Hour, 100)
	p := elevenlabs.NewTTSProvider(elevenlabs.Config{APIKey: "k", BaseURL: upstream.URL})
	h := httpapi.NewVoicesHandlerWithProvider(cache, p)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/v1/voices", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Voices []audio.Voice `json:"voices"`
	}
	json.NewDecoder(rr.Body).Decode(&resp)
	if len(resp.Voices) != 1 || resp.Voices[0].ID != "v2" {
		t.Errorf("unexpected voices: %+v", resp.Voices)
	}

	// Verify cache was populated.
	cached, ok := cache.Get(store.MasterTenantID)
	if !ok || len(cached) != 1 {
		t.Error("expected live fetch result to be cached")
	}
}

// TestVoicesHandler_RefreshUnauthenticated verifies POST /refresh requires auth.
func TestVoicesHandler_RefreshUnauthenticated(t *testing.T) {
	httpapi.InitGatewayToken(voicesTestToken)
	t.Cleanup(func() { httpapi.InitGatewayToken("") })

	cache := audio.NewVoiceCache(time.Hour, 100)
	h := httpapi.NewVoicesHandler(cache, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/v1/voices/refresh", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated refresh, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestVoicesHandler_RefreshAdmin verifies POST /refresh works for admin (dev mode).
func TestVoicesHandler_RefreshAdmin(t *testing.T) {
	httpapi.InitGatewayToken("")
	t.Cleanup(func() { httpapi.InitGatewayToken("") })

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"voices":[{"voice_id":"v3","name":"Rachel"}]}`))
	}))
	defer upstream.Close()

	cache := audio.NewVoiceCache(time.Hour, 100)
	p := elevenlabs.NewTTSProvider(elevenlabs.Config{APIKey: "k", BaseURL: upstream.URL})
	h := httpapi.NewVoicesHandlerWithProvider(cache, p)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/v1/voices/refresh", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin refresh, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Voices []audio.Voice `json:"voices"`
	}
	json.NewDecoder(rr.Body).Decode(&resp)
	if len(resp.Voices) != 1 || resp.Voices[0].ID != "v3" {
		t.Errorf("unexpected voices after refresh: %+v", resp.Voices)
	}
}
