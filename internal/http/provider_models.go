package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ModelInfo is a normalized model entry returned by the list-models endpoint.
type ModelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// handleListProviderModels proxies to the upstream provider API to list
// available models for the given provider.
//
//	GET /v1/providers/{id}/models
func (h *ProvidersHandler) handleListProviderModels(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "provider")})
		return
	}

	p, err := h.store.GetProvider(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "provider", id.String())})
		return
	}

	// Claude CLI doesn't need an API key — return hardcoded models
	if p.ProviderType == store.ProviderClaudeCLI {
		writeJSON(w, http.StatusOK, map[string]any{"models": claudeCLIModels()})
		return
	}

	// ACP agents don't need an API key — return hardcoded models
	if p.ProviderType == store.ProviderACP {
		writeJSON(w, http.StatusOK, map[string]any{"models": acpModels()})
		return
	}

	if p.APIKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "API key")})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	var models []ModelInfo

	switch p.ProviderType {
	case "anthropic_native":
		models, err = fetchAnthropicModels(ctx, p.APIKey, h.resolveAPIBase(p))
	case "gemini_native":
		models, err = fetchGeminiModels(ctx, p.APIKey)
	case "bailian":
		models = bailianModels()
	case "dashscope":
		models = dashScopeModels()
	case "minimax_native":
		models = minimaxModels()
	case "suno":
		models = sunoModels()
	default:
		// All other types use OpenAI-compatible /models endpoint
		apiBase := strings.TrimRight(h.resolveAPIBase(p), "/")
		if apiBase == "" {
			apiBase = "https://api.openai.com/v1"
		}
		models, err = fetchOpenAIModels(ctx, apiBase, p.APIKey)
	}

	if err != nil {
		slog.Warn("providers.models", "provider", p.Name, "error", err)
		// Return empty list instead of error — provider may not support /models
		writeJSON(w, http.StatusOK, map[string]any{"models": []ModelInfo{}})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

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

// bailianModels returns a hardcoded list of models available on the
// Bailian Coding platform (coding-intl.dashscope.aliyuncs.com).
// The platform does not expose a /v1/models endpoint.
func bailianModels() []ModelInfo {
	return []ModelInfo{
		{ID: "qwen3.5-plus", Name: "Qwen 3.5 Plus"},
		{ID: "kimi-k2.5", Name: "Kimi K2.5"},
		{ID: "GLM-5", Name: "GLM-5"},
		{ID: "MiniMax-M2.5", Name: "MiniMax M2.5"},
		{ID: "qwen3-max-2026-01-23", Name: "Qwen 3 Max (2026-01-23)"},
		{ID: "qwen3-coder-next", Name: "Qwen 3 Coder Next"},
		{ID: "qwen3-coder-plus", Name: "Qwen 3 Coder Plus"},
		{ID: "glm-4.7", Name: "GLM 4.7"},
	}
}

// minimaxModels returns a hardcoded list of MiniMax models.
// MiniMax does not expose a /v1/models endpoint.
func minimaxModels() []ModelInfo {
	return []ModelInfo{
		// Chat / text
		{ID: "MiniMax-Text-01", Name: "MiniMax Text 01"},
		{ID: "MiniMax-M1", Name: "MiniMax M1"},
		{ID: "MiniMax-M2.7", Name: "MiniMax M2.7"},
		{ID: "MiniMax-M2.5", Name: "MiniMax M2.5"},
		// Image generation
		{ID: "image-01", Name: "Image 01"},
		// Video generation
		{ID: "MiniMax-Hailuo-2.3", Name: "Hailuo Video 2.3"},
		{ID: "MiniMax-Hailuo-2", Name: "Hailuo Video 2"},
		{ID: "T2V-01-Director", Name: "T2V-01 Director"},
		// Music generation
		{ID: "music-2.5+", Name: "Music 2.5+"},
		{ID: "music-2.5", Name: "Music 2.5"},
		// TTS
		{ID: "speech-02-hd", Name: "Speech 02 HD"},
		{ID: "speech-02-turbo", Name: "Speech 02 Turbo"},
	}
}

// dashScopeModels returns a hardcoded list of DashScope (Qwen) models.
// DashScope does not expose a standard /v1/models endpoint.
func dashScopeModels() []ModelInfo {
	return []ModelInfo{
		// Qwen3.5 series — Text Generation + Deep Thinking + Visual Understanding
		{ID: "qwen3.5-plus", Name: "Qwen 3.5 Plus"},
		{ID: "qwen3.5-turbo", Name: "Qwen 3.5 Turbo"},
		// Qwen3 hosted series — Text + Thinking
		{ID: "qwen3-max", Name: "Qwen 3 Max"},
		{ID: "qwen3-plus", Name: "Qwen 3 Plus"},
		{ID: "qwen3-turbo", Name: "Qwen 3 Turbo"},
		// Image generation
		{ID: "wan2.6-image", Name: "Wan 2.6 Image"},
		{ID: "wan2.1-image", Name: "Wan 2.1 Image"},
		// Video generation
		{ID: "wan2.6-video", Name: "Wan 2.6 Video"},
	}
}

// sunoModels returns a hardcoded list of Suno music generation models.
func sunoModels() []ModelInfo {
	return []ModelInfo{
		{ID: "v4.5", Name: "Suno V4.5"},
		{ID: "v4", Name: "Suno V4"},
		{ID: "v3.5", Name: "Suno V3.5"},
	}
}

// claudeCLIModels returns the model aliases accepted by the Claude CLI.
func claudeCLIModels() []ModelInfo {
	return []ModelInfo{
		{ID: "sonnet", Name: "Sonnet"},
		{ID: "opus", Name: "Opus"},
		{ID: "haiku", Name: "Haiku"},
	}
}

// acpModels returns the model aliases for ACP-compatible coding agents.
func acpModels() []ModelInfo {
	return []ModelInfo{
		{ID: "claude", Name: "Claude"},
		{ID: "codex", Name: "Codex"},
		{ID: "gemini", Name: "Gemini"},
	}
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
