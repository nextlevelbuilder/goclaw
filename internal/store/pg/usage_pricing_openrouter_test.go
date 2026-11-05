package pg

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/usage/pricing"
)

// TestOpenRouterCatalogWithNegativePriceSurvivesStoreValidation is the
// end-to-end regression for Issue #1605: an upstream response that carries one
// negative price must still produce catalog entries the store accepts. Before
// the fix every entry was passed through with the negative decimal intact, so
// UpsertPricingCatalog skipped that model (and only that model's error was
// visible); the assertion below is what makes the skip observable without a DB.
func TestOpenRouterCatalogWithNegativePriceSurvivesStoreValidation(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		body := `{
			"data": [
				{"id": "openai/gpt-4o-mini", "pricing": {"prompt": "0.00000015", "completion": "0.0000006"}},
				{"id": "typesafe/jev-router", "pricing": {"prompt": "-0.0000001", "completion": "0.0000002"}},
				{"id": "google/gemini-2.5-flash", "pricing": {"prompt": "0.0000003", "completion": "0.0000025"}}
			]
		}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}

	entries, err := pricing.FetchOpenRouterCatalog(context.Background(), client)
	if err != nil {
		t.Fatalf("FetchOpenRouterCatalog: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(entries))
	}
	for _, e := range entries {
		if err := validateUsagePricingFields(e.Pricing); err != nil {
			t.Fatalf("entry %s rejected by store validation: %v", e.ModelID, err)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
