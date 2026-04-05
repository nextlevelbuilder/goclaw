package http

import (
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Provider-level embedding settings are used by the memory system, whose
// PostgreSQL schema currently stores fixed vector(1536) embeddings.
func validateProviderEmbeddingSettings(p *store.LLMProviderData) error {
	es := store.ParseEmbeddingSettings(p.Settings)
	if es == nil || !es.Enabled {
		return nil
	}
	if es.Dimensions < 0 {
		return fmt.Errorf("embedding.dimensions must be a positive integer or omitted")
	}
	if es.Dimensions > 0 && es.Dimensions != store.RequiredMemoryEmbeddingDimensions {
		return fmt.Errorf(
			"embedding.dimensions must be %d or omitted because GoClaw memory stores vector(%d)",
			store.RequiredMemoryEmbeddingDimensions,
			store.RequiredMemoryEmbeddingDimensions,
		)
	}
	return nil
}
