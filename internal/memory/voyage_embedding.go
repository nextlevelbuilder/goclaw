package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	voyageAPIBase    = "https://api.voyageai.com/v1"
	voyageDefaultModel = "voyage-4"
)

// VoyageEmbeddingProvider uses the Voyage AI embedding API.
// Voyage AI supports an optional input_type field ("query" or "document")
// which can improve retrieval quality for asymmetric use cases.
type VoyageEmbeddingProvider struct {
	name      string
	model     string
	apiKey    string
	apiBase   string
	inputType string // "query", "document", or "" (omit from request)
}

// NewVoyageEmbeddingProvider creates a Voyage AI embedding provider.
// apiBase defaults to https://api.voyageai.com/v1; model defaults to voyage-3.
func NewVoyageEmbeddingProvider(name, apiKey, apiBase, model string) *VoyageEmbeddingProvider {
	if apiBase == "" {
		apiBase = voyageAPIBase
	}
	if model == "" {
		model = voyageDefaultModel
	}
	return &VoyageEmbeddingProvider{
		name:    name,
		model:   model,
		apiKey:  apiKey,
		apiBase: apiBase,
	}
}

// WithInputType sets the input_type parameter sent to the Voyage API.
// Use "document" when indexing stored texts, "query" when embedding a search query.
// Leave empty (default) to omit the field and let the API use its default.
func (p *VoyageEmbeddingProvider) WithInputType(t string) *VoyageEmbeddingProvider {
	p.inputType = t
	return p
}

func (p *VoyageEmbeddingProvider) Name() string  { return p.name }
func (p *VoyageEmbeddingProvider) Model() string { return p.model }

func (p *VoyageEmbeddingProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := map[string]any{
		"input": texts,
		"model": p.model,
	}
	if p.inputType != "" {
		reqBody["input_type"] = p.inputType
	}

	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.apiBase+"/embeddings", bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("voyage embedding API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	embeddings := make([][]float32, len(result.Data))
	for i, d := range result.Data {
		embeddings[i] = d.Embedding
	}

	return embeddings, nil
}
