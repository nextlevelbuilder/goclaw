package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ---- isValidSettingsJSON (pure validator) ----

func TestIsValidSettingsJSON(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		valid bool
	}{
		{"object", `{"k":"v"}`, true},
		{"empty_object", `{}`, true},
		{"nested", `{"a":{"b":1}}`, true},
		{"null", `null`, true},
		{"array", `[1,2]`, false},
		{"string", `"s"`, false},
		{"number", `42`, false},
		{"bool", `true`, false},
		{"malformed", `{`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := isValidSettingsJSON(json.RawMessage(c.raw))
			if got != c.valid {
				t.Errorf("isValidSettingsJSON(%s) = %v, want %v", c.raw, got, c.valid)
			}
		})
	}
}

// ---- setTenantConfigRequest JSON decode (pointer semantics) ----

func TestSetTenantConfigRequest_DecodeSemantics(t *testing.T) {
	// Enabled present and false — pointer must be non-nil with *Enabled == false.
	var req1 setTenantConfigRequest
	if err := json.Unmarshal([]byte(`{"enabled":false}`), &req1); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req1.Enabled == nil || *req1.Enabled != false {
		t.Errorf("expected enabled=false, got %v", req1.Enabled)
	}
	if req1.Settings != nil {
		t.Errorf("expected settings=nil, got %s", req1.Settings)
	}

	// Settings only.
	var req2 setTenantConfigRequest
	if err := json.Unmarshal([]byte(`{"settings":{"brave":{"max_results":20}}}`), &req2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req2.Enabled != nil {
		t.Errorf("expected enabled=nil, got %v", *req2.Enabled)
	}
	if req2.Settings == nil {
		t.Errorf("expected settings non-nil")
	}

	// Empty body — both nil.
	var req3 setTenantConfigRequest
	if err := json.Unmarshal([]byte(`{}`), &req3); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req3.Enabled != nil || req3.Settings != nil {
		t.Errorf("empty body should produce both nil, got enabled=%v settings=%s", req3.Enabled, req3.Settings)
	}

	// Settings null literal — RawMessage preserves "null".
	var req4 setTenantConfigRequest
	if err := json.Unmarshal([]byte(`{"settings":null}`), &req4); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// json.RawMessage omitempty: RawMessage(nil) is omit; RawMessage{"null"} is present.
	// Go's json package omits null-valued fields with omitempty — req4.Settings will be nil.
	// This is a known edge case: distinguishing "not provided" from "explicit null" isn't
	// possible without a custom unmarshal. The handler treats both as "don't write settings".
	// We document the behavior here.
	_ = req4
}

// ---- Stub store + tenant store for handler tests ----

type stubTenantCfgStore struct {
	mu       sync.Mutex
	enabled  map[string]bool            // toolName → enabled
	settings map[string]json.RawMessage // toolName → settings bytes
}

func newStubTenantCfgStore() *stubTenantCfgStore {
	return &stubTenantCfgStore{
		enabled:  make(map[string]bool),
		settings: make(map[string]json.RawMessage),
	}
}

func (s *stubTenantCfgStore) ListDisabled(_ context.Context, tid uuid.UUID) ([]string, error) {
	if tid == uuid.Nil {
		return nil, store.ErrInvalidTenant
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for k, v := range s.enabled {
		if !v {
			out = append(out, k)
		}
	}
	return out, nil
}

func (s *stubTenantCfgStore) ListAll(_ context.Context, tid uuid.UUID) (map[string]bool, error) {
	if tid == uuid.Nil {
		return nil, store.ErrInvalidTenant
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]bool, len(s.enabled))
	for k, v := range s.enabled {
		out[k] = v
	}
	return out, nil
}

func (s *stubTenantCfgStore) Set(_ context.Context, tid uuid.UUID, name string, enabled bool) error {
	if tid == uuid.Nil {
		return store.ErrInvalidTenant
	}
	s.mu.Lock()
	s.enabled[name] = enabled
	s.mu.Unlock()
	return nil
}

func (s *stubTenantCfgStore) Delete(_ context.Context, tid uuid.UUID, name string) error {
	if tid == uuid.Nil {
		return store.ErrInvalidTenant
	}
	s.mu.Lock()
	delete(s.enabled, name)
	delete(s.settings, name)
	s.mu.Unlock()
	return nil
}

func (s *stubTenantCfgStore) GetSettings(_ context.Context, tid uuid.UUID, name string) (json.RawMessage, error) {
	if tid == uuid.Nil {
		return nil, store.ErrInvalidTenant
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings[name], nil
}

func (s *stubTenantCfgStore) SetSettings(_ context.Context, tid uuid.UUID, name string, raw json.RawMessage) error {
	if tid == uuid.Nil {
		return store.ErrInvalidTenant
	}
	s.mu.Lock()
	if raw == nil {
		delete(s.settings, name)
	} else {
		s.settings[name] = append(json.RawMessage(nil), raw...)
	}
	s.mu.Unlock()
	return nil
}

func (s *stubTenantCfgStore) ListAllSettings(_ context.Context, tid uuid.UUID) (map[string]json.RawMessage, error) {
	if tid == uuid.Nil {
		return nil, store.ErrInvalidTenant
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]json.RawMessage, len(s.settings))
	for k, v := range s.settings {
		out[k] = append(json.RawMessage(nil), v...)
	}
	return out, nil
}

// ---- Handler harness ----

// buildTenantCfgHandler returns a handler wired with a stub store and a
// valid-tenant ctx injected via a wrapping http.HandlerFunc. The wrapper
// sets tenant + owner role so requireTenantAdmin short-circuits (system
// owner bypass) and the inner handler sees a non-nil tid.
func buildTenantCfgHandler(tcfg *stubTenantCfgStore, tid uuid.UUID) (*BuiltinToolsHandler, http.HandlerFunc) {
	h := &BuiltinToolsHandler{tenantCfgStore: tcfg}
	inject := func(w http.ResponseWriter, r *http.Request) {
		ctx := store.WithTenantID(r.Context(), tid)
		ctx = store.WithRole(ctx, store.RoleOwner) // bypass tenant membership check
		r = r.WithContext(ctx)
		// Route to the target handler based on method.
		switch r.Method {
		case http.MethodPut:
			h.handleSetTenantConfig(w, r)
		case http.MethodGet:
			h.handleGetTenantConfig(w, r)
		}
	}
	return h, inject
}

// mustDoPut wires a mux pattern so r.PathValue("name") resolves correctly.
func mustDo(t *testing.T, inject http.HandlerFunc, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(method+" /v1/tools/builtin/{name}/tenant-config", inject)
	req := httptest.NewRequest(method, "/v1/tools/builtin/web_search/tenant-config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// ---- PUT handler tests ----

func TestPutTenantConfig_EnabledOnly_PreservesSettings(t *testing.T) {
	tcfg := newStubTenantCfgStore()
	tid := uuid.New()
	// Pre-seed settings.
	_ = tcfg.SetSettings(context.Background(), tid, "web_search", json.RawMessage(`{"k":"before"}`))

	_, inject := buildTenantCfgHandler(tcfg, tid)
	rec := mustDo(t, inject, http.MethodPut, `{"enabled":true}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !tcfg.enabled["web_search"] {
		t.Errorf("expected enabled=true persisted")
	}
	// Settings untouched.
	if string(tcfg.settings["web_search"]) != `{"k":"before"}` {
		t.Errorf("settings lost: %s", tcfg.settings["web_search"])
	}
}

func TestPutTenantConfig_SettingsOnly_PreservesEnabled(t *testing.T) {
	tcfg := newStubTenantCfgStore()
	tid := uuid.New()
	_ = tcfg.Set(context.Background(), tid, "web_search", true)

	_, inject := buildTenantCfgHandler(tcfg, tid)
	rec := mustDo(t, inject, http.MethodPut, `{"settings":{"brave":{"max_results":20}}}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := string(tcfg.settings["web_search"]); !strings.Contains(got, "brave") {
		t.Errorf("settings not persisted, got: %s", got)
	}
	// Enabled untouched.
	if !tcfg.enabled["web_search"] {
		t.Errorf("enabled flag lost")
	}
}

func TestPutTenantConfig_Both(t *testing.T) {
	tcfg := newStubTenantCfgStore()
	tid := uuid.New()
	_, inject := buildTenantCfgHandler(tcfg, tid)

	rec := mustDo(t, inject, http.MethodPut, `{"enabled":true,"settings":{"k":"v"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !tcfg.enabled["web_search"] || string(tcfg.settings["web_search"]) != `{"k":"v"}` {
		t.Errorf("both fields not persisted: enabled=%v settings=%s", tcfg.enabled["web_search"], tcfg.settings["web_search"])
	}
}

func TestPutTenantConfig_Neither_Returns400(t *testing.T) {
	tcfg := newStubTenantCfgStore()
	_, inject := buildTenantCfgHandler(tcfg, uuid.New())

	rec := mustDo(t, inject, http.MethodPut, `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty body status = %d, want 400", rec.Code)
	}
}

func TestPutTenantConfig_InvalidSettingsJSON_Returns400(t *testing.T) {
	tcfg := newStubTenantCfgStore()
	_, inject := buildTenantCfgHandler(tcfg, uuid.New())

	// Array is valid JSON but not a JSON object.
	rec := mustDo(t, inject, http.MethodPut, `{"settings":[1,2,3]}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("array settings status = %d, want 400", rec.Code)
	}
}

func TestPutTenantConfig_BackwardCompatEnabledOnly(t *testing.T) {
	// Old clients send { enabled: bool } only — must still work.
	tcfg := newStubTenantCfgStore()
	tid := uuid.New()
	_, inject := buildTenantCfgHandler(tcfg, tid)

	rec := mustDo(t, inject, http.MethodPut, `{"enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if tcfg.enabled["web_search"] {
		t.Errorf("expected enabled=false persisted")
	}
}

// ---- GET handler test ----

func TestGetTenantConfig_ReturnsCombinedView(t *testing.T) {
	tcfg := newStubTenantCfgStore()
	tid := uuid.New()
	_ = tcfg.Set(context.Background(), tid, "web_search", true)
	_ = tcfg.SetSettings(context.Background(), tid, "web_search", json.RawMessage(`{"k":"v"}`))

	_, inject := buildTenantCfgHandler(tcfg, tid)
	rec := mustDo(t, inject, http.MethodGet, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		ToolName string          `json:"tool_name"`
		Enabled  *bool           `json:"enabled"`
		Settings json.RawMessage `json:"settings"`
	}
	raw, _ := io.ReadAll(rec.Body)
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.ToolName != "web_search" {
		t.Errorf("tool_name = %s", resp.ToolName)
	}
	if resp.Enabled == nil || !*resp.Enabled {
		t.Errorf("expected enabled=true in response")
	}
	if !bytes.Contains(resp.Settings, []byte(`"k":"v"`)) {
		t.Errorf("settings missing from response: %s", resp.Settings)
	}
}
