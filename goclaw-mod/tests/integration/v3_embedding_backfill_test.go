//go:build integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func TestStoreVault_BackfillEmbeddings_Idempotent(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	storeWithoutProvider := pg.NewPGVaultStore(db)

	docWithSummary := makeVaultDoc(tenantID.String(), agentID.String(), "backfill/summary.md", "Recovery summary")
	docWithSummary.Summary = "Semantic recovery content"
	if err := storeWithoutProvider.UpsertDocument(ctx, docWithSummary); err != nil {
		t.Fatalf("UpsertDocument with summary: %v", err)
	}
	docWithoutSummary := makeVaultDoc(tenantID.String(), agentID.String(), "backfill/path-only.md", "Recovery path")
	if err := storeWithoutProvider.UpsertDocument(ctx, docWithoutSummary); err != nil {
		t.Fatalf("UpsertDocument without summary: %v", err)
	}

	storeWithoutProvider.SetEmbeddingProvider(newMockEmbedProvider())
	updated, err := storeWithoutProvider.BackfillVaultEmbeddings(ctx)
	if err != nil {
		t.Fatalf("BackfillVaultEmbeddings: %v", err)
	}
	if updated != 2 {
		t.Fatalf("BackfillVaultEmbeddings updated = %d, want 2", updated)
	}
	assertEmbeddingPresent(t, db, "SELECT embedding IS NOT NULL FROM vault_documents WHERE id = $1", "vault document", docWithSummary.ID)
	assertEmbeddingPresent(t, db, "SELECT embedding IS NOT NULL FROM vault_documents WHERE id = $1", "vault document", docWithoutSummary.ID)

	updated, err = storeWithoutProvider.BackfillVaultEmbeddings(ctx)
	if err != nil {
		t.Fatalf("BackfillVaultEmbeddings second call: %v", err)
	}
	if updated != 0 {
		t.Fatalf("BackfillVaultEmbeddings second call updated = %d, want 0", updated)
	}
}

func TestStoreEpisodic_BackfillEmbeddings_Idempotent(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	storeWithoutProvider := pg.NewPGEpisodicStore(db)
	episode := &store.EpisodicSummary{
		TenantID:   tenantID,
		AgentID:    agentID,
		UserID:     "embedding-backfill-user",
		SessionKey: "embedding-backfill-session",
		Summary:    "The user decided to restore all missing semantic vectors.",
		SourceType: "session",
		SourceID:   "embedding-backfill-" + uuid.NewString(),
	}
	if err := storeWithoutProvider.Create(ctx, episode); err != nil {
		t.Fatalf("Create episodic summary: %v", err)
	}
	expiredAt := time.Now().Add(-time.Hour)
	expiredEpisode := &store.EpisodicSummary{
		TenantID:   tenantID,
		AgentID:    agentID,
		UserID:     "embedding-backfill-user",
		SessionKey: "expired-embedding-backfill-session",
		Summary:    "This expired summary must not consume provider capacity.",
		SourceType: "session",
		SourceID:   "expired-embedding-backfill-" + uuid.NewString(),
		ExpiresAt:  &expiredAt,
	}
	if err := storeWithoutProvider.Create(ctx, expiredEpisode); err != nil {
		t.Fatalf("Create expired episodic summary: %v", err)
	}

	storeWithoutProvider.SetEmbeddingProvider(newMockEmbedProvider())
	updated, err := storeWithoutProvider.BackfillEpisodicEmbeddings(ctx)
	if err != nil {
		t.Fatalf("BackfillEpisodicEmbeddings: %v", err)
	}
	if updated != 1 {
		t.Fatalf("BackfillEpisodicEmbeddings updated = %d, want 1", updated)
	}
	assertEmbeddingPresent(t, db, "SELECT embedding IS NOT NULL FROM episodic_summaries WHERE id = $1", "episodic summary", episode.ID.String())
	assertEmbeddingMissing(t, db, "SELECT embedding IS NULL FROM episodic_summaries WHERE id = $1", "expired episodic summary", expiredEpisode.ID.String())

	updated, err = storeWithoutProvider.BackfillEpisodicEmbeddings(ctx)
	if err != nil {
		t.Fatalf("BackfillEpisodicEmbeddings second call: %v", err)
	}
	if updated != 0 {
		t.Fatalf("BackfillEpisodicEmbeddings second call updated = %d, want 0", updated)
	}
}

func TestStoreAgent_BackfillEmbeddings_Idempotent(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	agentStore := pg.NewPGAgentStore(db)

	if err := agentStore.Update(ctx, agentID, map[string]any{
		"display_name": "Recovery agent",
		"frontmatter":  "An agent with semantic routing metadata",
	}); err != nil {
		t.Fatalf("Update agent frontmatter: %v", err)
	}

	agentStore.SetEmbeddingProvider(newMockEmbedProvider())
	updated, err := agentStore.BackfillAgentEmbeddings(ctx)
	if err != nil {
		t.Fatalf("BackfillAgentEmbeddings: %v", err)
	}
	if updated != 1 {
		t.Fatalf("BackfillAgentEmbeddings updated = %d, want 1", updated)
	}
	assertEmbeddingPresent(t, db, "SELECT embedding IS NOT NULL FROM agents WHERE id = $1", "agent", agentID.String())

	updated, err = agentStore.BackfillAgentEmbeddings(ctx)
	if err != nil {
		t.Fatalf("BackfillAgentEmbeddings second call: %v", err)
	}
	if updated != 0 {
		t.Fatalf("BackfillAgentEmbeddings second call updated = %d, want 0", updated)
	}
}

func TestStoreAgent_AsyncEmbeddingDoesNotOverwriteNewerSource(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	provider := newBlockingAgentEmbedProvider()
	agentStore := pg.NewPGAgentStore(db)
	agentStore.SetEmbeddingProvider(provider)

	if err := agentStore.Update(ctx, agentID, map[string]any{
		"display_name": "Versioned agent",
		"frontmatter":  "old routing metadata",
	}); err != nil {
		t.Fatalf("Update old agent source: %v", err)
	}
	select {
	case <-provider.oldStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("old embedding request did not start")
	}

	if err := agentStore.Update(ctx, agentID, map[string]any{
		"frontmatter": "new routing metadata",
	}); err != nil {
		t.Fatalf("Update new agent source: %v", err)
	}
	assertAgentEmbeddingEquals(t, db, agentID, vectorLiteral(0.2))

	close(provider.releaseOld)
	select {
	case <-provider.oldDone:
	case <-time.After(3 * time.Second):
		t.Fatal("old embedding request did not finish")
	}
	assertAgentEmbeddingEquals(t, db, agentID, vectorLiteral(0.2))
}

func TestStoreVault_BackfillEmbeddings_RejectsIncompleteBatch(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	vaultStore := pg.NewPGVaultStore(db)
	doc := makeVaultDoc(tenantID.String(), agentID.String(), "backfill/incomplete.md", "Incomplete response")
	doc.Summary = "This row must remain pending when the provider drops a vector."
	if err := vaultStore.UpsertDocument(ctx, doc); err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}

	vaultStore.SetEmbeddingProvider(incompleteEmbedProvider{})
	if _, err := vaultStore.BackfillVaultEmbeddings(ctx); err == nil {
		t.Fatal("BackfillVaultEmbeddings error = nil, want incomplete batch error")
	}
	assertEmbeddingMissing(t, db, "SELECT embedding IS NULL FROM vault_documents WHERE id = $1", "vault document", doc.ID)
}

func TestStoreVault_BackfillEmbeddings_PoisonRowDoesNotStarveLaterRows(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	vaultStore := pg.NewPGVaultStore(db)

	poison := makeVaultDoc(tenantID.String(), agentID.String(), "backfill/poison.md", "Poison row")
	poison.Summary = "poison embedding input"
	valid := makeVaultDoc(tenantID.String(), agentID.String(), "backfill/valid.md", "Valid row")
	valid.Summary = "valid embedding input"
	if err := vaultStore.UpsertDocument(ctx, poison); err != nil {
		t.Fatalf("Upsert poison document: %v", err)
	}
	if err := vaultStore.UpsertDocument(ctx, valid); err != nil {
		t.Fatalf("Upsert valid document: %v", err)
	}

	vaultStore.SetEmbeddingProvider(poisonEmbedProvider{delegate: newMockEmbedProvider()})
	updated, err := vaultStore.BackfillVaultEmbeddings(ctx)
	if err == nil {
		t.Fatal("BackfillVaultEmbeddings error = nil, want poison-row error")
	}
	if updated != 1 {
		t.Fatalf("BackfillVaultEmbeddings updated = %d, want 1 valid row", updated)
	}
	assertEmbeddingMissing(t, db, "SELECT embedding IS NULL FROM vault_documents WHERE id = $1", "poison vault document", poison.ID)
	assertEmbeddingPresent(t, db, "SELECT embedding IS NOT NULL FROM vault_documents WHERE id = $1", "valid vault document", valid.ID)
}

func assertEmbeddingPresent(t *testing.T, db queryRower, query, surface, id string) {
	t.Helper()
	var present bool
	if err := db.QueryRow(query, id).Scan(&present); err != nil {
		t.Fatalf("query %s embedding: %v", surface, err)
	}
	if !present {
		t.Fatalf("expected %s embedding to be present", surface)
	}
}

func assertEmbeddingMissing(t *testing.T, db queryRower, query, surface, id string) {
	t.Helper()
	var missing bool
	if err := db.QueryRow(query, id).Scan(&missing); err != nil {
		t.Fatalf("query %s embedding: %v", surface, err)
	}
	if !missing {
		t.Fatalf("expected %s embedding to remain missing", surface)
	}
}

type queryRower interface {
	QueryRow(query string, args ...any) *sql.Row
}

type incompleteEmbedProvider struct{}

func (incompleteEmbedProvider) Name() string  { return "incomplete" }
func (incompleteEmbedProvider) Model() string { return "incomplete" }
func (incompleteEmbedProvider) Embed(context.Context, []string) ([][]float32, error) {
	return nil, nil
}

var _ store.EmbeddingProvider = incompleteEmbedProvider{}

type poisonEmbedProvider struct {
	delegate store.EmbeddingProvider
}

func (p poisonEmbedProvider) Name() string  { return "poison" }
func (p poisonEmbedProvider) Model() string { return "poison" }
func (p poisonEmbedProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	for _, text := range texts {
		if strings.Contains(text, "poison") {
			return nil, errors.New("rejected poison input")
		}
	}
	return p.delegate.Embed(ctx, texts)
}

var _ store.EmbeddingProvider = poisonEmbedProvider{}

type blockingAgentEmbedProvider struct {
	oldStarted chan struct{}
	releaseOld chan struct{}
	oldDone    chan struct{}
	startOnce  sync.Once
	doneOnce   sync.Once
}

func newBlockingAgentEmbedProvider() *blockingAgentEmbedProvider {
	return &blockingAgentEmbedProvider{
		oldStarted: make(chan struct{}),
		releaseOld: make(chan struct{}),
		oldDone:    make(chan struct{}),
	}
}

func (p *blockingAgentEmbedProvider) Name() string  { return "blocking-agent" }
func (p *blockingAgentEmbedProvider) Model() string { return "blocking-agent" }
func (p *blockingAgentEmbedProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	value := float32(0.2)
	if strings.Contains(texts[0], "old routing metadata") {
		value = 0.1
		p.startOnce.Do(func() { close(p.oldStarted) })
		select {
		case <-p.releaseOld:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		p.doneOnce.Do(func() { close(p.oldDone) })
	}
	result := make([][]float32, len(texts))
	for i := range result {
		result[i] = make([]float32, store.RequiredMemoryEmbeddingDimensions)
		result[i][0] = value
	}
	return result, nil
}

func assertAgentEmbeddingEquals(t *testing.T, db queryRower, agentID uuid.UUID, expected string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var equal bool
		if err := db.QueryRow(`SELECT COALESCE(embedding = $1::vector, false) FROM agents WHERE id = $2`, expected, agentID).Scan(&equal); err != nil {
			t.Fatalf("query agent embedding: %v", err)
		}
		if equal {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("agent embedding did not match the newest source")
}

func vectorLiteral(first float64) string {
	values := make([]string, store.RequiredMemoryEmbeddingDimensions)
	values[0] = "0.2"
	if first == 0.1 {
		values[0] = "0.1"
	}
	for i := 1; i < len(values); i++ {
		values[i] = "0"
	}
	return "[" + strings.Join(values, ",") + "]"
}

var _ store.EmbeddingProvider = (*blockingAgentEmbedProvider)(nil)
