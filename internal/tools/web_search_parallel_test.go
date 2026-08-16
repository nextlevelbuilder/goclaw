package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParallelSearchRequestAndResponse(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}

		var body struct {
			JSONRPC string `json:"jsonrpc"`
			Method  string `json:"method"`
			Params  struct {
				Name      string `json:"name"`
				Arguments struct {
					Objective     string   `json:"objective"`
					SearchQueries []string `json:"search_queries"`
				} `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.JSONRPC != "2.0" || body.Method != "tools/call" || body.Params.Name != "web_search" {
			t.Errorf("unexpected RPC envelope: %+v", body)
		}
		if body.Params.Arguments.Objective != "current Go release" {
			t.Errorf("objective = %q", body.Params.Arguments.Objective)
		}
		if want := []string{"current Go release"}; !reflect.DeepEqual(body.Params.Arguments.SearchQueries, want) {
			t.Errorf("search_queries = %v, want %v", body.Params.Arguments.SearchQueries, want)
		}

		writeParallelSearchResponse(t, w, `{"results":[
			{"url":"https://go.dev/doc/devel/release","title":"Go releases","excerpts":[" Current release notes. ","More context."]},
			{"url":"https://go.dev/","title":"The Go Programming Language","excerpts":[]}
		]}`)
	}))
	defer server.Close()

	provider := &parallelSearchProvider{maxResults: 5, client: server.Client(), endpoint: server.URL}
	results, err := provider.Search(context.Background(), searchParams{Query: "current Go release", Count: 2})
	if err != nil {
		t.Fatal(err)
	}
	want := []searchResult{
		{Title: "Go releases", URL: "https://go.dev/doc/devel/release", Description: "Current release notes."},
		{Title: "The Go Programming Language", URL: "https://go.dev/"},
	}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("results = %#v, want %#v", results, want)
	}
}

func TestParallelSearchAppliesLimitAndSkipsMissingURL(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeParallelSearchResponse(t, w, `{"results":[
			{"url":"","title":"missing URL","excerpts":["skip"]},
			{"url":"https://example.com/1","title":"one","excerpts":["one"]},
			{"url":"https://example.com/2","title":"two","excerpts":["two"]}
		]}`)
	}))
	defer server.Close()

	provider := &parallelSearchProvider{maxResults: 1, client: server.Client(), endpoint: server.URL}
	results, err := provider.Search(context.Background(), searchParams{Query: "q", Count: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Title != "one" {
		t.Fatalf("results = %#v, want first valid result", results)
	}
}

func TestParallelSearchResponseErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "malformed envelope", body: `{`, want: "parse response"},
		{name: "json-rpc error", body: `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"bad arguments"}}`, want: "error -32602"},
		{name: "tool error", body: `{"jsonrpc":"2.0","id":1,"result":{"isError":true,"content":[]}}`, want: "returned an error"},
		{name: "missing text", body: `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"image"}]}}`, want: "no text results"},
		{name: "malformed nested results", body: `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"{"}]}}`, want: "parse search results"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()

			provider := &parallelSearchProvider{maxResults: 1, client: server.Client(), endpoint: server.URL}
			_, err := provider.Search(context.Background(), searchParams{Query: "q", Count: 1})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestParallelSearchHTTPError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	provider := &parallelSearchProvider{maxResults: 1, client: server.Client(), endpoint: server.URL}
	_, err := provider.Search(context.Background(), searchParams{Query: "q", Count: 1})
	if err == nil || !strings.Contains(err.Error(), "returned 503") {
		t.Fatalf("error = %v, want status 503", err)
	}
}

func TestParallelSearchPropagatesCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	client := &http.Client{Transport: parallelRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	provider := &parallelSearchProvider{maxResults: 1, client: client, endpoint: "https://example.com/mcp"}
	_, err := provider.Search(ctx, searchParams{Query: "q", Count: 1})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
}

func TestParallelProviderIsExplicitOnly(t *testing.T) {
	if got := NormalizeWebSearchProviderOrder(nil); containsString(got, searchProviderParallel) {
		t.Fatalf("default provider order unexpectedly contains parallel: %v", got)
	}
	got := NormalizeWebSearchProviderOrder([]string{searchProviderParallel})
	if len(got) == 0 || got[0] != searchProviderParallel {
		t.Fatalf("explicit provider order = %v, want parallel first", got)
	}
	if provider := buildProviderByName(searchProviderParallel, "", 3); provider == nil || provider.Name() != searchProviderParallel {
		t.Fatalf("parallel provider construction failed: %T", provider)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type parallelRoundTripFunc func(*http.Request) (*http.Response, error)

func (f parallelRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func writeParallelSearchResponse(t *testing.T, w http.ResponseWriter, payload string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"result": map[string]any{
			"content": []map[string]string{{"type": "text", "text": payload}},
		},
	}); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
