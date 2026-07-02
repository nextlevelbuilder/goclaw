package pricing

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFetchOpenRouterCatalogMapsPricingFields(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", req.Method)
		}
		if req.URL.String() != OpenRouterModelsURL {
			t.Fatalf("url = %s, want %s", req.URL.String(), OpenRouterModelsURL)
		}
		body := `{
			"data": [
				{
					"id": "openai/gpt-4o-mini",
					"name": "GPT-4o mini",
					"pricing": {
						"prompt": "0.00000015",
						"completion": "0.0000006",
						"input_cache_read": "0.000000075",
						"input_cache_write": "0.0000003",
						"internal_reasoning": "0.0000002",
						"request": "0",
						"image": "0.001",
						"web_search": "0.01"
					}
				},
				{"name": "missing id"}
			]
		}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}

	entries, err := FetchOpenRouterCatalog(context.Background(), client)
	if err != nil {
		t.Fatalf("FetchOpenRouterCatalog: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	got := entries[0]
	if got.ModelID != "openai/gpt-4o-mini" || got.CanonicalModelID != got.ModelID {
		t.Fatalf("model ids = %q/%q, want openai/gpt-4o-mini", got.ModelID, got.CanonicalModelID)
	}
	assertStringPtr(t, "input", got.Pricing.Input, "0.00000015")
	assertStringPtr(t, "output", got.Pricing.Output, "0.0000006")
	assertStringPtr(t, "cache_read", got.Pricing.CacheRead, "0.000000075")
	assertStringPtr(t, "cache_write", got.Pricing.CacheWrite, "0.0000003")
	assertStringPtr(t, "reasoning", got.Pricing.Reasoning, "0.0000002")
	assertStringPtr(t, "request", got.Pricing.Request, "0")
	assertStringPtr(t, "image", got.Pricing.Image, "0.001")
	assertStringPtr(t, "web_search", got.Pricing.WebSearch, "0.01")
	if len(got.RawPricing) == 0 || len(got.RawModel) == 0 {
		t.Fatal("expected raw pricing and raw model payloads")
	}
	if got.SyncedAt.IsZero() {
		t.Fatal("SyncedAt is zero")
	}
}

func TestFetchOpenRouterCatalogReturnsStatusError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       io.NopCloser(strings.NewReader(`bad gateway`)),
			Header:     make(http.Header),
		}, nil
	})}

	if _, err := FetchOpenRouterCatalog(context.Background(), client); err == nil {
		t.Fatal("expected non-2xx status error")
	}
}

func assertStringPtr(t *testing.T, name string, got *string, want string) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s = nil, want %q", name, want)
	}
	if *got != want {
		t.Fatalf("%s = %q, want %q", name, *got, want)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
