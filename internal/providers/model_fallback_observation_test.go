package providers

import (
	"encoding/json"
	"testing"
)

func TestMergeModelFallbackMetadataPreservesExistingSections(t *testing.T) {
	existing := json.RawMessage(`{"reasoning":{"effort":"high"}}`)
	merged := MergeModelFallbackMetadata(existing, []ModelFallbackAttemptMetadata{
		{ProviderName: "primary", Model: "primary-model", Status: "error", Reason: string(FailoverContentPolicy), Error: "refused"},
		{ProviderName: "backup", Model: "backup-model", Status: "success"},
	})

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(merged, &payload); err != nil {
		t.Fatalf("metadata JSON invalid: %v", err)
	}
	if len(payload["reasoning"]) == 0 {
		t.Fatal("existing reasoning section missing")
	}
	var fallback struct {
		Attempts             []ModelFallbackAttemptMetadata `json:"attempts"`
		SelectedProviderName string                         `json:"selected_provider_name"`
		SelectedModel        string                         `json:"selected_model"`
	}
	if err := json.Unmarshal(payload[ModelFallbackMetadataKey], &fallback); err != nil {
		t.Fatalf("model fallback section invalid: %v", err)
	}
	if len(fallback.Attempts) != 2 {
		t.Fatalf("attempts = %d, want 2", len(fallback.Attempts))
	}
	if fallback.SelectedProviderName != "backup" || fallback.SelectedModel != "backup-model" {
		t.Fatalf("selected = %s/%s, want backup/backup-model", fallback.SelectedProviderName, fallback.SelectedModel)
	}
}
