package channels

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// memoryStoreAdapter bridges store.MemoryStore to the local
// MemoryQueryer interface that voice_summarizer.go consumes. Keeping
// the adapter here (rather than depending on store from voice_summarizer)
// keeps the dependency boundary one-way: this file imports store; the
// summarizer depends only on the local interface.
type memoryStoreAdapter struct {
	ms store.MemoryStore
}

func newMemoryStoreAdapter(ms store.MemoryStore) MemoryQueryer {
	if ms == nil {
		return nil
	}
	return &memoryStoreAdapter{ms: ms}
}

func (a *memoryStoreAdapter) Search(ctx context.Context, query, agentID, userID string, opts MemorySearchOpts) ([]MemorySnippet, error) {
	results, err := a.ms.Search(ctx, query, agentID, userID, store.MemorySearchOptions{
		MaxResults: opts.MaxResults,
		MinScore:   opts.MinScore,
	})
	if err != nil {
		return nil, err
	}
	out := make([]MemorySnippet, len(results))
	for i, r := range results {
		out[i] = MemorySnippet{Path: r.Path, Snippet: r.Snippet, Score: r.Score}
	}
	return out, nil
}

func (a *memoryStoreAdapter) PutDocument(ctx context.Context, agentID, userID, path, content string) error {
	return a.ms.PutDocument(ctx, agentID, userID, path, content)
}
