package providers

// VoyageEmbeddingProvider wraps OpenAIEmbeddingProvider with Voyage AI base URL.
// Voyage is Anthropic's embedding partner and uses the same wire format as OpenAI.
type VoyageEmbeddingProvider struct {
	*OpenAIEmbeddingProvider
}

// NewVoyageEmbeddingProvider creates an embedding provider for Voyage AI.
// Default model: voyage-3 (1024 dim) — NOTE: must use voyage-3-large (1536 dim)
// or text-embedding-3-small via OpenAI to match the system's pgvector(1536) column.
func NewVoyageEmbeddingProvider(apiKey, model string) *VoyageEmbeddingProvider {
	if model == "" {
		model = "voyage-3-large" // 1536 dimensions, matches pgvector column
	}
	p := NewOpenAIEmbeddingProvider(apiKey, "https://api.voyageai.com/v1", model)
	p.providerName = "voyage"
	return &VoyageEmbeddingProvider{OpenAIEmbeddingProvider: p}
}
