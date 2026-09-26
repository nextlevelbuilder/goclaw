package memory

import (
	"context"
	"fmt"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tokencount"
)

type budgetTestEpisodicStore struct {
	store.EpisodicStore
	results []store.EpisodicSearchResult
}

func (s *budgetTestEpisodicStore) Search(
	context.Context,
	string,
	string,
	string,
	store.EpisodicSearchOptions,
) ([]store.EpisodicSearchResult, error) {
	return s.results, nil
}

func TestAutoInjectorRespectsMaxTokens(t *testing.T) {
	results := make([]store.EpisodicSearchResult, 12)
	for i := range results {
		results[i] = store.EpisodicSearchResult{
			L0Abstract: fmt.Sprintf("memory %02d: %s", i, "important project context repeated for token budget coverage"),
			Score:      0.9,
		}
	}

	tests := []struct {
		name      string
		maxTokens int
		wantLimit int
	}{
		{name: "explicit budget", maxTokens: 100, wantLimit: 100},
		{name: "default budget", maxTokens: 0, wantLimit: 200},
	}

	counter := tokencount.NewFallbackCounter()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			injector := NewAutoInjector(&budgetTestEpisodicStore{results: results}, nil)
			got, err := injector.Inject(context.Background(), InjectParams{
				AgentID:     "agent-id",
				UserID:      "user-id",
				UserMessage: "What did we decide about the project?",
				MaxEntries:  len(results),
				MaxTokens:   tt.maxTokens,
			})
			if err != nil {
				t.Fatalf("Inject() error = %v", err)
			}
			if got.Injected == 0 {
				t.Fatal("Inject() injected no memories within a usable budget")
			}
			if got.Injected >= len(results) {
				t.Fatalf("Inject() injected %d memories; want budget to truncate %d available memories", got.Injected, len(results))
			}
			if tokens := counter.Count("", got.Section); tokens > tt.wantLimit {
				t.Fatalf("Inject() section uses %d tokens; want <= %d\n%s", tokens, tt.wantLimit, got.Section)
			}
		})
	}
}
