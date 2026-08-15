//go:build sqliteonly

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/sqlitestore"
)

// newSQLiteTestStores creates a SQLite-backed store for integration tests.
func newSQLiteTestStores(t *testing.T) *store.Stores {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	skillDir := filepath.Join(tmpDir, "skills")
	os.MkdirAll(skillDir, 0o755)

	stores, err := sqlitestore.NewSQLiteStores(store.StoreConfig{
		SQLitePath:       dbPath,
		SkillsStorageDir: skillDir,
		EncryptionKey:    "test-key-32-bytes-long-for-aes!!",
	})
	if err != nil {
		t.Fatalf("NewSQLiteStores: %v", err)
	}
	t.Cleanup(func() { stores.DB.Close() })
	return stores
}

// TestSQLiteVaultStore_Search_SharedDocsVisible verifies that Search returns
// shared (agent_id=NULL) documents alongside personal (agent_id=X) documents
// when AgentID is specified.
func TestSQLiteVaultStore_Search_SharedDocsVisible(t *testing.T) {
	stores := newSQLiteTestStores(t)

	tenantID := uuid.Must(uuid.NewV7())
	agentID := uuid.Must(uuid.NewV7())
	agentIDStr := agentID.String()

	// Seed tenant + agent.
	stores.DB.Exec(`INSERT INTO tenants (id, name, slug, status) VALUES (?, 'T', 'srch', 'active')`, tenantID.String())
	stores.DB.Exec(
		`INSERT INTO agents (id, agent_key, display_name, status, tenant_id, owner_id, model, provider)
		 VALUES (?, 'srch-agt', 'A', 'active', ?, 'owner', 'gpt-4o', 'openai')`,
		agentIDStr, tenantID.String())

	ctx := store.WithTenantID(context.Background(), tenantID)

	// Personal doc (agent-owned).
	personalDoc := &store.VaultDocument{
		TenantID:    tenantID.String(),
		AgentID:     &agentIDStr,
		Scope:       "personal",
		Path:        "agents/a1/srch-personal.md",
		Title:       "Personal Search Doc",
		DocType:     "note",
		ContentHash: "ph1",
	}
	if err := stores.Vault.UpsertDocument(ctx, personalDoc); err != nil {
		t.Fatalf("UpsertDocument personal: %v", err)
	}

	// Shared doc (agent_id=NULL, scope=shared).
	sharedDoc := &store.VaultDocument{
		TenantID:    tenantID.String(),
		AgentID:     nil,
		Scope:       "shared",
		Path:        "shared/srch-shared.md",
		Title:       "Shared Search Doc",
		DocType:     "note",
		ContentHash: "sh1",
	}
	if err := stores.Vault.UpsertDocument(ctx, sharedDoc); err != nil {
		t.Fatalf("UpsertDocument shared: %v", err)
	}

	results, err := stores.Vault.Search(ctx, store.VaultSearchOptions{
		Query:    "Search Doc",
		AgentID:  agentIDStr,
		TenantID: tenantID.String(),
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	found := map[string]bool{}
	for _, r := range results {
		found[r.Document.Path] = true
	}

	if !found["agents/a1/srch-personal.md"] {
		t.Errorf("Search: personal doc not found in results %v", results)
	}
	if !found["shared/srch-shared.md"] {
		t.Errorf("Search: shared doc (agent_id=NULL) not found in results — BUG: shared docs excluded")
	}
}

// TestSQLiteVaultStore_List_IncludesShared verifies that ListDocuments returns
// shared (agent_id=NULL) documents alongside personal (agent_id=X) documents
// when agentID is specified.
func TestSQLiteVaultStore_List_IncludesShared(t *testing.T) {
	stores := newSQLiteTestStores(t)

	tenantID := uuid.Must(uuid.NewV7())
	agentID := uuid.Must(uuid.NewV7())
	agentIDStr := agentID.String()

	// Seed tenant + agent.
	stores.DB.Exec(`INSERT INTO tenants (id, name, slug, status) VALUES (?, 'T', 'list', 'active')`, tenantID.String())
	stores.DB.Exec(
		`INSERT INTO agents (id, agent_key, display_name, status, tenant_id, owner_id, model, provider)
		 VALUES (?, 'list-agt', 'A', 'active', ?, 'owner', 'gpt-4o', 'openai')`,
		agentIDStr, tenantID.String())

	ctx := store.WithTenantID(context.Background(), tenantID)

	// Personal doc.
	personalDoc := &store.VaultDocument{
		TenantID:    tenantID.String(),
		AgentID:     &agentIDStr,
		Scope:       "personal",
		Path:        "agents/a1/list-personal.md",
		Title:       "Personal List Doc",
		DocType:     "note",
		ContentHash: "lph1",
	}
	if err := stores.Vault.UpsertDocument(ctx, personalDoc); err != nil {
		t.Fatalf("UpsertDocument personal: %v", err)
	}

	// Shared doc.
	sharedDoc := &store.VaultDocument{
		TenantID:    tenantID.String(),
		AgentID:     nil,
		Scope:       "shared",
		Path:        "shared/list-shared.md",
		Title:       "Shared List Doc",
		DocType:     "note",
		ContentHash: "lsh1",
	}
	if err := stores.Vault.UpsertDocument(ctx, sharedDoc); err != nil {
		t.Fatalf("UpsertDocument shared: %v", err)
	}

	docs, err := stores.Vault.ListDocuments(ctx, tenantID.String(), agentIDStr, store.VaultListOptions{Limit: 50})
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}

	found := map[string]bool{}
	for _, d := range docs {
		found[d.Path] = true
	}

	if !found["agents/a1/list-personal.md"] {
		t.Errorf("ListDocuments: personal doc not found in results %v", docs)
	}
	if !found["shared/list-shared.md"] {
		t.Errorf("ListDocuments: shared doc (agent_id=NULL) not found in results — BUG: shared docs excluded")
	}
}
