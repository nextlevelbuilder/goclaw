package http

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

var updateProviderUsageFixtures = flag.Bool("update-provider-usage-fixtures", false,
	"rewrite the UI fixtures from the provider usage handlers' real output")

// UI contract fixtures: both frontends parse these in their tests.
var providerUsageFixtureDirs = []string{
	"../../ui/web/src/pages/providers/hooks/__fixtures__",
	"../../ui/desktop/frontend/src/test/fixtures",
}

// fakeCallStatsTracing is a tracing store that also serves provider call stats.
// Only GetProviderCallStats is implemented; other TracingStore methods panic.
type fakeCallStatsTracing struct {
	store.TracingStore
	stats *store.ProviderCallStats
	err   error
	got   store.ProviderCallStatsQuery
	calls int
}

func (f *fakeCallStatsTracing) GetProviderCallStats(_ context.Context, q store.ProviderCallStatsQuery) (*store.ProviderCallStats, error) {
	f.got = q
	f.calls++
	return f.stats, f.err
}

const providerUsageSecretBody = "HTTP 429: {\"error\":\"rate limited\",\"key\":\"sk-secret-123\"}"

func providerUsageFakeStats() *store.ProviderCallStats {
	at := time.Date(2026, 11, 6, 4, 38, 10, 0, time.UTC)
	return &store.ProviderCallStats{
		Calls: 1284, Errors: 80, LatencyP50MS: 1400, LatencyP95MS: 4800,
		RecentErrors: []store.ProviderErrorSpan{
			{At: at, Error: providerUsageSecretBody},
			{At: at.Add(-5 * time.Minute), Error: "provider call: HTTP 429: slow down"},
			{At: at.Add(-20 * time.Minute), Error: "context deadline exceeded"},
			{At: at.Add(-2 * time.Hour), Error: "HTTP 401: invalid api key"},
		},
	}
}

func setupProvidersViewerToken(t *testing.T) string {
	t.Helper()
	token := "viewer-read-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {ID: uuid.New(), Scopes: []string{"operator.read"}},
	})
	return token
}

func newProviderUsageMux(t *testing.T, tracing store.TracingStore) (*http.ServeMux, *store.LLMProviderData) {
	t.Helper()
	st := newMockProviderStore()
	p := &store.LLMProviderData{
		BaseModel:    store.BaseModel{ID: uuid.New()},
		TenantID:     uuid.New(),
		Name:         "dashscope",
		ProviderType: store.ProviderAnthropicNative,
		Enabled:      true,
	}
	if err := st.CreateProvider(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	h := NewProvidersHandler(st, newMockSecretsStore(), nil, "")
	if tracing != nil {
		h.SetTracingStore(tracing)
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux, p
}

func providerUsageGet(mux *http.ServeMux, token, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func TestProviderUsageDetailValidation(t *testing.T) {
	token := setupProvidersViewerToken(t)
	fake := &fakeCallStatsTracing{stats: providerUsageFakeStats()}
	mux, p := newProviderUsageMux(t, fake)
	base := "/v1/providers/" + p.ID.String() + "/usage-detail"

	cases := []struct {
		name string
		path string
		want int
	}{
		{"missing params", base, http.StatusBadRequest},
		{"missing to", base + "?from=2026-11-01T00:00:00Z", http.StatusBadRequest},
		{"bad from", base + "?from=yesterday&to=2026-11-02T00:00:00Z", http.StatusBadRequest},
		{"to before from", base + "?from=2026-11-02T00:00:00Z&to=2026-11-01T00:00:00Z", http.StatusBadRequest},
		{"range over 90 days", base + "?from=2026-01-01T00:00:00Z&to=2026-11-01T00:00:00Z", http.StatusBadRequest},
		{"bad id", "/v1/providers/not-a-uuid/usage-detail?from=2026-11-01T00:00:00Z&to=2026-11-02T00:00:00Z", http.StatusBadRequest},
		{"unknown provider", "/v1/providers/" + uuid.NewString() + "/usage-detail?from=2026-11-01T00:00:00Z&to=2026-11-02T00:00:00Z", http.StatusNotFound},
		{"ok", base + "?from=2026-11-01T00:00:00Z&to=2026-11-02T00:00:00Z", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if w := providerUsageGet(mux, token, tc.path); w.Code != tc.want {
				t.Fatalf("status = %d, want %d, body = %s", w.Code, tc.want, w.Body.String())
			}
		})
	}
	if fake.calls != 1 {
		t.Errorf("store called %d times, want only for the valid request", fake.calls)
	}
}

func TestProviderUsageDetailScopesQueryAndHidesErrorText(t *testing.T) {
	token := setupProvidersViewerToken(t)
	fake := &fakeCallStatsTracing{stats: providerUsageFakeStats()}
	mux, p := newProviderUsageMux(t, fake)

	w := providerUsageGet(mux, token, "/v1/providers/"+p.ID.String()+
		"/usage-detail?from=2026-11-05T00:00:00%2B07:00&to=2026-11-06T00:00:00Z&limit=500")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	// The query is scoped to the provider row's tenant and name, in UTC.
	if fake.got.TenantID != p.TenantID || fake.got.Provider != p.Name {
		t.Errorf("query scope = %s/%q, want %s/%q", fake.got.TenantID, fake.got.Provider, p.TenantID, p.Name)
	}
	if want := time.Date(2026, 11, 4, 17, 0, 0, 0, time.UTC); !fake.got.From.Equal(want) || fake.got.From.Location() != time.UTC {
		t.Errorf("from = %v, want %v in UTC", fake.got.From, want)
	}

	body := w.Body.String()
	for _, leak := range []string{"sk-secret", "rate limited", "slow down", "invalid api key", "deadline"} {
		if strings.Contains(body, leak) {
			t.Fatalf("response leaks span error text %q: %s", leak, body)
		}
	}

	var got providerUsageDetail
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Calls != 1284 || got.Errors != 80 || got.LatencyMS.P50 != 1400 || got.LatencyMS.P95 != 4800 {
		t.Errorf("totals = %+v", got)
	}
	// Two 429s in the same hour fold into one item; timeout and auth stay separate.
	wantClasses := []string{"rate_limit", "timeout", "auth"}
	var classes []string
	for _, e := range got.RecentErrors {
		classes = append(classes, e.ErrorClass)
	}
	if !slices.Equal(classes, wantClasses) || got.RecentErrors[0].Count != 2 {
		t.Errorf("recent errors = %+v, want classes %v with 2 rate limits first", got.RecentErrors, wantClasses)
	}
}

func TestProviderUsageDetailLimitAndUnavailableStore(t *testing.T) {
	token := setupProvidersViewerToken(t)
	q := "?from=2026-11-01T00:00:00Z&to=2026-11-02T00:00:00Z"

	fake := &fakeCallStatsTracing{stats: providerUsageFakeStats()}
	mux, p := newProviderUsageMux(t, fake)
	w := providerUsageGet(mux, token, "/v1/providers/"+p.ID.String()+"/usage-detail"+q+"&limit=1")
	var got providerUsageDetail
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != http.StatusOK || len(got.RecentErrors) != 1 {
		t.Fatalf("limit=1: status %d, %d items", w.Code, len(got.RecentErrors))
	}

	mux, p = newProviderUsageMux(t, nil)
	if w := providerUsageGet(mux, token, "/v1/providers/"+p.ID.String()+"/usage-detail"+q); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("no tracing store: status = %d, want 503", w.Code)
	}
}

func TestClassifySpanError(t *testing.T) {
	cases := map[string]string{
		"HTTP 429: {\"error\":{\"message\":\"Too many requests\"}}": "rate_limit",
		"fallback: HTTP 429: quota":                                 "rate_limit",
		"HTTP 401: invalid api key":                                 "auth",
		"context deadline exceeded":                                 "timeout",
		"something odd":                                             "unknown",
	}
	for text, want := range cases {
		if got := classifySpanError(text); got != want {
			t.Errorf("classifySpanError(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestParseProviderUsageParamsLimitClamp(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x?from=2026-11-01T00:00:00Z&to=2026-11-02T00:00:00Z&limit=500", nil)
	if _, _, limit, msg := parseProviderUsageParams(r); msg != "" || limit != providerUsageMaxLimit {
		t.Fatalf("limit=500 → %d %q, want clamp to %d", limit, msg, providerUsageMaxLimit)
	}
	r = httptest.NewRequest(http.MethodGet, "/x?from=2026-11-01T00:00:00Z&to=2026-11-02T00:00:00Z&limit=abc", nil)
	if _, _, limit, _ := parseProviderUsageParams(r); limit != providerUsageDefaultLimit {
		t.Fatalf("limit=abc → %d, want default %d", limit, providerUsageDefaultLimit)
	}
}

// TestProviderUsageFixturesMatchHandlerOutput keeps the UI fixtures equal to what
// the handlers really return. Regenerate with:
//
//	go test ./internal/http -run ProviderUsageFixtures -update-provider-usage-fixtures
func TestProviderUsageFixturesMatchHandlerOutput(t *testing.T) {
	token := setupProvidersViewerToken(t)
	fake := &fakeCallStatsTracing{stats: providerUsageFakeStats()}
	mux, p := newProviderUsageMux(t, fake)

	w := providerUsageGet(mux, token, "/v1/providers/"+p.ID.String()+
		"/usage-detail?from=2026-11-05T00:00:00Z&to=2026-11-06T12:00:00Z")
	if w.Code != http.StatusOK {
		t.Fatalf("usage-detail status = %d", w.Code)
	}
	detail := indentJSON(t, w.Body.Bytes())

	// Health: a provider cooling down on a rate limit. The id and timestamps
	// are pinned so the fixture is stable; key set and value types are real.
	until := time.Date(2026, 11, 6, 4, 42, 0, 0, time.UTC)
	health := providerHealthPayload(p, &store.ProviderHealth{
		ConsecutiveFailures: 3, CooldownUntil: &until, LastErrorClass: "rate_limit",
		ErrorCounts: map[string]int{"rate_limit": 14, "timeout": 2}, UpdatedAt: until.Add(-90 * time.Second),
	}, nil)
	health["provider_id"] = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"
	health["cooling_down"] = true
	rawHealth, err := json.Marshal(health)
	if err != nil {
		t.Fatal(err)
	}
	healthJSON := indentJSON(t, rawHealth)

	files := map[string][]byte{"provider-usage-detail.json": detail, "provider-health.json": healthJSON}
	for _, dir := range providerUsageFixtureDirs {
		for name, want := range files {
			path := filepath.Join(dir, name)
			if *updateProviderUsageFixtures {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, want, 0o644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture (regenerate with -update-provider-usage-fixtures): %v", err)
			}
			if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), want) {
				t.Errorf("%s is stale; regenerate with -update-provider-usage-fixtures", path)
			}
		}
	}
}

func indentJSON(t *testing.T, raw []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Indent(&buf, bytes.TrimSpace(raw), "", "  "); err != nil {
		t.Fatal(err)
	}
	buf.WriteByte('\n')
	return buf.Bytes()
}
