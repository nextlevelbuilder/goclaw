package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type validationSystemConfigStore struct {
	mu      sync.Mutex
	data    map[string]string
	setErr  error
	setKeys []string
}

func (s *validationSystemConfigStore) Get(ctx context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data[key], nil
}

func (s *validationSystemConfigStore) Set(ctx context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setKeys = append(s.setKeys, key)
	if s.setErr != nil {
		return s.setErr
	}
	if s.data == nil {
		s.data = map[string]string{}
	}
	s.data[key] = value
	return nil
}

func (s *validationSystemConfigStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

func (s *validationSystemConfigStore) List(ctx context.Context) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out, nil
}

type validationSecretsStore struct {
	mu   sync.Mutex
	data map[string]string
}

func (s *validationSecretsStore) Get(ctx context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data[key], nil
}

func (s *validationSecretsStore) Set(ctx context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		s.data = map[string]string{}
	}
	s.data[key] = value
	return nil
}

func (s *validationSecretsStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

func (s *validationSecretsStore) GetAll(ctx context.Context) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out, nil
}

func newValidationTTSConfigMux(sc store.SystemConfigStore, cs store.ConfigSecretsStore) *http.ServeMux {
	h := NewTTSConfigHandler(sc, cs)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux
}

func TestTTSConfigSave_AcceptsLegacyAndUISchemaAliases(t *testing.T) {
	setupTestToken(t, "")

	sc := &validationSystemConfigStore{data: map[string]string{}}
	cs := &validationSecretsStore{data: map[string]string{}}
	mux := newValidationTTSConfigMux(sc, cs)

	req := httptest.NewRequest("POST", "/v1/tts/config", ttsBody(t, map[string]any{
		"provider":   "elevenlabs",
		"timeout_ms": 1234,
		"elevenlabs": map[string]string{
			"base_url": "https://custom.elevenlabs.invalid",
			"voice_id": "voice-123",
			"model_id": "model-123",
			"api_key":  "xi-test",
		},
		"edge": map[string]any{
			"voice":   "en-US-AvaMultilingualNeural",
			"rate":    "+15%",
			"enabled": false,
		},
	}))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := sc.data["tts.elevenlabs.api_base"]; got != "https://custom.elevenlabs.invalid" {
		t.Fatalf("want elevenlabs api_base persisted, got %q", got)
	}
	if got := sc.data["tts.elevenlabs.voice"]; got != "voice-123" {
		t.Fatalf("want elevenlabs voice persisted, got %q", got)
	}
	if got := sc.data["tts.elevenlabs.model"]; got != "model-123" {
		t.Fatalf("want elevenlabs model persisted, got %q", got)
	}
	if got := sc.data["tts.timeout_ms"]; got != "1234" {
		t.Fatalf("want timeout_ms persisted, got %q", got)
	}
	if got := sc.data["tts.edge.voice"]; got != "en-US-AvaMultilingualNeural" {
		t.Fatalf("want edge voice persisted, got %q", got)
	}
	if got := sc.data["tts.edge.rate"]; got != "+15%" {
		t.Fatalf("want edge rate persisted, got %q", got)
	}
	if got := sc.data["tts.edge.enabled"]; got != "false" {
		t.Fatalf("want edge enabled persisted, got %q", got)
	}
	if got := cs.data["tts.elevenlabs.api_key"]; got != "xi-test" {
		t.Fatalf("want elevenlabs api_key persisted, got %q", got)
	}
}

func TestTTSConfigGet_RoundTripsCompatibilityFields(t *testing.T) {
	setupTestToken(t, "")

	sc := &validationSystemConfigStore{data: map[string]string{
		"tts.provider":            "elevenlabs",
		"tts.timeout_ms":          "1234",
		"tts.elevenlabs.api_base": "https://custom.elevenlabs.invalid",
		"tts.elevenlabs.voice":    "voice-123",
		"tts.elevenlabs.model":    "model-123",
		"tts.edge.voice":          "en-US-AvaMultilingualNeural",
		"tts.edge.rate":           "+15%",
		"tts.edge.enabled":        "false",
	}}
	cs := &validationSecretsStore{data: map[string]string{}}
	mux := newValidationTTSConfigMux(sc, cs)

	req := httptest.NewRequest("GET", "/v1/tts/config", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got, _ := body["timeout_ms"].(float64); got != 1234 {
		t.Fatalf("want timeout_ms 1234, got %v", body["timeout_ms"])
	}
	elevenlabs, _ := body["elevenlabs"].(map[string]any)
	if got, _ := elevenlabs["api_base"].(string); got != "https://custom.elevenlabs.invalid" {
		t.Fatalf("want elevenlabs api_base, got %q", got)
	}
	if got, _ := elevenlabs["base_url"].(string); got != "https://custom.elevenlabs.invalid" {
		t.Fatalf("want elevenlabs base_url alias, got %q", got)
	}
	if got, _ := elevenlabs["voice_id"].(string); got != "voice-123" {
		t.Fatalf("want elevenlabs voice_id alias, got %q", got)
	}
	if got, _ := elevenlabs["model_id"].(string); got != "model-123" {
		t.Fatalf("want elevenlabs model_id alias, got %q", got)
	}
	edge, _ := body["edge"].(map[string]any)
	if got, _ := edge["voice"].(string); got != "en-US-AvaMultilingualNeural" {
		t.Fatalf("want edge voice, got %q", got)
	}
	if got, _ := edge["rate"].(string); got != "+15%" {
		t.Fatalf("want edge rate, got %q", got)
	}
	if got, ok := edge["enabled"].(bool); !ok || got {
		t.Fatalf("want edge enabled=false, got %#v", edge["enabled"])
	}
}

func TestTTSConfigSave_WriteFailuresReturnError(t *testing.T) {
	setupTestToken(t, "")

	sc := &validationSystemConfigStore{data: map[string]string{}, setErr: errors.New("boom")}
	cs := &validationSecretsStore{data: map[string]string{}}
	mux := newValidationTTSConfigMux(sc, cs)

	req := httptest.NewRequest("POST", "/v1/tts/config", ttsBody(t, map[string]any{
		"provider": "openai",
		"openai": map[string]string{
			"api_base": "https://example.invalid/v1",
		},
	}))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d: %s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() == "{\"ok\":true}\n" {
		t.Fatalf("unexpected success body: %q", rr.Body.String())
	}
}
