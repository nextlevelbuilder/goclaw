package memory

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

const (
	geminiEmbeddingBatchSize = 100 // batchEmbedContents API limit
	geminiEmbeddingAPIBase   = "https://generativelanguage.googleapis.com/v1beta"
	// GeminiDefaultEmbeddingModel is the default model for Gemini embeddings.
	// text-embedding-004/005 cap at 768 dims; gemini-embedding-2 supports higher dims (incl. 1536).
	GeminiDefaultEmbeddingModel = "gemini-embedding-2"
)

// GeminiEmbeddingProvider implements EmbeddingProvider using the Google Generative Language API.
type GeminiEmbeddingProvider struct {
	name   string
	model  string
	apiKey string
	apiBase string
	dims   int
	client *http.Client
}

// NewGeminiEmbeddingProvider creates an embedding provider backed by the Gemini API.
// apiBase may be empty (defaults to generativelanguage.googleapis.com).
func NewGeminiEmbeddingProvider(name, apiKey, apiBase, model string) *GeminiEmbeddingProvider {
	if apiBase == "" {
		apiBase = geminiEmbeddingAPIBase
	}
	if model == "" {
		model = GeminiDefaultEmbeddingModel
	}
	return &GeminiEmbeddingProvider{
		name:    name,
		model:   model,
		apiKey:  apiKey,
		apiBase: strings.TrimRight(apiBase, "/"),
		dims:    0,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// WithDimensions sets the outputDimensionality sent to the API.
// Must match the pgvector column size (RequiredMemoryEmbeddingDimensions = 1536).
func (p *GeminiEmbeddingProvider) WithDimensions(d int) *GeminiEmbeddingProvider {
	p.dims = d
	return p
}

func (p *GeminiEmbeddingProvider) Name() string  { return p.name }
func (p *GeminiEmbeddingProvider) Model() string { return p.model }

func (p *GeminiEmbeddingProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	results := make([][]float32, len(texts))
	for start := 0; start < len(texts); start += geminiEmbeddingBatchSize {
		end := min(start+geminiEmbeddingBatchSize, len(texts))
		batch, err := p.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, fmt.Errorf("gemini embedding batch [%d:%d]: %w", start, end, err)
		}
		for i, emb := range batch {
			results[start+i] = emb
		}
	}
	return results, nil
}

// modelName returns the fully-qualified model name required by the Gemini API.
// e.g. "gemini-embedding-exp-03-07" → "models/gemini-embedding-exp-03-07"
func (p *GeminiEmbeddingProvider) modelName() string {
	if strings.HasPrefix(p.model, "models/") {
		return p.model
	}
	return "models/" + p.model
}

func (p *GeminiEmbeddingProvider) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	type contentPart struct {
		Text string `json:"text"`
	}
	type content struct {
		Parts []contentPart `json:"parts"`
	}
	type embedRequest struct {
		Model               string  `json:"model"`
		Content             content `json:"content"`
		OutputDimensionality *int   `json:"outputDimensionality,omitempty"`
	}
	type batchRequest struct {
		Requests []embedRequest `json:"requests"`
	}

	reqs := make([]embedRequest, len(texts))
	for i, t := range texts {
		r := embedRequest{
			Model:   p.modelName(),
			Content: content{Parts: []contentPart{{Text: t}}},
		}
		if p.dims > 0 {
			d := p.dims
			r.OutputDimensionality = &d
		}
		reqs[i] = r
	}

	body, err := json.Marshal(batchRequest{Requests: reqs})
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	endpoint := fmt.Sprintf("%s/models/%s:batchEmbedContents", p.apiBase, p.model)
	// Use the unqualified model name in the URL path.
	if strings.HasPrefix(p.model, "models/") {
		endpoint = fmt.Sprintf("%s/%s:batchEmbedContents", p.apiBase, p.model)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini embedding API %d: %s", resp.StatusCode, string(b))
	}

	var result struct {
		Embeddings []struct {
			Values []float32 `json:"values"`
		} `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if len(result.Embeddings) != len(texts) {
		return nil, fmt.Errorf("gemini embedding count mismatch: got %d, want %d", len(result.Embeddings), len(texts))
	}

	out := make([][]float32, len(texts))
	for i, e := range result.Embeddings {
		out[i] = e.Values
	}
	return out, nil
}
