package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
			Pricing:          mapOpenRouterPricing(model.Pricing),
			RawPricing:       rawPricing,
			RawModel:         raw,
			SyncedAt:         now,
		})
	}
	return entries, nil
}

func mapOpenRouterPricing(raw map[string]any) store.UsagePricingFields {
	return store.UsagePricingFields{
		Input:      decimalStringPtr(raw["prompt"]),
		Output:     decimalStringPtr(raw["completion"]),
		CacheRead:  decimalStringPtr(raw["input_cache_read"]),
		CacheWrite: decimalStringPtr(raw["input_cache_write"]),
		Reasoning:  decimalStringPtr(raw["internal_reasoning"]),
		Request:    decimalStringPtr(raw["request"]),
		Image:      decimalStringPtr(raw["image"]),
		WebSearch:  decimalStringPtr(raw["web_search"]),
	}
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
