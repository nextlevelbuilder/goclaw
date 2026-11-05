package pricing

import (
	"context"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"testing"
)

// TestFetchOpenRouterCatalogIgnoresNegativePriceFields covers the OpenRouter
// catalog sync failure from issue #1605: the API occasionally returns a
// negative price (e.g. typesafe/jev-router reported a negative prompt price).
// Such a value must be treated as unavailable instead of being mapped into the
// normalized pricing, otherwise the catalog store rejects the whole entry and
// the model never reaches the catalog.
//
// The raw payload must keep the negative value so operators can still inspect
// what the upstream API returned.
func TestFetchOpenRouterCatalogIgnoresNegativePriceFields(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		body := `{
			"data": [
				{
					"id": "openai/gpt-4o-mini",
					"pricing": {"prompt": "0.00000015", "completion": "0.0000006"}
				},
				{
					"id": "typesafe/jev-router",
					"pricing": {"prompt": "-0.0000001", "completion": "0.0000002"}
				},
				{
					"id": "anthropic/claude-sonnet-4.5",
					"pricing": {"prompt": -0.0000003, "completion": 0.000003, "request": -1}
				},
				{
					"id": "google/gemini-2.5-flash",
					"pricing": {"prompt": "0.0000003", "completion": "0.0000025"}
				}
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
	if len(entries) != 4 {
		t.Fatalf("entries = %d, want 4 (a bad price must not drop the model)", len(entries))
	}

	byID := map[string]int{}
	for i, e := range entries {
		byID[e.ModelID] = i
	}

	// Negative string price: dropped, sibling fields preserved.
	jev, ok := byID["typesafe/jev-router"]
	if !ok {
		t.Fatal("typesafe/jev-router missing from catalog entries")
	}
	if got := entries[jev].Pricing.Input; got != nil {
		t.Fatalf("negative prompt price mapped as %q, want nil", *got)
	}
	assertStringPtr(t, "jev completion", entries[jev].Pricing.Output, "0.0000002")
	if raw := string(entries[jev].RawPricing); !strings.Contains(raw, "-0.0000001") {
		t.Fatalf("raw pricing lost the upstream value: %s", raw)
	}

	// Negative JSON number price: dropped for both float fields.
	claude, ok := byID["anthropic/claude-sonnet-4.5"]
	if !ok {
		t.Fatal("anthropic/claude-sonnet-4.5 missing from catalog entries")
	}
	if got := entries[claude].Pricing.Input; got != nil {
		t.Fatalf("negative numeric prompt price mapped as %q, want nil", *got)
	}
	if got := entries[claude].Pricing.Request; got != nil {
		t.Fatalf("negative numeric request price mapped as %q, want nil", *got)
	}
	assertStringPtr(t, "claude completion", entries[claude].Pricing.Output, decimalValue(t, "0.000003"))

	// Healthy models are untouched, including the one after the bad entry.
	assertStringPtr(t, "first input", entries[byID["openai/gpt-4o-mini"]].Pricing.Input, "0.00000015")
	assertStringPtr(t, "last input", entries[byID["google/gemini-2.5-flash"]].Pricing.Input, "0.0000003")
}

// decimalValue renders want exactly like the float64 normalization path does, so
// assertions stay about presence/absence rather than decimal formatting.
func decimalValue(t *testing.T, want string) string {
	t.Helper()
	r, ok := new(big.Rat).SetString(want)
	if !ok {
		t.Fatalf("bad test decimal %q", want)
	}
	f, _ := r.Float64()
	return fmt.Sprintf("%.18g", f)
}

// TestMapOpenRouterPricingKeepsZeroPrices pins the boundary: a zero price is a
// real (free) price, not an unavailable one, so it must survive normalization.
func TestMapOpenRouterPricingKeepsZeroPrices(t *testing.T) {
	got := mapOpenRouterPricing("free/model", map[string]any{
		"prompt": "0",
		"image":  float64(0),
	})
	assertStringPtr(t, "zero string input", got.Input, "0")
	assertStringPtr(t, "zero numeric image", got.Image, "0")
}
