package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const OpenRouterModelsURL = "https://openrouter.ai/api/v1/models"

type openRouterModelsResponse struct {
	Data []json.RawMessage `json:"data"`
}

type openRouterModel struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Pricing map[string]any `json:"pricing"`
}

func FetchOpenRouterCatalog(ctx context.Context, client *http.Client) ([]store.UsagePricingCatalogEntry, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, OpenRouterModelsURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openrouter models status %d", resp.StatusCode)
	}
	var payload openRouterModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	entries := make([]store.UsagePricingCatalogEntry, 0, len(payload.Data))
	for _, raw := range payload.Data {
		var model openRouterModel
		if err := json.Unmarshal(raw, &model); err != nil || model.ID == "" {
			continue
		}
		rawPricing, _ := json.Marshal(model.Pricing)
		entries = append(entries, store.UsagePricingCatalogEntry{
			ModelID:          model.ID,
			CanonicalModelID: model.ID,
			Pricing:          mapOpenRouterPricing(model.ID, model.Pricing),
			RawPricing:       rawPricing,
			RawModel:         raw,
			SyncedAt:         now,
		})
	}
	return entries, nil
}

// mapOpenRouterPricing normalizes the upstream pricing map. Negative values are
// treated as unavailable (Issue #1605): OpenRouter occasionally publishes them
// (e.g. a negative prompt price), and the catalog store rejects any entry with a
// negative price, which would drop the model instead of importing it. The raw
// payload keeps the original value for troubleshooting.
func mapOpenRouterPricing(modelID string, raw map[string]any) store.UsagePricingFields {
	return store.UsagePricingFields{
		Input:      nonNegativeDecimalStringPtr(modelID, "prompt", raw["prompt"]),
		Output:     nonNegativeDecimalStringPtr(modelID, "completion", raw["completion"]),
		CacheRead:  nonNegativeDecimalStringPtr(modelID, "input_cache_read", raw["input_cache_read"]),
		CacheWrite: nonNegativeDecimalStringPtr(modelID, "input_cache_write", raw["input_cache_write"]),
		Reasoning:  nonNegativeDecimalStringPtr(modelID, "internal_reasoning", raw["internal_reasoning"]),
		Request:    nonNegativeDecimalStringPtr(modelID, "request", raw["request"]),
		Image:      nonNegativeDecimalStringPtr(modelID, "image", raw["image"]),
		WebSearch:  nonNegativeDecimalStringPtr(modelID, "web_search", raw["web_search"]),
	}
}

// nonNegativeDecimalStringPtr is decimalStringPtr with the additional rule that
// a negative price yields nil (unavailable) instead of an invalid decimal, so a
// single bad field cannot sink the whole catalog entry. Zero stays a real price.
func nonNegativeDecimalStringPtr(modelID, field string, v any) *string {
	s := decimalStringPtr(v)
	if s == nil {
		return nil
	}
	r, ok := new(big.Rat).SetString(strings.TrimSpace(*s))
	if !ok {
		return s // let the store's validation report the malformed decimal
	}
	if r.Sign() < 0 {
		slog.Warn("usage_pricing.openrouter_negative_price_ignored",
			"model", modelID, "field", field, "value", *s)
		return nil
	}
	return s
}

func decimalStringPtr(v any) *string {
	switch x := v.(type) {
	case string:
		if x == "" {
			return nil
		}
		return &x
	case float64:
		s := fmt.Sprintf("%.18g", x)
		return &s
	default:
		return nil
	}
}
