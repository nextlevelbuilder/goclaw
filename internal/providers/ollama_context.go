package providers

import (
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		slog.Warn("ollama.context: build request failed", "model", model, "error", err, "fallback", OllamaDefaultNumCtx)
		return OllamaDefaultNumCtx
	}
	q := req.URL.Query()
	q.Set("model", model)
	req.URL.RawQuery = q.Encode()
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
		ModelInfo struct {
			ContextLength int `json:"context_length"`
		} `json:"model_info"`
	}
	if err := json.Unmarshal(rawBody, &result); err != nil {
		slog.Warn("ollama.context: decode failed", "model", model, "error", fmt.Sprintf("%v", err), "fallback", OllamaDefaultNumCtx)
		return OllamaDefaultNumCtx
	}

	slog.Debug("ollama.context: extracted context_length", "model", model, "context_length", result.ModelInfo.ContextLength)

	if result.ModelInfo.ContextLength <= 0 {
		slog.Debug("ollama.context: context_length not positive, using default", "model", model, "fallback", OllamaDefaultNumCtx)
		return OllamaDefaultNumCtx
	}
	slog.Info("ollama.context: resolved context window", "model", model, "num_ctx", result.ModelInfo.ContextLength)
	return result.ModelInfo.ContextLength
}
