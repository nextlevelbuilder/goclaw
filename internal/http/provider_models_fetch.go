package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// fetchAnthropicModels calls the Anthropic models API.
func fetchAnthropicModels(ctx context.Context, apiKey, apiBase string) ([]ModelInfo, error) {
	base := strings.TrimRight(apiBase, "/")
	if base == "" {
		base = "https://api.anthropic.com/v1"
	}
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("anthropic API returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode anthropic response: %w", err)
	}

	models := make([]ModelInfo, 0, len(result.Data))
	for _, m := range result.Data {
		models = append(models, ModelInfo{ID: m.ID, Name: m.DisplayName})
	}
	return models, nil
}

// fetchGeminiModels calls the Google Gemini models API.
// Gemini uses a different format: GET /v1beta/models?key=API_KEY
func fetchGeminiModels(ctx context.Context, apiKey string) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://generativelanguage.googleapis.com/v1beta/models?key="+apiKey, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("gemini API returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Models []struct {
			Name        string `json:"name"`        // e.g. "models/gemini-2.0-flash"
			DisplayName string `json:"displayName"` // e.g. "Gemini 2.0 Flash"
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode gemini response: %w", err)
	}

	models := make([]ModelInfo, 0, len(result.Models))
	for _, m := range result.Models {
		// Strip "models/" prefix to get the usable model ID
		id := strings.TrimPrefix(m.Name, "models/")
		models = append(models, ModelInfo{ID: id, Name: m.DisplayName})
	}
	return models, nil
}

// fetchOpenAIModels calls an OpenAI-compatible /models endpoint.
func fetchOpenAIModels(ctx context.Context, apiBase, apiKey string) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", apiBase+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("provider API returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode provider response: %w", err)
	}

	models := make([]ModelInfo, 0, len(result.Data))
	for _, m := range result.Data {
		models = append(models, ModelInfo{ID: m.ID, Name: m.ID})
	}
	return models, nil
}

// fetchOllamaModels calls Ollama's native /api/tags endpoint to get model metadata
// including parameter size, quantization level, and model family.
// The api_base may include a /v1 suffix (from issue #654 normalization) — strip it
// before appending /api/tags since /api/tags lives at the root, not under /v1.
func (h *ProvidersHandler) fetchOllamaModels(ctx context.Context, apiBase, apiKey string) ([]ModelInfo, error) {
	// Strip /v1 suffix if present, then trim trailing slash.
	base := strings.TrimRight(strings.TrimSuffix(strings.TrimRight(apiBase, "/"), "/v1"), "/")
	url := config.DockerLocalhost(base + "/api/tags")

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("ollama /api/tags returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Models []struct {
			Name    string `json:"name"`
			Details struct {
				Family            string `json:"family"`
				ParameterSize     string `json:"parameter_size"`
				QuantizationLevel string `json:"quantization_level"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode ollama response: %w", err)
	}

	models := make([]ModelInfo, 0, len(result.Models))
	for _, m := range result.Models {
		// Build a human-readable display name: "family paramSize quantLevel" e.g. "gemma4 8.0B Q4_K_M"
		var parts []string
		if m.Details.Family != "" {
			parts = append(parts, m.Details.Family)
		}
		if m.Details.ParameterSize != "" {
			parts = append(parts, m.Details.ParameterSize)
		}
		if m.Details.QuantizationLevel != "" {
			parts = append(parts, m.Details.QuantizationLevel)
		}
		name := m.Name
		if len(parts) > 0 {
			name = strings.Join(parts, " ")
		}
		models = append(models, ModelInfo{ID: m.Name, Name: name})
	}
	return models, nil
}
