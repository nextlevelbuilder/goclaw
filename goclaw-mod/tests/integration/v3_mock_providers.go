//go:build integration

package integration

import (
	"context"
	"hash/fnv"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// mockEmbedProvider returns deterministic vectors based on text content hash.
// Implements store.EmbeddingProvider for integration tests.
type mockEmbedProvider struct {
	dim int
}

func newMockEmbedProvider() *mockEmbedProvider {
	return &mockEmbedProvider{dim: 1536}
}

func (m *mockEmbedProvider) Name() string  { return "mock" }
func (m *mockEmbedProvider) Model() string { return "mock-embed-v1" }

func (m *mockEmbedProvider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i, text := range texts {
		vec := make([]float32, m.dim)
		h := fnv.New32a()
		h.Write([]byte(text))
		seed := h.Sum32()
		for j := range vec {
			seed = seed*1103515245 + 12345
			vec[j] = float32(seed%1000) / 1000.0
		}
		result[i] = vec
	}
	return result, nil
}

// Compile-time interface check.
var _ store.EmbeddingProvider = (*mockEmbedProvider)(nil)
