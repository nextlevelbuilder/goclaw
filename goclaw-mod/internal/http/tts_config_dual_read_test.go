package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ttsConfigDualReadCtx returns a context with a non-nil tenant ID for config handler tests.
func ttsConfigDualReadCtx() context.Context {
	return store.WithTenantID(context.Background(), uuid.New())
}

// buildConfigSaveBody marshals req to JSON and returns a request body reader.
func buildConfigSaveBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return bytes.NewBuffer(b)
}

// configGET issues GET /v1/tts/config with tenant context.
func configGET(mux *http.ServeMux) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/v1/tts/config", nil)
	req = req.WithContext(ttsConfigDualReadCtx())
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

// configPOST issues POST /v1/tts/config with tenant context.
func configPOST(mux *http.ServeMux, body any, t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/v1/tts/config", buildConfigSaveBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ttsConfigDualReadCtx())
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

// newDualReadMux creates a config handler mux with pre-populated stores.
// It bypasses auth by directly setting context — suitable for unit tests.
func newDualReadMux(sc store.SystemConfigStore, cs store.ConfigSecretsStore) *http.ServeMux {
	h := NewTTSConfigHandler(sc, cs)
	mux := http.NewServeMux()
	// Register without requireAuth so we can inject context directly.
	mux.HandleFunc("GET /v1/tts/config", func(w http.ResponseWriter, r *http.Request) {
		h.handleGet(w, r)
	})
	mux.HandleFunc("POST /v1/tts/config", func(w http.ResponseWriter, r *http.Request) {
		h.handleSave(w, r)
	})
	return mux
}

// TestTTSConfig_DualRead_LegacyOnly verifies that legacy flat keys are returned in GET.
func TestTTSConfig_DualRead_LegacyOnly(t *testing.T) {
	sc := &validationSystemConfigStore{data: map[string]string{
		"tts.provider":         "openai",
		"tts.openai.voice":     "alloy",
		"tts.openai.model":     "gpt-4o-mini-tts",
	}}
	cs := &validationSecretsStore{data: map[string]string{}}
	mux := newDualReadMux(sc, cs)

	rr := configGET(mux)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET config: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp ttsConfigResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Provider != "openai" {
		t.Errorf("provider: got %q, want openai", resp.Provider)
	}
	if resp.OpenAI.Voice != "alloy" {
		t.Errorf("openai.voice: got %q, want alloy", resp.OpenAI.Voice)
	}
	if resp.OpenAI.Params != nil {
		t.Errorf("openai.params: expected nil when no blob stored, got %v", resp.OpenAI.Params)
	}
}

// TestTTSConfig_DualRead_BlobOnly verifies that a stored params blob is returned.
func TestTTSConfig_DualRead_BlobOnly(t *testing.T) {
	sc := &validationSystemConfigStore{data: map[string]string{
		"tts.provider":        "elevenlabs",
		"tts.elevenlabs.params": `{"voice_settings":{"stability":0.8}}`,
	}}
	cs := &validationSecretsStore{data: map[string]string{}}
	mux := newDualReadMux(sc, cs)

	rr := configGET(mux)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET config: want 200, got %d", rr.Code)
	}
	var resp ttsConfigResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp.ElevenLabs.Params == nil {
		t.Fatal("elevenlabs.params: expected non-nil")
	}
	vs, _ := resp.ElevenLabs.Params["voice_settings"].(map[string]any)
	if vs == nil {
		t.Fatal("voice_settings: expected map")
	}
	if vs["stability"] != 0.8 {
		t.Errorf("stability: got %v, want 0.8", vs["stability"])
	}
}

// TestTTSConfig_DualRead_BothPresent verifies that blob is returned alongside flat keys.
func TestTTSConfig_DualRead_BothPresent(t *testing.T) {
	sc := &validationSystemConfigStore{data: map[string]string{
		"tts.provider":       "minimax",
		"tts.minimax.voice":  "Wise_Woman",
		"tts.minimax.params": `{"speed":1.5}`,
	}}
	cs := &validationSecretsStore{data: map[string]string{}}
	mux := newDualReadMux(sc, cs)

	rr := configGET(mux)
	var resp ttsConfigResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp.MiniMax.Voice != "Wise_Woman" {
		t.Errorf("minimax.voice: got %q, want Wise_Woman", resp.MiniMax.Voice)
	}
	if resp.MiniMax.Params == nil {
		t.Fatal("minimax.params: expected non-nil")
	}
	if resp.MiniMax.Params["speed"] != 1.5 {
		t.Errorf("speed: got %v, want 1.5", resp.MiniMax.Params["speed"])
	}
}

// TestTTSConfig_DualWrite_SavesBoth verifies that POST saves both legacy flat keys AND blob.
func TestTTSConfig_DualWrite_SavesBoth(t *testing.T) {
	sc := &validationSystemConfigStore{data: map[string]string{}}
	cs := &validationSecretsStore{data: map[string]string{}}
	mux := newDualReadMux(sc, cs)

	body := map[string]any{
		"provider": "openai",
		"openai": map[string]any{
			"voice":     "nova",
			"model":     "gpt-4o-mini-tts",
			"api_key":   "sk-test",
			"params":    map[string]any{"speed": 1.2, "response_format": "opus"},
		},
	}
	rr := configPOST(mux, body, t)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST config: want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Legacy flat key persisted
	if sc.data["tts.openai.voice"] != "nova" {
		t.Errorf("legacy voice key: got %q, want nova", sc.data["tts.openai.voice"])
	}
	// Blob persisted
	blobRaw := sc.data["tts.openai.params"]
	if blobRaw == "" {
		t.Fatal("tts.openai.params blob: expected non-empty")
	}
	var blob map[string]any
	if err := json.Unmarshal([]byte(blobRaw), &blob); err != nil {
		t.Fatalf("unmarshal blob: %v", err)
	}
	if blob["speed"] != 1.2 {
		t.Errorf("blob.speed: got %v, want 1.2", blob["speed"])
	}
	if blob["response_format"] != "opus" {
		t.Errorf("blob.response_format: got %v, want opus", blob["response_format"])
	}
}

// TestTTSConfig_DualWrite_DisjointUnion verifies that blob and flat keys cover disjoint fields.
// Flat keys carry voice/model; blob carries nested provider params.
func TestTTSConfig_DualWrite_DisjointUnion(t *testing.T) {
	sc := &validationSystemConfigStore{data: map[string]string{}}
	cs := &validationSecretsStore{data: map[string]string{}}
	mux := newDualReadMux(sc, cs)

	body := map[string]any{
		"provider": "elevenlabs",
		"elevenlabs": map[string]any{
			"voice_id":  "Rachel",
			"model_id":  "eleven_multilingual_v2",
			"api_key":   "xi-key",
			"params": map[string]any{
				"voice_settings": map[string]any{"stability": 0.6},
				"seed":           42,
			},
		},
	}
	rr := configPOST(mux, body, t)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST config: want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Flat key for voice
	if sc.data["tts.elevenlabs.voice"] != "Rachel" {
		t.Errorf("flat voice: got %q, want Rachel", sc.data["tts.elevenlabs.voice"])
	}
	// Blob contains voice_settings
	blobRaw := sc.data["tts.elevenlabs.params"]
	var blob map[string]any
	json.Unmarshal([]byte(blobRaw), &blob)
	vs, _ := blob["voice_settings"].(map[string]any)
	if vs == nil || vs["stability"] != 0.6 {
		t.Errorf("blob voice_settings.stability: got %v, want 0.6", vs)
	}
	if blob["seed"] != float64(42) {
		t.Errorf("blob.seed: got %v, want 42", blob["seed"])
	}
}
