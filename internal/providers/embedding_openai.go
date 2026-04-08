package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const embeddingBatchSize = 2048

// ExpectedEmbeddingDim is the pgvector column dimension used across the system.
// All embedding providers must return vectors of this dimension.
const ExpectedEmbeddingDim = 1536

// OpenAIEmbeddingProvider implements store.EmbeddingProvider for OpenAI-compatible APIs.
type OpenAIEmbeddingProvider struct {
	providerName string
	apiKey       string
	apiBase      string
	model        string
	client       *http.Client
	retry        RetryConfig
}

// NewOpenAIEmbeddingProvider creates an embedding provider for OpenAI-compatible APIs.
func NewOpenAIEmbeddingProvider(apiKey, apiBase, model string) *OpenAIEmbeddingProvider {
	if apiBase == "" {
		apiBase = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "text-embedding-3-small" // 1536 dimensions, matches pgvector column
	}
	return &OpenAIEmbeddingProvider{
		providerName: "openai",
		apiKey:       apiKey,
		apiBase:      strings.TrimRight(apiBase, "/"),
		model:        model,
		client:       &http.Client{Timeout: 60 * time.Second},
		retry:        DefaultRetryConfig(),
	}
}

func (p *OpenAIEmbeddingProvider) Name() string  { return p.providerName }
func (p *OpenAIEmbeddingProvider) Model() string { return p.model }

// Embed generates vector embeddings for the given texts.
// Returns [][]float32 where each inner slice has ExpectedEmbeddingDim elements.
func (p *OpenAIEmbeddingProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	results := make([][]float32, len(texts))

	// Process in batches of embeddingBatchSize
	for start := 0; start < len(texts); start += embeddingBatchSize {
		end := min(start+embeddingBatchSize, len(texts))

		embeddings, err := p.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, fmt.Errorf("embedding batch [%d:%d]: %w", start, end, err)
		}

		for i, emb := range embeddings {
			results[start+i] = emb
		}
	}

	return results, nil
}

func (p *OpenAIEmbeddingProvider) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	reqBody := map[string]any{
		"model": p.model,
		"input": texts,
	}

	return RetryDo(ctx, p.retry, func() ([][]float32, error) {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", p.apiBase+"/embeddings", bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+p.apiKey)

		resp, err := p.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("embedding request: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, &HTTPError{
				Status:     resp.StatusCode,
				Body:       string(body),
				RetryAfter: ParseRetryAfter(resp.Header.Get("Retry-After")),
			}
		}

		var result embeddingResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decode embedding response: %w", err)
		}

		// Convert float64 → float32, validate dimension, sort by index
		embeddings := make([][]float32, len(texts))
		for _, d := range result.Data {
			if d.Index >= len(embeddings) {
				continue
			}
			if len(d.Embedding) != ExpectedEmbeddingDim {
				return nil, fmt.Errorf(
					"embedding dimension mismatch: model %s returned %d, expected %d (pgvector column)",
					p.model, len(d.Embedding), ExpectedEmbeddingDim,
				)
			}
			vec := make([]float32, len(d.Embedding))
			for j, v := range d.Embedding {
				vec[j] = float32(v)
			}
			embeddings[d.Index] = vec
		}

		return embeddings, nil
	})
}

// --- Embedding API response types ---

type embeddingResponse struct {
	Data  []embeddingData `json:"data"`
	Usage *embeddingUsage `json:"usage,omitempty"`
}

type embeddingData struct {
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

type embeddingUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}
