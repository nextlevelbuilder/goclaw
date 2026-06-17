package providers

import (
	"encoding/json"
)

const ModelFallbackMetadataKey = "model_fallback"

type ModelFallbackAttemptMetadata struct {
	ProviderName string `json:"provider_name,omitempty"`
	Model        string `json:"model,omitempty"`
	Status       string `json:"status,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Error        string `json:"error,omitempty"`
	Streamed     bool   `json:"streamed,omitempty"`
}

func (m ModelFallbackAttemptMetadata) Empty() bool {
	return m.ProviderName == "" && m.Model == "" && m.Status == "" &&
		m.Reason == "" && m.Error == "" && !m.Streamed
}

func MergeModelFallbackMetadata(existing json.RawMessage, entries []ModelFallbackAttemptMetadata) json.RawMessage {
	clean := make([]ModelFallbackAttemptMetadata, 0, len(entries))
	selectedProvider := ""
	selectedModel := ""
	for _, entry := range entries {
		if entry.Empty() {
			continue
		}
		clean = append(clean, entry)
		if entry.Status == "success" {
			selectedProvider = entry.ProviderName
			selectedModel = entry.Model
		}
	}
	if len(clean) == 0 {
		return existing
	}
	payload := map[string]any{}
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &payload)
	}
	section := map[string]any{"attempts": clean}
	if selectedProvider != "" {
		section["selected_provider_name"] = selectedProvider
	}
	if selectedModel != "" {
		section["selected_model"] = selectedModel
	}
	payload[ModelFallbackMetadataKey] = section
	data, err := json.Marshal(payload)
	if err != nil {
		return existing
	}
	return json.RawMessage(data)
}
