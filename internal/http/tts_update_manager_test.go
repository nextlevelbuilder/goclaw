package http

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// TestTTSHandler_UpdateManager_SwapsProvider verifies that UpdateManager changes
// the underlying provider.
func TestTTSHandler_UpdateManager_SwapsProvider(t *testing.T) {
	setupTestToken(t, "") // dev mode

	// Initial provider "mock-a"
	providerA := &mockTTSProvider{name: "mock-a"}
	mgrA := audio.NewManager(audio.ManagerConfig{Primary: "mock-a"})
	mgrA.RegisterTTS(providerA)

	h := NewTTSHandler(mgrA)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// First request uses mock-a
	req1 := httptest.NewRequest("POST", "/v1/tts/synthesize",
		ttsBody(t, map[string]string{"text": "hello"}))
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	mux.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first request: want 200, got %d: %s", rr1.Code, rr1.Body.String())
	}

	// Swap to provider "mock-b"
	providerB := &mockTTSProvider{name: "mock-b"}
	mgrB := audio.NewManager(audio.ManagerConfig{Primary: "mock-b"})
	mgrB.RegisterTTS(providerB)
	h.UpdateManager(mgrB)

	// Second request should use mock-b
	req2 := httptest.NewRequest("POST", "/v1/tts/synthesize",
		ttsBody(t, map[string]string{"text": "hello", "provider": "mock-b"}))
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	mux.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("second request: want 200, got %d: %s", rr2.Code, rr2.Body.String())
	}

	// Old provider should no longer be accessible after swap
	req3 := httptest.NewRequest("POST", "/v1/tts/synthesize",
		ttsBody(t, map[string]string{"text": "hello", "provider": "mock-a"}))
	req3.Header.Set("Content-Type", "application/json")
	rr3 := httptest.NewRecorder()
	mux.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusNotFound {
		t.Errorf("third request (old provider): want 404, got %d", rr3.Code)
	}
}

// TestTTSHandler_UpdateManager_ConcurrentSafe verifies no race condition
// when calling UpdateManager while requests are in flight.
// Note: Uses stateless mock to avoid false race detection in mock's capturedOpts.
func TestTTSHandler_UpdateManager_ConcurrentSafe(t *testing.T) {
	setupTestToken(t, "") // dev mode

	// Use stateless mock to avoid race on capturedOpts
	mgr := audio.NewManager(audio.ManagerConfig{Primary: "mock"})
	mgr.RegisterTTS(&mockTTSProvider{name: "mock", stateless: true})

	h := NewTTSHandler(mgr)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	var wg sync.WaitGroup
	const numRequests = 10

	// Spawn concurrent requests
	for i := range numRequests {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			body := ttsBody(t, map[string]string{"text": "hello"})
			req := httptest.NewRequest("POST", "/v1/tts/synthesize", body)
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)
			// Either 200 or 404 is acceptable during swap
		}(i)
	}

	// Mid-way call UpdateManager with new manager
	wg.Add(1)
	go func() {
		defer wg.Done()
		newMgr := audio.NewManager(audio.ManagerConfig{Primary: "mock-new"})
		newMgr.RegisterTTS(&mockTTSProvider{name: "mock-new", stateless: true})
		h.UpdateManager(newMgr)
	}()

	wg.Wait()
	// If we reach here without panic/race in handler code, test passes.
	// Note: The handler's mutex protects manager access; mock races are test artifacts.
}

// TestTTSHandler_UpdateManager_NilManagerNoop verifies UpdateManager(nil)
// does not panic and keeps the old manager.
func TestTTSHandler_UpdateManager_NilManagerNoop(t *testing.T) {
	setupTestToken(t, "") // dev mode

	provider := &mockTTSProvider{name: "mock"}
	mgr := audio.NewManager(audio.ManagerConfig{Primary: "mock"})
	mgr.RegisterTTS(provider)

	h := NewTTSHandler(mgr)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Call with nil — should not panic
	h.UpdateManager(nil)

	// Request should still work with original manager
	req := httptest.NewRequest("POST", "/v1/tts/synthesize",
		ttsBody(t, map[string]string{"text": "hello"}))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("after nil update: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
}
