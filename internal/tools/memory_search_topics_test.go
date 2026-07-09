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
}

func (f *memorySearchFakeMemoryStore) Search(context.Context, string, string, string, store.MemorySearchOptions) ([]store.MemorySearchResult, error) {
	return f.results, nil
}

type memorySearchFakeEpisodicStore struct {
	store.EpisodicStore
	results []store.EpisodicSearchResult
	ep      *store.EpisodicSummary
}

func (f *memorySearchFakeEpisodicStore) Search(context.Context, string, string, string, store.EpisodicSearchOptions) ([]store.EpisodicSearchResult, error) {
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
			L0Abstract: "BUV website Workshop 5 typography contrast.",
			KeyTopics:  []string{"typography", "BUV", "Workshop 5"},
			Score:      0.8,
			CreatedAt:  time.Now(),
			SessionKey: "channel:design",
		},
	}})

	ctx := store.WithUserID(store.WithAgentID(context.Background(), uuid.New()), "user-1")
	res := tool.Execute(ctx, map[string]any{"query": "BUV typography"})
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
	if strings.Join(got.KeyTopics, ",") != "typography,BUV,Workshop 5" {
		t.Fatalf("key_topics = %#v", got.KeyTopics)
	}
}

func TestMemoryExpandIncludesEpisodicKeyTopics(t *testing.T) {
	tool := NewMemoryExpandTool()
	tool.SetEpisodicStore(&memorySearchFakeEpisodicStore{ep: &store.EpisodicSummary{
		L0Abstract: "BUV website Workshop 5.",
		SessionKey: "channel:design",
		CreatedAt:  time.Date(2026, 7, 9, 14, 55, 0, 0, time.UTC),
		KeyTopics:  []string{"typography", "BUV", "Workshop 5"},
		Summary:    "Typography weight contrast is a focus area.",
	}})

	res := tool.Execute(context.Background(), map[string]any{"id": "ep-1"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "**Topics:** typography, BUV, Workshop 5") {
		t.Fatalf("topics missing from expanded memory: %s", res.ForLLM)
	}
}
