package http

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ModelInfo is a normalized model entry returned by the list-models endpoint.
type ModelInfo struct {
	ID        string                         `json:"id"`
	Name      string                         `json:"name,omitempty"`
	Reasoning *providers.ReasoningCapability `json:"reasoning,omitempty"`
}

type ProviderModelsResponse struct {
	Models            []ModelInfo                    `json:"models"`
	ReasoningDefaults *store.ProviderReasoningConfig `json:"reasoning_defaults,omitempty"`
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

	respond := func(models []ModelInfo) {
		writeJSON(w, http.StatusOK, ProviderModelsResponse{
			Models:            models,
			ReasoningDefaults: reasoningDefaultsForModels(p.Settings, models),
		})
	}

	// Claude CLI doesn't need an API key — return hardcoded models
	if p.ProviderType == store.ProviderClaudeCLI {
		respond(claudeCLIModels())
		return
	}

	if p.ProviderType == store.ProviderChatGPTOAuth {
		respond(chatGPTOAuthModels())
		return
	}

	// ACP agents don't need an API key — return hardcoded models
	if p.ProviderType == store.ProviderACP {
		respond(acpModels())
		return
	}

	// Ollama: use native /api/tags for richer metadata (parameter size, quantization, family).
	// ProviderOllama has no API key; ProviderOllamaCloud requires one but both use the same endpoint.
	if p.ProviderType == store.ProviderOllama || p.ProviderType == store.ProviderOllamaCloud {
		ctx, cancel := context.WithTimeout(r.Context(), time.Duration(loadProviderRequestTimeoutSec(r.Context(), h.sysConfigStore))*time.Second)
		defer cancel()
		apiBase := h.resolveAPIBase(p)
		if apiBase == "" {
			apiBase = "http://localhost:11434"
		}
		models, err := h.fetchOllamaModels(ctx, apiBase, p.APIKey)
		if err != nil {
			slog.Warn("providers.models.ollama", "provider", p.Name, "error", err)
			respond([]ModelInfo{})
			return
		}
		respond(models)
		return
	}

	if p.APIKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "API key")})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(loadProviderRequestTimeoutSec(r.Context(), h.sysConfigStore))*time.Second)
	defer cancel()

	var models []ModelInfo

	switch p.ProviderType {
	case store.ProviderAnthropicNative:
		models, err = fetchAnthropicModels(ctx, p.APIKey, h.resolveAPIBase(p))
	case store.ProviderGeminiNative:
		models, err = fetchGeminiModels(ctx, p.APIKey)
	case store.ProviderBailian:
		models = bailianModels()
	case store.ProviderDashScope:
		models = dashScopeModels()
	case store.ProviderMiniMax:
		models = minimaxModels()
	case store.ProviderZai, store.ProviderZaiCoding:
		models = zaiModels()
	case store.ProviderAIMLAPI:
		models = aimlapiModels()
	default:
		// All other types use OpenAI-compatible /models endpoint
		apiBase := openAIModelsAPIBase(p.ProviderType, h.resolveAPIBase(p))
		models, err = fetchOpenAIModels(ctx, apiBase, p.APIKey, openAIModelsExtraHeaders(p.ProviderType))
	}

	if err != nil {
		slog.Warn("providers.models", "provider", p.Name, "error", err)
		// Return empty list instead of error — provider may not support /models
		respond([]ModelInfo{})
		return
	}

	respond(withReasoningCapabilities(models))
}

func aimlapiModels() []ModelInfo {
	models := providers.AIMLAPIChatModels()
	result := make([]ModelInfo, 0, len(models))
	for _, model := range models {
		result = append(result, ModelInfo{ID: model, Name: model})
	}
	return result
}

func openAIModelsAPIBase(providerType, apiBase string) string {
	base := strings.TrimRight(apiBase, "/")
	if base != "" {
		return base
	}
	switch providerType {
	case store.ProviderKimiCoding:
		return store.KimiCodingDefaultAPIBase
	default:
		return "https://api.openai.com/v1"
	}
}

func openAIModelsExtraHeaders(providerType string) map[string]string {
	if providerType != store.ProviderKimiCoding {
		return nil
	}
	return map[string]string{
		"User-Agent": store.KimiCodingRequiredUserAgent,
	}
}

func reasoningDefaultsForModels(
	settings []byte,
	models []ModelInfo,
) *store.ProviderReasoningConfig {
	if len(models) == 0 {
		return nil
	}
	for _, model := range models {
		if model.Reasoning != nil {
			return store.ParseProviderReasoningConfig(settings)
		}
	}
	return nil
}

func withReasoningCapabilities(models []ModelInfo) []ModelInfo {
	result := make([]ModelInfo, 0, len(models))
	for _, model := range models {
		next := model
		next.Reasoning = providers.LookupReasoningCapability(model.ID)
		result = append(result, next)
	}
	return result
}
