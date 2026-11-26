package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

var specstreamTestNow = func() time.Time { return time.Date(2026, 11, 26, 9, 30, 0, 0, time.UTC) }

func TestSpecStreamSearchRequestAndResponse(t *testing.T) {
	t.Parallel()
	var got url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key-specstream" {
			t.Errorf("Authorization = %q, want Bearer test-key-specstream", auth)
		}
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"query":"current Go release","sources":[
			{"url":"https://go.dev/doc/devel/release","title":"Go releases","site":"go.dev","snippet":" Current release notes. ","read_at":"2026-11-26T09:00:00Z"},
			{"url":"https://go.dev/","title":"","snippet":""}
		]}}`)
	}))
	defer server.Close()

	provider := &specstreamSearchProvider{apiKey: "test-key-specstream", maxResults: 5, client: server.Client(), endpoint: server.URL, now: specstreamTestNow}
	results, err := provider.Search(context.Background(), searchParams{
		Query: "current Go release", Count: 2, Country: "DE", SearchLang: "de", Freshness: "pw",
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ key, want string }{
		{"q", "current Go release after:2026-11-19"},
		{"num", "2"},
		{"speed", "fast"},
		{"country", "DE"},
		{"language", "de"},
	} {
		if v := got.Get(tc.key); v != tc.want {
			t.Errorf("query %s = %q, want %q", tc.key, v, tc.want)
		}
	}
	if strings.Contains(got.Encode(), "test-key-specstream") {
		t.Errorf("API key leaked into the query string: %s", got.Encode())
	}

	want := []searchResult{
		{Title: "Go releases", URL: "https://go.dev/doc/devel/release", Description: "Current release notes."},
		{Title: "https://go.dev/", URL: "https://go.dev/"},
	}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("results = %#v, want %#v", results, want)
	}
}

// SpecStream reads after: and before: in the query as inclusive day bounds,
// so every freshness form the tool accepts, the date range included, maps.
func TestSpecStreamSearchFreshnessMapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		freshness string
		wantQuery string
	}{
		{freshness: "pd", wantQuery: "q after:2026-11-25"},
		{freshness: "pw", wantQuery: "q after:2026-11-19"},
		{freshness: "pm", wantQuery: "q after:2026-10-26"},
		{freshness: "py", wantQuery: "q after:2025-11-26"},
		{freshness: "2024-01-01to2024-01-31", wantQuery: "q after:2024-01-01 before:2024-01-31"},
		{freshness: "2024-02-01to2024-01-01", wantQuery: "q"},
		{freshness: "nonsense", wantQuery: "q"},
		{freshness: "", wantQuery: "q"},
	}
	for _, tc := range tests {
		t.Run(tc.freshness, func(t *testing.T) {
			t.Parallel()
			var gotQuery string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.Query().Get("q")
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"data":{"sources":[]}}`)
			}))
			defer server.Close()

			provider := &specstreamSearchProvider{maxResults: 5, client: server.Client(), endpoint: server.URL, now: specstreamTestNow}
			if _, err := provider.Search(context.Background(), searchParams{Query: "q", Count: 1, Freshness: tc.freshness}); err != nil {
				t.Fatal(err)
			}
			if gotQuery != tc.wantQuery {
				t.Errorf("q = %q, want %q", gotQuery, tc.wantQuery)
			}
		})
	}
}

// The API rejects country and language values that are not two-letter codes,
// so a longer tag is cut and an unusable one is left out of the request.
func TestSpecStreamSearchLocaleCodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                  string
		params                searchParams
		wantCountry, wantLang string
		countrySent, langSent bool
	}{
		{name: "plain codes", params: searchParams{Country: "us", SearchLang: "en"}, wantCountry: "us", wantLang: "en", countrySent: true, langSent: true},
		{name: "language tag", params: searchParams{SearchLang: "zh-hans"}, wantLang: "zh", langSent: true},
		{name: "ui language fallback", params: searchParams{UILang: "pt_BR"}, wantLang: "pt", langSent: true},
		{name: "unusable values", params: searchParams{Country: "ALL", SearchLang: "e"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got url.Values
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.URL.Query()
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"data":{"sources":[]}}`)
			}))
			defer server.Close()

			params := tc.params
			params.Query, params.Count = "q", 1
			provider := &specstreamSearchProvider{maxResults: 5, client: server.Client(), endpoint: server.URL, now: specstreamTestNow}
			if _, err := provider.Search(context.Background(), params); err != nil {
				t.Fatal(err)
			}
			if _, ok := got["country"]; ok != tc.countrySent || got.Get("country") != tc.wantCountry {
				t.Errorf("country = %q (sent %v), want %q (sent %v)", got.Get("country"), ok, tc.wantCountry, tc.countrySent)
			}
			if _, ok := got["language"]; ok != tc.langSent || got.Get("language") != tc.wantLang {
				t.Errorf("language = %q (sent %v), want %q (sent %v)", got.Get("language"), ok, tc.wantLang, tc.langSent)
			}
		})
	}
}

func TestSpecStreamSearchAppliesLimitAndSkipsMissingURL(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"sources":[
			{"title":"missing url","url":"","snippet":"skip"},
			{"title":"one","url":"https://example.com/1","snippet":"one"},
			{"title":"two","url":"https://example.com/2","snippet":"two"}
		]}}`)
	}))
	defer server.Close()

	provider := &specstreamSearchProvider{maxResults: 1, client: server.Client(), endpoint: server.URL, now: specstreamTestNow}
	results, err := provider.Search(context.Background(), searchParams{Query: "q", Count: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Title != "one" {
		t.Fatalf("results = %#v, want first valid result", results)
	}
}

func TestSpecStreamSearchResponseErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "invalid key", status: http.StatusUnauthorized, body: `{"error":{"code":"invalid_token","message":"SpecStream didn't accept this API key or token.","details":{}}}`, want: "returned 401 (invalid_token): SpecStream didn't accept"},
		{name: "no balance", status: http.StatusPaymentRequired, body: `{"error":{"code":"insufficient_balance","message":"Add credit to keep searching."}}`, want: "returned 402 (insufficient_balance)"},
		{name: "server error", status: http.StatusServiceUnavailable, body: ``, want: "returned 503"},
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

			provider := &specstreamSearchProvider{maxResults: 1, client: server.Client(), endpoint: server.URL, now: specstreamTestNow}
			_, err := provider.Search(context.Background(), searchParams{Query: "q", Count: 1})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestSpecStreamSearchPropagatesCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	client := &http.Client{Transport: specstreamRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	provider := &specstreamSearchProvider{maxResults: 1, client: client, endpoint: specstreamSearchEndpoint, now: specstreamTestNow}
	_, err := provider.Search(ctx, searchParams{Query: "q", Count: 1})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
}

// SpecStream is key-gated like Serply, so it sits in the default order and a
// tenant with no key is skipped by BuildChainFromStorage.
func TestSpecStreamProviderIsInDefaultOrder(t *testing.T) {
	if got := NormalizeWebSearchProviderOrder(nil); !containsString(got, searchProviderSpecStream) {
		t.Fatalf("default provider order missing specstream: %v", got)
	}
	got := NormalizeWebSearchProviderOrder([]string{searchProviderSpecStream})
	if len(got) == 0 || got[0] != searchProviderSpecStream {
		t.Fatalf("explicit provider order = %v, want specstream first", got)
	}
	if provider := buildProviderByName(searchProviderSpecStream, "k", 3); provider == nil || provider.Name() != searchProviderSpecStream {
		t.Fatalf("specstream provider construction failed: %T", provider)
	}
}

type specstreamRoundTripFunc func(*http.Request) (*http.Response, error)

func (f specstreamRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
