package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/vault"
)

// fakeSearchBackend supplies pre-canned results to the VaultSearchService
// via store-shaped fakes (Search service itself is real).

type vsFakeVault struct {
	store.VaultStore
	res []store.VaultSearchResult
}

func (f *vsFakeVault) Search(ctx context.Context, opts store.VaultSearchOptions) ([]store.VaultSearchResult, error) {
	return f.res, nil
}

type vsFakeEpisodic struct {
	store.EpisodicStore
	res []store.EpisodicSearchResult
}

func (f *vsFakeEpisodic) Search(ctx context.Context, query string, agentID, userID string, opts store.EpisodicSearchOptions) ([]store.EpisodicSearchResult, error) {
	return f.res, nil
}

type vsFakeKG struct {
	store.KnowledgeGraphStore
	res []store.Entity
}

func (f *vsFakeKG) SearchEntities(ctx context.Context, agentID, userID, query string, limit int) ([]store.Entity, error) {
	return f.res, nil
}

func TestVaultSearch_OutputIncludesToolHint(t *testing.T) {
	vaultFake := &vsFakeVault{res: []store.VaultSearchResult{
		{Document: store.VaultDocument{ID: "vault-id", Title: "VDoc", Path: "v.md", DocType: "context"}, Score: 0.9, Source: "vault"},
	}}
	epFake := &vsFakeEpisodic{res: []store.EpisodicSearchResult{
		{EpisodicID: "ep-id", SessionKey: "sess", L0Abstract: "abs", Score: 0.7},
	}}
	kgFake := &vsFakeKG{res: []store.Entity{
		{ID: "kg-id", Name: "KGEntity", EntityType: "document", Confidence: 0.6, Description: "desc"},
	}}

	svc := vault.NewVaultSearchService(vaultFake, epFake, kgFake)
	tool := NewVaultSearchTool()
	tool.SetSearchService(svc)

	tenantID := uuid.New()
	agentID := uuid.New()
	ctx := store.WithAgentID(store.WithTenantID(context.Background(), tenantID), agentID)

	res := tool.Execute(ctx, map[string]any{"query": "something"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	out := res.ForLLM

	// Every source must have an explicit next-tool hint.
	if !strings.Contains(out, "vault_read") {
		t.Errorf("vault hint missing: %s", out)
	}
	// kg source should redirect — accept either `knowledge_graph_search` (current tool)
	// or `kg_get` / `kg_search`; all flag namespace explicitly.
	if !strings.Contains(out, "knowledge_graph_search") && !strings.Contains(out, "kg_get") && !strings.Contains(out, "kg_search") {
		t.Errorf("kg hint missing: %s", out)
	}
	if !strings.Contains(out, "memory_search") && !strings.Contains(out, "episodic_read") {
		t.Errorf("episodic hint missing: %s", out)
	}
	// Hint marker "→ use" appears per result line.
	if strings.Count(out, "→ use") < 3 {
		t.Errorf("expected ≥3 hint markers, got: %s", out)
	}
}
