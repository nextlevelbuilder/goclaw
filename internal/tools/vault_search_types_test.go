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

	// Per-source id field names match each downstream tool's input param —
	// the schema-level fix that prevents LLM from misrouting a foreign id
	// into vault_read.
	if !strings.Contains(out, "doc_id: vault-id") {
		t.Errorf("vault result must use 'doc_id:' field: %s", out)
	}
	if !strings.Contains(out, "entity_id: kg-id") {
		t.Errorf("kg result must use 'entity_id:' field: %s", out)
	}
	if !strings.Contains(out, "episodic_id: ep-id") {
		t.Errorf("episodic result must use 'episodic_id:' field: %s", out)
	}
	// Generic `id:` label must not appear — it is the honeypot that caused
	// the original bug (LLM pattern-matched `id:` regex to doc_id).
	if strings.Contains(out, " id: ") {
		t.Errorf("generic ' id: ' label leaked into output: %s", out)
	}
	// Follow-up tool hint still present per result as secondary signal.
	if !strings.Contains(out, "vault_read") {
		t.Errorf("vault hint missing: %s", out)
	}
	if !strings.Contains(out, "knowledge_graph_search") {
		t.Errorf("kg hint missing: %s", out)
	}
	if !strings.Contains(out, "memory_expand") {
		t.Errorf("episodic hint missing: %s", out)
	}
}
