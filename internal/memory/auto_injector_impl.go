package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// pgAutoInjector implements AutoInjector backed by EpisodicStore + FTS search.
type pgAutoInjector struct {
	episodicStore store.EpisodicStore
}

// NewAutoInjector creates an AutoInjector backed by episodic store search.
func NewAutoInjector(es store.EpisodicStore) AutoInjector {
	return &pgAutoInjector{episodicStore: es}
}

// Inject searches episodic memory for relevant L0 abstracts and formats a prompt section.
func (a *pgAutoInjector) Inject(ctx context.Context, params InjectParams) (*InjectResult, error) {
	if a.episodicStore == nil {
		return &InjectResult{}, nil
	}
	if isTrivialMessage(params.UserMessage) {
		return &InjectResult{}, nil
	}

	maxEntries := params.MaxEntries
	if maxEntries <= 0 {
		maxEntries = 5
	}
	threshold := params.Threshold
	if threshold <= 0 {
		threshold = 0.3
	}

	// Search with FTS bias (faster than vector for auto-inject)
	results, err := a.episodicStore.Search(ctx, params.UserMessage, params.AgentID, params.UserID,
		store.EpisodicSearchOptions{
			MaxResults:   maxEntries * 2, // fetch more, filter by threshold
			MinScore:     threshold,
			VectorWeight: 0.3,
			TextWeight:   0.7,
		})
	if err != nil {
		return nil, fmt.Errorf("auto-inject search: %w", err)
	}
	if len(results) == 0 {
		return &InjectResult{}, nil
	}

	// Build prompt section from L0 abstracts
	var sb strings.Builder
	sb.WriteString("## Memory Context\n\nRelevant memories from past sessions (use memory_search for details):\n")

	injected := 0
	var topScore float64
	for _, r := range results {
		if injected >= maxEntries {
			break
		}
		if r.L0Abstract == "" {
			continue
		}
		sb.WriteString("- ")
		sb.WriteString(r.L0Abstract)
		sb.WriteString("\n")
		injected++
		if r.Score > topScore {
			topScore = r.Score
		}
	}

	if injected == 0 {
		return &InjectResult{MatchCount: len(results)}, nil
	}

	return &InjectResult{
		Section:    sb.String(),
		MatchCount: len(results),
		Injected:   injected,
		TopScore:   topScore,
	}, nil
}
