package vault

import (
	"context"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// fakeVaultStoreSearch embeds store.VaultStore to satisfy the interface;
// only Search is exercised.
type fakeVaultStoreSearch struct {
	store.VaultStore
	results []store.VaultSearchResult
	calls   int
}

func (f *fakeVaultStoreSearch) Search(ctx context.Context, opts store.VaultSearchOptions) ([]store.VaultSearchResult, error) {
	f.calls++
	return f.results, nil
}

type fakeEpisodicStoreSearch struct {
	store.EpisodicStore
	results []store.EpisodicSearchResult
	calls   int
}

func (f *fakeEpisodicStoreSearch) Search(ctx context.Context, query string, agentID, userID string, opts store.EpisodicSearchOptions) ([]store.EpisodicSearchResult, error) {
	f.calls++
	return f.results, nil
}

type fakeKGStoreSearch struct {
	store.KnowledgeGraphStore
	entities []store.Entity
	calls    int
}

func (f *fakeKGStoreSearch) SearchEntities(ctx context.Context, agentID, userID, query string, limit int) ([]store.Entity, error) {
	f.calls++
	return f.entities, nil
}

func makeVaultResult(id, title string) store.VaultSearchResult {
	return store.VaultSearchResult{
		Document: store.VaultDocument{ID: id, Title: title, Path: title + ".md", DocType: "context"},
		Score:    0.8,
		Source:   "vault",
	}
}

func makeEpisodicResult(id, key string) store.EpisodicSearchResult {
	return store.EpisodicSearchResult{
		EpisodicID: id,
		SessionKey: key,
		L0Abstract: "abstract",
		Score:      0.5,
		CreatedAt:  time.Now(),
	}
}

func makeEntity(id, name string) store.Entity {
	return store.Entity{ID: id, Name: name, EntityType: "document", Confidence: 0.7}
}

// --- Test 1: types filter excludes KG/episodic when a narrow type is requested. ---
func TestSearch_TypesFilterExcludesKG(t *testing.T) {
	vs := &fakeVaultStoreSearch{results: []store.VaultSearchResult{makeVaultResult("V1", "VaultDoc")}}
	es := &fakeEpisodicStoreSearch{results: []store.EpisodicSearchResult{makeEpisodicResult("E1", "sess")}}
	kg := &fakeKGStoreSearch{entities: []store.Entity{makeEntity("K1", "KGDoc")}}

	svc := NewVaultSearchService(vs, es, kg)
	res, err := svc.Search(context.Background(), UnifiedSearchOptions{
		Query:      "q",
		AgentID:    "a",
		TenantID:   "t",
		DocTypes:   []string{"context"},
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	for _, r := range res {
		if r.Source == "kg" {
			t.Fatalf("KG result leaked when types=context: %+v", r)
		}
		if r.Source == "episodic" {
			t.Fatalf("episodic result leaked when types=context: %+v", r)
		}
	}
	if kg.calls != 0 {
		t.Fatalf("kg store should not be called when DocTypes=[context]; got %d calls", kg.calls)
	}
	if es.calls != 0 {
		t.Fatalf("episodic store should not be called when DocTypes=[context]; got %d calls", es.calls)
	}
}

// --- Test 1b: empty DocTypes still fans out to all sources. ---
func TestSearch_TypesEmptyIncludesAll(t *testing.T) {
	vs := &fakeVaultStoreSearch{results: []store.VaultSearchResult{makeVaultResult("V1", "VaultDoc")}}
	es := &fakeEpisodicStoreSearch{results: []store.EpisodicSearchResult{makeEpisodicResult("E1", "sess")}}
	kg := &fakeKGStoreSearch{entities: []store.Entity{makeEntity("K1", "KGDoc")}}

	svc := NewVaultSearchService(vs, es, kg)
	res, err := svc.Search(context.Background(), UnifiedSearchOptions{
		Query:      "q",
		AgentID:    "a",
		TenantID:   "t",
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	sources := map[string]bool{}
	for _, r := range res {
		sources[r.Source] = true
	}
	if !sources["vault"] || !sources["episodic"] || !sources["kg"] {
		t.Fatalf("expected all 3 sources, got: %v", sources)
	}
	if kg.calls != 1 || es.calls != 1 || vs.calls != 1 {
		t.Fatalf("expected 1 call each; got vault=%d ep=%d kg=%d", vs.calls, es.calls, kg.calls)
	}
}

// --- Test 1c: DocTypes includes "kg" → kg fan-out runs. ---
func TestSearch_TypesIncludesKG(t *testing.T) {
	vs := &fakeVaultStoreSearch{}
	kg := &fakeKGStoreSearch{entities: []store.Entity{makeEntity("K1", "KGDoc")}}

	svc := NewVaultSearchService(vs, nil, kg)
	res, err := svc.Search(context.Background(), UnifiedSearchOptions{
		Query:      "q",
		AgentID:    "a",
		TenantID:   "t",
		DocTypes:   []string{"kg"},
		MaxResults: 10,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if kg.calls != 1 {
		t.Fatalf("kg should run when DocTypes=[kg]; calls=%d", kg.calls)
	}
	found := false
	for _, r := range res {
		if r.Source == "kg" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected kg result, got: %+v", res)
	}
}
