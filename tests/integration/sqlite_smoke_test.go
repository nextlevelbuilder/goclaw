//go:build sqliteonly

package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/sqlitestore"
)

// TestSQLiteSmokeTest boots a SQLite gateway with all stores and exercises each new store.
func TestSQLiteSmokeTest(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "smoke.db")
	skillDir := filepath.Join(tmpDir, "skills")
	os.MkdirAll(skillDir, 0o755)

	stores, err := sqlitestore.NewSQLiteStores(store.StoreConfig{
		SQLitePath:       dbPath,
		SkillsStorageDir: skillDir,
		EncryptionKey:    "test-key-32-bytes-long-for-aes!!", // 32 bytes for AES-256
	})
	if err != nil {
		t.Fatalf("NewSQLiteStores: %v", err)
	}
	defer stores.DB.Close()

	// Verify all 9 previously-nil stores are now non-nil.
	t.Run("FactoryAllStoresNonNil", func(t *testing.T) {
		if stores.AgentLinks == nil {
			t.Error("AgentLinks is nil")
		}
		if stores.SubagentTasks == nil {
			t.Error("SubagentTasks is nil")
		}
		if stores.SecureCLI == nil {
			t.Error("SecureCLI is nil")
		}
		if stores.SecureCLIGrants == nil {
			t.Error("SecureCLIGrants is nil")
		}
		if stores.Episodic == nil {
			t.Error("Episodic is nil")
		}
		if stores.EvolutionMetrics == nil {
			t.Error("EvolutionMetrics is nil")
		}
		if stores.EvolutionSuggestions == nil {
			t.Error("EvolutionSuggestions is nil")
		}
		if stores.KnowledgeGraph == nil {
			t.Error("KnowledgeGraph is nil")
		}
		if stores.Vault == nil {
			t.Error("Vault is nil")
		}
	})

	// Seed a tenant + agent for FK satisfaction.
	tenantID := uuid.Must(uuid.NewV7())
	agentID := uuid.Must(uuid.NewV7())
	agentKey := "smoke-" + agentID.String()[:8]
	userID := "smoke-user"

	_, err = stores.DB.Exec(
		`INSERT INTO tenants (id, name, slug, status) VALUES (?, 'Smoke', 'smoke', 'active')`,
		tenantID.String())
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	_, err = stores.DB.Exec(
		`INSERT INTO agents (id, agent_key, display_name, status, tenant_id, owner_id, model, provider)
		 VALUES (?, ?, 'Smoke Agent', 'active', ?, 'smoke-owner', 'gpt-4o', 'openai')`,
		agentID.String(), agentKey, tenantID.String())
	if err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	ctx := store.WithTenantID(context.Background(), tenantID)

	// --- SubagentTasks round-trip ---
	t.Run("SubagentTasks", func(t *testing.T) {
		taskID := uuid.Must(uuid.NewV7())
		task := &store.SubagentTaskData{
			ParentAgentKey: agentKey,
			Subject:        "smoke task",
			Description:    "test",
			Status:         "running",
			Depth:          1,
			Metadata:       map[string]any{"key": "val"},
		}
		task.ID = taskID
		task.TenantID = tenantID
		if err := stores.SubagentTasks.Create(ctx, task); err != nil {
			t.Fatalf("Create: %v", err)
		}
		got, err := stores.SubagentTasks.Get(ctx, taskID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got == nil || got.Subject != "smoke task" {
			t.Fatalf("unexpected: %+v", got)
		}
	})

	// --- EvolutionMetrics round-trip ---
	t.Run("EvolutionMetrics", func(t *testing.T) {
		metric := store.EvolutionMetric{
			ID:         uuid.Must(uuid.NewV7()),
			TenantID:   tenantID,
			AgentID:    agentID,
			SessionKey: "s1",
			MetricType: "tool",
			MetricKey:  "exec",
			Value:      json.RawMessage(`{"success":"true"}`),
		}
		if err := stores.EvolutionMetrics.RecordMetric(ctx, metric); err != nil {
			t.Fatalf("RecordMetric: %v", err)
		}
		metrics, err := stores.EvolutionMetrics.QueryMetrics(ctx, agentID, "tool", time.Time{}, 10)
		if err != nil {
			t.Fatalf("QueryMetrics: %v", err)
		}
		if len(metrics) == 0 {
			t.Fatal("expected metrics")
		}
	})

	// --- EpisodicStore round-trip ---
	t.Run("Episodic", func(t *testing.T) {
		summary := &store.EpisodicSummary{
			ID:         uuid.Must(uuid.NewV7()),
			TenantID:   tenantID,
			AgentID:    agentID,
			UserID:     userID,
			SessionKey: "s1",
			Summary:    "discussed machine learning algorithms",
			L0Abstract: "ML discussion",
			KeyTopics:  []string{"ml", "algorithms"},
			SourceType: "session",
			TurnCount:  5,
			TokenCount: 100,
		}
		if err := stores.Episodic.Create(ctx, summary); err != nil {
			t.Fatalf("Create: %v", err)
		}
		results, err := stores.Episodic.Search(ctx, "machine", agentID.String(), userID, store.EpisodicSearchOptions{MaxResults: 5})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected search results")
		}
	})

	// --- KnowledgeGraph round-trip ---
	t.Run("KnowledgeGraph", func(t *testing.T) {
		entityA := &store.Entity{
			ID:         uuid.Must(uuid.NewV7()).String(),
			AgentID:    agentID.String(),
			UserID:     userID,
			ExternalID: "ext-a",
			Name:       "Alice",
			EntityType: "person",
			Confidence: 0.9,
			Properties: map[string]string{"role": "engineer"},
		}
		entityB := &store.Entity{
			ID:         uuid.Must(uuid.NewV7()).String(),
			AgentID:    agentID.String(),
			UserID:     userID,
			ExternalID: "ext-b",
			Name:       "Bob",
			EntityType: "person",
			Confidence: 0.9,
		}
		if err := stores.KnowledgeGraph.UpsertEntity(ctx, entityA); err != nil {
			t.Fatalf("UpsertEntity A: %v", err)
		}
		if err := stores.KnowledgeGraph.UpsertEntity(ctx, entityB); err != nil {
			t.Fatalf("UpsertEntity B: %v", err)
		}

		rel := &store.Relation{
			ID:             uuid.Must(uuid.NewV7()).String(),
			AgentID:        agentID.String(),
			UserID:         userID,
			SourceEntityID: entityA.ID,
			TargetEntityID: entityB.ID,
			RelationType:   "knows",
			Confidence:     0.8,
		}
		if err := stores.KnowledgeGraph.UpsertRelation(ctx, rel); err != nil {
			t.Fatalf("UpsertRelation: %v", err)
		}

		results, err := stores.KnowledgeGraph.Traverse(ctx, agentID.String(), userID, entityA.ID, 3)
		if err != nil {
			t.Fatalf("Traverse: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected traversal results")
		}
	})

	// --- VaultStore round-trip ---
	t.Run("Vault", func(t *testing.T) {
		doc := &store.VaultDocument{
			TenantID:    tenantID.String(),
			AgentID:     agentID.String(),
			Scope:       "personal",
			Path:        "notes/meeting.md",
			Title:       "Meeting Notes",
			DocType:     "note",
			ContentHash: "abc123",
			Metadata:    map[string]any{"tags": []string{"meeting"}},
		}
		if err := stores.Vault.UpsertDocument(ctx, doc); err != nil {
			t.Fatalf("UpsertDocument: %v", err)
		}
		if doc.ID == "" {
			t.Fatal("expected ID set after upsert")
		}

		got, err := stores.Vault.GetDocument(ctx, tenantID.String(), agentID.String(), "notes/meeting.md")
		if err != nil {
			t.Fatalf("GetDocument: %v", err)
		}
		if got.Title != "Meeting Notes" {
			t.Fatalf("unexpected title: %s", got.Title)
		}

		// Search
		results, err := stores.Vault.Search(ctx, store.VaultSearchOptions{
			Query:    "meeting",
			AgentID:  agentID.String(),
			TenantID: tenantID.String(),
		})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(results) == 0 {
			t.Fatal("expected vault search results")
		}

		// Links
		doc2 := &store.VaultDocument{
			TenantID:    tenantID.String(),
			AgentID:     agentID.String(),
			Scope:       "personal",
			Path:        "notes/followup.md",
			Title:       "Follow-up",
			DocType:     "note",
			ContentHash: "def456",
		}
		if err := stores.Vault.UpsertDocument(ctx, doc2); err != nil {
			t.Fatalf("UpsertDocument2: %v", err)
		}

		link := &store.VaultLink{
			FromDocID: doc.ID,
			ToDocID:   doc2.ID,
			LinkType:  "wikilink",
			Context:   "see also",
		}
		if err := stores.Vault.CreateLink(ctx, link); err != nil {
			t.Fatalf("CreateLink: %v", err)
		}

		outLinks, err := stores.Vault.GetOutLinks(ctx, tenantID.String(), doc.ID)
		if err != nil {
			t.Fatalf("GetOutLinks: %v", err)
		}
		if len(outLinks) != 1 {
			t.Fatalf("expected 1 outlink, got %d", len(outLinks))
		}
	})

	// --- SecureCLI encrypt/decrypt ---
	t.Run("SecureCLIEncryptDecrypt", func(t *testing.T) {
		binID := uuid.Must(uuid.NewV7())
		env := map[string]string{"API_KEY": "secret123"}
		envBytes, _ := json.Marshal(env)

		bin := &store.SecureCLIBinary{
			BinaryName:     "smoke-cli",
			Description:    "test binary",
			EncryptedEnv:   envBytes,
			DenyArgs:       json.RawMessage(`[]`),
			DenyVerbose:    json.RawMessage(`[]`),
			TimeoutSeconds: 30,
			IsGlobal:       true,
			Enabled:        true,
		}
		bin.ID = binID
		if err := stores.SecureCLI.Create(ctx, bin); err != nil {
			t.Fatalf("Create: %v", err)
		}

		got, err := stores.SecureCLI.Get(ctx, binID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got == nil {
			t.Fatal("expected non-nil binary")
		}
		// Verify decrypted env matches original.
		var gotEnv map[string]string
		if err := json.Unmarshal(got.EncryptedEnv, &gotEnv); err != nil {
			t.Fatalf("unmarshal env: %v", err)
		}
		if gotEnv["API_KEY"] != "secret123" {
			t.Fatalf("expected API_KEY=secret123, got %s", gotEnv["API_KEY"])
		}
	})

	// --- F15: SecureCLI nil when no encryption key ---
	t.Run("SecureCLINilWithoutKey", func(t *testing.T) {
		tmpDir2 := t.TempDir()
		dbPath2 := filepath.Join(tmpDir2, "nokey.db")
		skillDir2 := filepath.Join(tmpDir2, "skills")
		os.MkdirAll(skillDir2, 0o755)

		stores2, err := sqlitestore.NewSQLiteStores(store.StoreConfig{
			SQLitePath:       dbPath2,
			SkillsStorageDir: skillDir2,
			EncryptionKey:    "", // empty = disabled
		})
		if err != nil {
			t.Fatalf("NewSQLiteStores: %v", err)
		}
		defer stores2.DB.Close()

		if stores2.SecureCLI != nil {
			t.Error("expected SecureCLI to be nil when EncryptionKey is empty")
		}
	})
}
