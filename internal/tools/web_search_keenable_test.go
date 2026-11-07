package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestKeenableSearchRequestAndResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		apiKey  string
		wantKey string
	}{
		{name: "keyless", apiKey: "", wantKey: ""},
		{name: "keyed", apiKey: "test-key-keenable", wantKey: "test-key-keenable"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %q, want POST", r.Method)
				}
				if got := r.Header.Get("Content-Type"); got != "application/json" {
					t.Errorf("Content-Type = %q, want application/json", got)
				}
				if got := r.Header.Get("X-Keenable-Title"); got != "goclaw" {
					t.Errorf("X-Keenable-Title = %q, want goclaw", got)
				}
				if got := r.Header.Get("X-API-Key"); got != tc.wantKey {
					t.Errorf("X-API-Key = %q, want %q", got, tc.wantKey)
				}

				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				want := map[string]any{"query": "current Go release", "max_results": float64(2)}
				if !reflect.DeepEqual(body, want) {
					t.Errorf("body = %v, want %v", body, want)
				}

				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"results":[
					{"title":"Go releases","url":"https://go.dev/doc/devel/release","description":"","snippet":" Current release\n\nnotes. "},
					{"title":"","url":"https://go.dev/","description":"fallback text","snippet":""}
				]}`)
			}))
			defer server.Close()

			provider := &keenableSearchProvider{apiKey: tc.apiKey, maxResults: 5, client: server.Client(), endpoint: server.URL}
			results, err := provider.Search(context.Background(), searchParams{Query: "current Go release", Count: 2})
			if err != nil {
				t.Fatal(err)
			}
			want := []searchResult{
				{Title: "Go releases", URL: "https://go.dev/doc/devel/release", Description: "Current release notes."},
				{Title: "https://go.dev/", URL: "https://go.dev/", Description: "fallback text"},
			}
			if !reflect.DeepEqual(results, want) {
				t.Fatalf("results = %#v, want %#v", results, want)
			}
		})
	}
}

func TestKeenableEndpointFollowsKey(t *testing.T) {
	if got := newKeenableSearchProvider("", 5).endpoint; got != keenablePublicSearchEndpoint {
		t.Errorf("keyless endpoint = %q, want %q", got, keenablePublicSearchEndpoint)
	}
	if got := newKeenableSearchProvider("k", 5).endpoint; got != keenableSearchEndpoint {
		t.Errorf("keyed endpoint = %q, want %q", got, keenableSearchEndpoint)
	}
}

func TestKeenableDateRange(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		freshness  string
		wantAfter  string
		wantBefore string
	}{
		{freshness: "pd", wantAfter: "2026-03-14"},
		{freshness: "pw", wantAfter: "2026-03-08"},
		{freshness: "pm", wantAfter: "2026-02-15"},
		{freshness: "py", wantAfter: "2025-03-15"},
		{freshness: "2024-01-01to2024-01-31", wantAfter: "2024-01-01", wantBefore: "2024-01-31"},
		{freshness: "2024-02-01to2024-01-01"},
		{freshness: "nonsense"},
		{freshness: ""},
	}
	for _, tc := range tests {
		after, before := keenableDateRange(tc.freshness, now)
		if after != tc.wantAfter || before != tc.wantBefore {
			t.Errorf("keenableDateRange(%q) = (%q, %q), want (%q, %q)", tc.freshness, after, before, tc.wantAfter, tc.wantBefore)
		}
	}
}

func TestKeenableSearchSendsDateFilters(t *testing.T) {
	t.Parallel()
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results":[]}`)
	}))
	defer server.Close()

	provider := &keenableSearchProvider{maxResults: 5, client: server.Client(), endpoint: server.URL}
	if _, err := provider.Search(context.Background(), searchParams{Query: "q", Count: 1, Freshness: "2024-01-01to2024-01-31"}); err != nil {
		t.Fatal(err)
	}
	if body["published_after"] != "2024-01-01" || body["published_before"] != "2024-01-31" {
		t.Errorf("date filters = (%v, %v), want (2024-01-01, 2024-01-31)", body["published_after"], body["published_before"])
	}
}

func TestKeenableSearchAppliesLimitAndSkipsMissingURL(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results":[
			{"title":"missing URL","url":"","snippet":"skip"},
			{"title":"one","url":"https://example.com/1","snippet":"one"},
			{"title":"two","url":"https://example.com/2","snippet":"two"}
		]}`)
	}))
	defer server.Close()

	provider := &keenableSearchProvider{maxResults: 1, client: server.Client(), endpoint: server.URL}
	results, err := provider.Search(context.Background(), searchParams{Query: "q", Count: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Title != "one" {
		t.Fatalf("results = %#v, want first valid result", results)
	}
}

func TestKeenableSearchResponseErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "missing app title", status: http.StatusBadRequest, body: `{"error":"Missing app identifier"}`, want: "returned 400"},
		{name: "invalid key", status: http.StatusUnauthorized, body: `{"error":"Authentication failed"}`, want: "returned 401"},
		{name: "rate limited", status: http.StatusTooManyRequests, body: ``, want: "rate limited (429)"},
		{name: "malformed body", status: http.StatusOK, body: `{`, want: "parse response"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()

			provider := &keenableSearchProvider{maxResults: 1, client: server.Client(), endpoint: server.URL}
			_, err := provider.Search(context.Background(), searchParams{Query: "q", Count: 1})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestKeenableSearchPropagatesCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	client := &http.Client{Transport: keenableRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	provider := &keenableSearchProvider{maxResults: 1, client: client, endpoint: keenablePublicSearchEndpoint}
	_, err := provider.Search(ctx, searchParams{Query: "q", Count: 1})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
}

// Keenable works without a key, so like Parallel it stays out of the default
// order: a stored key alone must not add it to anyone's chain.
func TestKeenableProviderIsExplicitOnly(t *testing.T) {
	if got := NormalizeWebSearchProviderOrder(nil); containsString(got, searchProviderKeenable) {
		t.Fatalf("default provider order unexpectedly contains keenable: %v", got)
	}
	got := NormalizeWebSearchProviderOrder([]string{searchProviderKeenable})
	if len(got) == 0 || got[0] != searchProviderKeenable {
		t.Fatalf("explicit provider order = %v, want keenable first", got)
	}
	if provider := buildProviderByName(searchProviderKeenable, "", 3); provider == nil || provider.Name() != searchProviderKeenable {
		t.Fatalf("keenable provider construction failed: %T", provider)
	}
}

func TestKeylessProvidersStayOutOfDefaultOrder(t *testing.T) {
	for name := range keylessSearchProviders {
		if containsString(defaultSearchProviderOrder, name) {
			t.Errorf("keyless provider %q is in defaultSearchProviderOrder", name)
		}
		if !isKnownSearchProvider(name) || buildProviderByName(name, "", 3) == nil {
			t.Errorf("keyless provider %q is not wired into isKnownSearchProvider/buildProviderByName", name)
		}
	}
}

func TestBuildChainFromStorageKeenableUsesStoredKey(t *testing.T) {
	ctx := store.WithTenantID(context.Background(), uuid.New())
	fake := newFakeSecretsStore()
	if err := fake.Set(ctx, "tools.web.keenable.api_key", "test-key-keenable"); err != nil {
		t.Fatal(err)
	}
	ctx = WithTenantToolSettings(ctx, BuiltinToolSettings{
		"web_search": []byte(`{"provider_order":["keenable"],"keenable":{"enabled":true}}`),
	})

	chain := BuildChainFromStorage(ctx, fake)
	if names := chainNames(chain); !reflect.DeepEqual(names, []string{"keenable", "duckduckgo"}) {
		t.Fatalf("chain = %v, want [keenable duckduckgo]", names)
	}
	p, ok := chain[0].(*keenableSearchProvider)
	if !ok {
		t.Fatalf("chain[0] = %T, want *keenableSearchProvider", chain[0])
	}
	if p.apiKey != "test-key-keenable" || p.endpoint != keenableSearchEndpoint {
		t.Fatalf("provider apiKey=%q endpoint=%q, want stored key on the keyed endpoint", p.apiKey, p.endpoint)
	}
}

// failingSecretsStore answers every Get with err, standing in for a database
// outage or a decrypt failure after an encryption-key rotation.
type failingSecretsStore struct {
	*fakeSecretsStore
	err error
}

func (f failingSecretsStore) Get(context.Context, string) (string, error) { return "", f.err }

func TestBuildChainFromStorageKeenableSecretsErrors(t *testing.T) {
	settings := BuiltinToolSettings{
		"web_search": []byte(`{"provider_order":["parallel","keenable"]}`),
	}
	tests := []struct {
		name      string
		err       error
		wantNames []string
	}{
		// No stored key: Keenable runs keyless.
		{name: "key not found", err: sql.ErrNoRows, wantNames: []string{"parallel", "keenable", "duckduckgo"}},
		// The key may exist but cannot be read: skip Keenable rather than move
		// the tenant onto the shared keyless quota. Parallel reads no secret.
		{name: "store unavailable", err: errors.New("connection refused"), wantNames: []string{"parallel", "duckduckgo"}},
		{name: "decrypt failure", err: fmt.Errorf("decrypt secret %q: %w", "tools.web.keenable.api_key", errors.New("cipher: message authentication failed")), wantNames: []string{"parallel", "duckduckgo"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := WithTenantToolSettings(store.WithTenantID(context.Background(), uuid.New()), settings)
			secrets := failingSecretsStore{fakeSecretsStore: newFakeSecretsStore(), err: tc.err}
			chain := BuildChainFromStorage(ctx, secrets)
			if names := chainNames(chain); !reflect.DeepEqual(names, tc.wantNames) {
				t.Fatalf("chain = %v, want %v", names, tc.wantNames)
			}
			if errors.Is(tc.err, sql.ErrNoRows) {
				if p := chain[1].(*keenableSearchProvider); p.apiKey != "" || p.endpoint != keenablePublicSearchEndpoint {
					t.Fatalf("provider apiKey=%q endpoint=%q, want keyless public endpoint", p.apiKey, p.endpoint)
				}
			}
		})
	}
}

func TestKeenableRateLimitHoldsBackUntilRetryAfter(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":"Too many requests","retryAfter":120}`)
	}))
	defer server.Close()

	keyless := &keenableSearchProvider{maxResults: 1, client: server.Client(), endpoint: server.URL}
	_, err := keyless.Search(context.Background(), searchParams{Query: "q", Count: 1})
	if err == nil || !strings.Contains(err.Error(), "keyless quota") || !strings.Contains(err.Error(), "next try in 2m0s") {
		t.Fatalf("first error = %v, want keyless rate-limit error with the Retry-After wait", err)
	}

	// A fresh provider, as the chain cache builds every minute, stays held
	// back without sending a request.
	again := &keenableSearchProvider{maxResults: 1, client: server.Client(), endpoint: server.URL}
	_, err = again.Search(context.Background(), searchParams{Query: "q", Count: 1})
	if err == nil || !strings.Contains(err.Error(), "rate limited (429)") {
		t.Fatalf("second error = %v, want rate-limit error", err)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("server hits = %d, want 1 (second call must not re-probe)", got)
	}

	// A stored key draws on its own quota, so it is not held back by the
	// keyless cooldown.
	keyed := &keenableSearchProvider{apiKey: "test-key-keenable", maxResults: 1, client: server.Client(), endpoint: server.URL}
	_, err = keyed.Search(context.Background(), searchParams{Query: "q", Count: 1})
	if err == nil || !strings.Contains(err.Error(), "on the stored API key") {
		t.Fatalf("keyed error = %v, want keyed rate-limit error", err)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("server hits = %d, want 2 (keyed call has its own cooldown)", got)
	}
}

func TestKeenableRetryAfter(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		header string
		want   time.Duration
	}{
		{header: "", want: keenableDefaultCooldown},
		{header: "garbage", want: keenableDefaultCooldown},
		{header: "1", want: time.Second},
		{header: "0", want: time.Second},
		{header: "1800", want: 30 * time.Minute},
		{header: "86400", want: keenableMaxCooldown},
		{header: now.Add(90 * time.Second).Format(http.TimeFormat), want: 90 * time.Second},
		{header: now.Add(-time.Minute).Format(http.TimeFormat), want: time.Second},
	}
	for _, tc := range tests {
		h := http.Header{}
		if tc.header != "" {
			h.Set("Retry-After", tc.header)
		}
		if got := keenableRetryAfter(h, now); got != tc.want {
			t.Errorf("keenableRetryAfter(%q) = %s, want %s", tc.header, got, tc.want)
		}
	}
}

func TestKeenableSnippetTruncationKeepsUTF8Valid(t *testing.T) {
	t.Parallel()
	// 239 ASCII bytes then a 3-byte rune: a byte cut at 240 would split it.
	snippet := strings.Repeat("a", 239) + strings.Repeat("界", 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]string{{"title": "t", "url": "https://example.com", "snippet": snippet}},
		})
	}))
	defer server.Close()

	provider := &keenableSearchProvider{maxResults: 1, client: server.Client(), endpoint: server.URL}
	results, err := provider.Search(context.Background(), searchParams{Query: "q", Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got := results[0].Description; !utf8.ValidString(got) || got != strings.Repeat("a", 239)+"..." {
		t.Fatalf("description = %q, want the ASCII prefix cut before the split rune", got)
	}
	if got := truncateStr("héllo", 2); got != "h..." {
		t.Fatalf("truncateStr = %q, want %q", got, "h...")
	}
}

type keenableRoundTripFunc func(*http.Request) (*http.Response, error)

func (f keenableRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
