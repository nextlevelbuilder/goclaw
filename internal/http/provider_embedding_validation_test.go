package http

import (
	"encoding/json"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func embeddingProvider(dims int) *store.LLMProviderData {
	settings := map[string]any{
		"embedding": map[string]any{
			"enabled":    true,
			"model":      "test-model",
			"dimensions": dims,
		},
	}
	raw, _ := json.Marshal(settings)
	return &store.LLMProviderData{Settings: raw}
}

func TestValidateProviderEmbeddingSettings(t *testing.T) {
	tests := []struct {
		name    string
		p       *store.LLMProviderData
		wantErr bool
	}{
		{"nil settings", &store.LLMProviderData{}, false},
		{"embedding disabled", &store.LLMProviderData{
			Settings: json.RawMessage(`{"embedding":{"enabled":false}}`),
		}, false},
		{"dimensions omitted (0)", &store.LLMProviderData{
			Settings: json.RawMessage(`{"embedding":{"enabled":true,"model":"m"}}`),
		}, false},
		{"dimensions 1536", embeddingProvider(1536), false},
		{"dimensions 2048", embeddingProvider(2048), true},
		{"dimensions 1024", embeddingProvider(1024), true},
		{"dimensions negative", embeddingProvider(-1), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateProviderEmbeddingSettings(tt.p)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateProviderEmbeddingSettings() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
