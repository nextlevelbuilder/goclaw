package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/decisionmodel"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const dmTestKey = "sk-dm-SECRET-abcdef"

type dmFixture struct {
	mux *http.ServeMux
	sc  *validationSystemConfigStore
	cs  *validationSecretsStore
	h   *DecisionModelHandler
}

func newDMFixture(t *testing.T) dmFixture {
	t.Helper()
	setupTestToken(t, "gateway-token")
	setupTestNoAuthFallback(t, false)
	ts := newMockTenantStore()
	tid := uuid.New()
	ts.addTenant(tid, "acme")
	ts.setUserRole(tid, "admin-user", store.TenantRoleAdmin)
	ts.setUserRole(tid, "viewer-user", store.TenantRoleViewer)
	setupTestTenantStore(t, ts)

	f := dmFixture{
		sc: &validationSystemConfigStore{data: map[string]string{}},
		cs: &validationSecretsStore{data: map[string]string{}},
	}
	f.h = NewDecisionModelHandler(f.sc, f.cs, ts)
	f.mux = http.NewServeMux()
	f.h.RegisterRoutes(f.mux)
	return f
}

func (f dmFixture) do(t *testing.T, method, path, user, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer gateway-token")
	req.Header.Set("X-GoClaw-User-Id", user)
	req.Header.Set("X-GoClaw-Tenant-Id", "acme")
	rr := httptest.NewRecorder()
	f.mux.ServeHTTP(rr, req)
	return rr
}

func TestDecisionModelConfigRequiresTenantAdmin(t *testing.T) {
	f := newDMFixture(t)
	for _, c := range []struct{ method, path, body string }{
		{"GET", "/v1/decision-model/config", ""},
		{"POST", "/v1/decision-model/config", `{"enabled":false}`},
		{"POST", "/v1/decision-model/verify", `{"api_key":"x"}`},
	} {
		if rr := f.do(t, c.method, c.path, "viewer-user", c.body); rr.Code != http.StatusForbidden {
			t.Errorf("viewer %s %s = %d, want 403", c.method, c.path, rr.Code)
		}
	}
	if len(f.sc.data) != 0 || len(f.cs.data) != 0 {
		t.Fatalf("viewer request wrote state: %v %v", f.sc.data, f.cs.data)
	}
}

func TestDecisionModelConfigMasksKeyAndKeepsItOnMaskedSave(t *testing.T) {
	f := newDMFixture(t)
	rr := f.do(t, "POST", "/v1/decision-model/config", "admin-user", `{"enabled":true,"api_key":"`+dmTestKey+`"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("save = %d %s", rr.Code, rr.Body)
	}
	if strings.Contains(rr.Body.String(), dmTestKey) {
		t.Fatal("POST response echoes the key")
	}
	// The key lives only in the encrypted secrets store, never in system_configs.
	if f.cs.data[decisionmodel.KeyAPIKey] != dmTestKey {
		t.Fatalf("secret not stored: %v", f.cs.data)
	}
	for k, v := range f.sc.data {
		if strings.Contains(v, dmTestKey) {
			t.Fatalf("key leaked into system_configs[%s]", k)
		}
	}
	if f.sc.data[decisionmodel.KeyEnabled] != "true" || f.sc.data[decisionmodel.KeyModel] != decisionmodel.DefaultModel {
		t.Fatalf("configs = %v", f.sc.data)
	}

	rr = f.do(t, "GET", "/v1/decision-model/config", "admin-user", "")
	var got decisionModelConfig
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got.APIKey != "***" || !got.Enabled || strings.Contains(rr.Body.String(), dmTestKey) {
		t.Fatalf("GET = %s", rr.Body)
	}

	// Saving the masked value back must leave the stored secret byte-identical.
	if rr := f.do(t, "POST", "/v1/decision-model/config", "admin-user", `{"enabled":true,"api_key":"***","timeout_ms":2000}`); rr.Code != http.StatusOK {
		t.Fatalf("masked save = %d %s", rr.Code, rr.Body)
	}
	if f.cs.data[decisionmodel.KeyAPIKey] != dmTestKey || f.sc.data[decisionmodel.KeyTimeoutMS] != "2000" {
		t.Fatalf("after masked save: secret=%q configs=%v", f.cs.data[decisionmodel.KeyAPIKey], f.sc.data)
	}
}

func TestDecisionModelConfigRejectsUnsafeWrites(t *testing.T) {
	f := newDMFixture(t)
	for name, body := range map[string]string{
		"loopback endpoint":  `{"endpoint":"https://127.0.0.1/v1","api_key":"k"}`,
		"metadata endpoint":  `{"endpoint":"https://169.254.169.254/latest","api_key":"k"}`,
		"plain http":         `{"endpoint":"http://api.typesafe.ai/v1/systemone","api_key":"k"}`,
		"timeout too low":    `{"timeout_ms":5,"api_key":"k"}`,
		"state too large":    `{"max_state_chars":999999,"api_key":"k"}`,
		"enable without key": `{"enabled":true}`,
	} {
		if rr := f.do(t, "POST", "/v1/decision-model/config", "admin-user", body); rr.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (%s)", name, rr.Code, rr.Body)
		}
	}
	if len(f.sc.data) != 0 || len(f.cs.data) != 0 {
		t.Fatalf("rejected writes persisted state: %v %v", f.sc.data, f.cs.data)
	}
}

type stubDecider struct {
	dec decisionmodel.Decision
	err error
	got decisionmodel.Config
}

func (s *stubDecider) Decide(context.Context, decisionmodel.State) (decisionmodel.Decision, error) {
	return s.dec, s.err
}

func TestDecisionModelVerifyUsesStoredKeyAndReportsResult(t *testing.T) {
	f := newDMFixture(t)
	f.cs.data[decisionmodel.KeyAPIKey] = dmTestKey
	stub := &stubDecider{dec: decisionmodel.Decision{Option: "coder", Confidence: 0.92, Model: "jev-1.13.0"}}
	f.h.newDecider = func(cfg decisionmodel.Config) (decisionmodel.Decider, error) {
		stub.got = cfg
		return stub, nil
	}
	rr := f.do(t, "POST", "/v1/decision-model/verify", "admin-user", `{"api_key":"***"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("verify = %d %s", rr.Code, rr.Body)
	}
	var got map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got["valid"] != true || got["choice"] != "coder" || got["model"] != "jev-1.13.0" {
		t.Fatalf("verify body = %s", rr.Body)
	}
	if stub.got.APIKey != dmTestKey {
		t.Fatal("masked verify must use the stored key")
	}

	stub.err = decisionmodel.ErrUnavailable
	rr = f.do(t, "POST", "/v1/decision-model/verify", "admin-user", `{"api_key":"***"}`)
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got["valid"] != false || strings.Contains(rr.Body.String(), dmTestKey) {
		t.Fatalf("failed verify body = %s", rr.Body)
	}
}

func TestSystemConfigsRefusesDecisionModelPrefix(t *testing.T) {
	setupTestToken(t, "gateway-token")
	sc := &validationSystemConfigStore{data: map[string]string{}}
	mux := http.NewServeMux()
	NewSystemConfigsHandler(sc, nil).RegisterRoutes(mux)
	for _, key := range []string{decisionmodel.KeyEndpoint, decisionmodel.KeyEnabled, decisionmodel.KeyAPIKey} {
		req := httptest.NewRequest("PUT", "/v1/system-configs/"+key, strings.NewReader(`{"value":"https://127.0.0.1/"}`))
		req.Header.Set("Authorization", "Bearer gateway-token")
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("PUT %s = %d, want 400", key, rr.Code)
		}
	}
	if len(sc.data) != 0 {
		t.Fatalf("generic endpoint wrote decision_model keys: %v", sc.data)
	}
}
