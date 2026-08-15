package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

const (
	// OllamaDefaultNumCtx is the fallback context window size used when neither a
	// user-configured value nor a successful /api/show query is available.
	OllamaDefaultNumCtx = 131072
)

// FetchOllamaModelContext queries the Ollama /api/show endpoint for a model's
// native context length. The apiBase may include a /v1 suffix — it is stripped
// before building the URL since /api/show lives at the root, not under /v1.
//
// Returns OllamaDefaultNumCtx on any error so callers never need to handle
// the error path; the slog warning is emitted here for observability.
func FetchOllamaModelContext(ctx context.Context, apiBase, model, apiKey string) int {
	base := strings.TrimRight(strings.TrimSuffix(strings.TrimRight(apiBase, "/"), "/v1"), "/")
	url := base + "/api/show"

	slog.Debug("ollama.context: querying /api/show", "api_base", apiBase, "resolved_base", base, "model", model)

	// /api/show is POST-only: a GET is answered with "405 method not allowed",
	// which silently degraded every lookup to the fallback.
	payload, err := json.Marshal(map[string]string{"model": model})
	if err != nil {
		slog.Warn("ollama.context: encode request failed", "model", model, "error", err, "fallback", OllamaDefaultNumCtx)
		return OllamaDefaultNumCtx
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		slog.Warn("ollama.context: build request failed", "model", model, "error", err, "fallback", OllamaDefaultNumCtx)
		return OllamaDefaultNumCtx
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("ollama.context: request failed", "model", model, "error", err, "fallback", OllamaDefaultNumCtx)
		return OllamaDefaultNumCtx
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		slog.Warn("ollama.context: non-200 response", "model", model, "status", resp.StatusCode, "body", string(body), "fallback", OllamaDefaultNumCtx)
		return OllamaDefaultNumCtx
	}

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Warn("ollama.context: read body failed", "model", model, "error", err, "fallback", OllamaDefaultNumCtx)
		return OllamaDefaultNumCtx
	}
	slog.Debug("ollama.context: /api/show raw response", "model", model, "response", string(rawBody))

	var result struct {
		ModelInfo map[string]json.RawMessage `json:"model_info"`
	}
	if err := json.Unmarshal(rawBody, &result); err != nil {
		slog.Warn("ollama.context: decode failed", "model", model, "error", fmt.Sprintf("%v", err), "fallback", OllamaDefaultNumCtx)
		return OllamaDefaultNumCtx
	}

	contextLength := extractContextLength(result.ModelInfo)
	slog.Debug("ollama.context: extracted context_length", "model", model, "context_length", contextLength)

	if contextLength <= 0 {
		slog.Debug("ollama.context: context_length not positive, using default", "model", model, "fallback", OllamaDefaultNumCtx)
		return OllamaDefaultNumCtx
	}
	slog.Info("ollama.context: resolved context window", "model", model, "num_ctx", contextLength)
	return contextLength
}

// extractContextLength pulls the context window out of an /api/show model_info map.
// Ollama namespaces the key by model architecture ("gemma4.context_length",
// "qwen35.context_length", "llama.context_length"), so a fixed "context_length"
// lookup never matches a real server response; the bare key is still accepted
// because it is what hand-written fixtures and older stubs return.
func extractContextLength(modelInfo map[string]json.RawMessage) int {
	for key, raw := range modelInfo {
		if key != "context_length" && !strings.HasSuffix(key, ".context_length") {
			continue
		}
		var length int
		if err := json.Unmarshal(raw, &length); err != nil {
			continue
		}
		if length > 0 {
			return length
		}
	}
	return 0
}
