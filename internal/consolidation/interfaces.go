package consolidation

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/knowledgegraph"
)

// EntityExtractor extracts knowledge graph entities from text.
// Defined at the consumer to allow test mocks without depending on concrete Extractor.
type EntityExtractor interface {
	Extract(ctx context.Context, text string) (*knowledgegraph.ExtractionResult, error)
}
