package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type memorySearchFakeMemoryStore struct {
	store.MemoryStore
	results []store.MemorySearchResult
	calls   int
}

func (f *memorySearchFakeMemoryStore) Search(context.Context, string, string, string, store.MemorySearchOptions) ([]store.MemorySearchResult, error) {
	f.calls++
	return f.results, nil
}

type memorySearchFakeEpisodicStore struct {
	store.EpisodicStore
	results []store.EpisodicSearchResult
	ep      *store.EpisodicSummary
	query   string
	opts    store.EpisodicSearchOptions
}

func (f *memorySearchFakeEpisodicStore) Search(_ context.Context, query string, _ string, _ string, opts store.EpisodicSearchOptions) ([]store.EpisodicSearchResult, error) {
	f.query = query
	f.opts = opts
	return f.results, nil
}

func (f *memorySearchFakeEpisodicStore) Get(context.Context, string) (*store.EpisodicSummary, error) {
	return f.ep, nil
}

func (f *memorySearchFakeEpisodicStore) RecordRecall(context.Context, string, float64) error {
	return nil
}

func TestMemorySearchIncludesEpisodicKeyTopics(t *testing.T) {
	tool := NewMemorySearchTool()
	tool.SetMemoryStore(&memorySearchFakeMemoryStore{})
	tool.SetEpisodicStore(&memorySearchFakeEpisodicStore{results: []store.EpisodicSearchResult{
		{
			EpisodicID: "ep-1",
			L0Abstract: "Project Alpha workshop notes mention visual hierarchy.",
			KeyTopics:  []string{"visual-hierarchy", "Project Alpha", "Workshop Notes"},
			Score:      0.8,
			CreatedAt:  time.Now(),
			SessionKey: "channel:design",
		},
	}})

	ctx := store.WithUserID(store.WithAgentID(context.Background(), uuid.New()), "user-1")
	res := tool.Execute(ctx, map[string]any{"query": "Project Alpha visual hierarchy"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}

	var output struct {
		Results []struct {
			Tier       string   `json:"tier"`
			EpisodicID string   `json:"episodic_id"`
			KeyTopics  []string `json:"key_topics"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(res.ForLLM), &output); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, res.ForLLM)
	}
	if len(output.Results) != 1 {
		t.Fatalf("results = %d, want 1: %s", len(output.Results), res.ForLLM)
	}
	got := output.Results[0]
	if got.Tier != "episodic" || got.EpisodicID != "ep-1" {
		t.Fatalf("episodic result metadata = %+v", got)
	}
	if strings.Join(got.KeyTopics, ",") != "visual-hierarchy,Project Alpha,Workshop Notes" {
		t.Fatalf("key_topics = %#v", got.KeyTopics)
	}
}

func TestMemorySearchTimeRangeListsEpisodicOnly(t *testing.T) {
	createdAt := time.Date(2026, 7, 9, 17, 21, 29, 0, time.UTC)
	expiresAt := createdAt.Add(7 * 24 * time.Hour)
	mem := &memorySearchFakeMemoryStore{}
	ep := &memorySearchFakeEpisodicStore{results: []store.EpisodicSearchResult{
		{
			EpisodicID: "ep-1",
			L0Abstract: "Project Alpha workshop notes mention visual hierarchy.",
			KeyTopics:  []string{"visual-hierarchy", "Project Alpha"},
			Score:      1,
			CreatedAt:  createdAt,
			ExpiresAt:  &expiresAt,
			SessionKey: "channel:design",
		},
	}}
	tool := NewMemorySearchTool()
	tool.SetMemoryStore(mem)
	tool.SetEpisodicStore(ep)

	ctx := store.WithUserID(store.WithAgentID(context.Background(), uuid.New()), "user-1")
	res := tool.Execute(ctx, map[string]any{"query": "*", "timeRange": "24h"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if mem.calls != 0 {
		t.Fatalf("document memory search calls = %d, want 0 for query='*'", mem.calls)
	}
	if ep.query != "*" {
		t.Fatalf("episodic query = %q, want *", ep.query)
	}
	if ep.opts.CreatedAfter == nil {
		t.Fatal("CreatedAfter was not forwarded")
	}
	if ep.opts.IncludeExpired {
		t.Fatal("IncludeExpired default = true, want false")
	}

	var output struct {
		Results []struct {
			CreatedAt string  `json:"created_at"`
			ExpiresAt *string `json:"expires_at"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(res.ForLLM), &output); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, res.ForLLM)
	}
	if len(output.Results) != 1 {
		t.Fatalf("results = %d, want 1: %s", len(output.Results), res.ForLLM)
	}
	if output.Results[0].CreatedAt == "" {
		t.Fatalf("created_at missing: %s", res.ForLLM)
	}
	if output.Results[0].ExpiresAt == nil || *output.Results[0].ExpiresAt == "" {
		t.Fatalf("expires_at missing: %s", res.ForLLM)
	}
}

func TestMemoryExpandIncludesEpisodicKeyTopics(t *testing.T) {
	tool := NewMemoryExpandTool()
	tool.SetEpisodicStore(&memorySearchFakeEpisodicStore{ep: &store.EpisodicSummary{
		L0Abstract: "Project Alpha workshop notes.",
		SessionKey: "channel:design",
		CreatedAt:  time.Date(2026, 7, 9, 14, 55, 0, 0, time.UTC),
		KeyTopics:  []string{"visual-hierarchy", "Project Alpha", "Workshop Notes"},
		Summary:    "Visual hierarchy is a focus area.",
	}})

	res := tool.Execute(context.Background(), map[string]any{"id": "ep-1"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "**Topics:** visual-hierarchy, Project Alpha, Workshop Notes") {
		t.Fatalf("topics missing from expanded memory: %s", res.ForLLM)
	}
}
