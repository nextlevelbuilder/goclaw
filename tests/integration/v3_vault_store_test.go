//go:build integration

package integration

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func newVaultStore(db *sql.DB) *pg.PGVaultStore {
	pg.InitSqlx(db)
	vs := pg.NewPGVaultStore(db)
	vs.SetEmbeddingProvider(newMockEmbedProvider())
	return vs
}

func makeVaultDoc(tenantID, agentID, path, title string) *store.VaultDocument {
	return &store.VaultDocument{
		TenantID:    tenantID,
		AgentID:     agentID,
		Scope:       "personal",
		Path:        path,
		Title:       title,
		DocType:     "note",
		ContentHash: "abc123",
	}
}

func TestStoreVault_UpsertAndGetDocument(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	vs := newVaultStore(db)

	tid := tenantID.String()
	aid := agentID.String()

	doc := makeVaultDoc(tid, aid, "notes/intro.md", "Introduction")
	if err := vs.UpsertDocument(ctx, doc); err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}
	if doc.ID == "" {
		t.Fatal("expected doc.ID to be set after upsert")
	}

	// GetDocument by path.
	got, err := vs.GetDocument(ctx, tid, aid, "notes/intro.md")
	if err != nil {
		t.Fatalf("GetDocument: %v", err)
	}
	if got.Title != "Introduction" {
		t.Errorf("expected Title='Introduction', got %q", got.Title)
	}

	// GetDocumentByID.
	byID, err := vs.GetDocumentByID(ctx, tid, doc.ID)
	if err != nil {
		t.Fatalf("GetDocumentByID: %v", err)
	}
	if byID.Path != "notes/intro.md" {
		t.Errorf("expected Path='notes/intro.md', got %q", byID.Path)
	}

	// Re-upsert (same path) — should update title.
	doc.Title = "Introduction Updated"
	if err := vs.UpsertDocument(ctx, doc); err != nil {
		t.Fatalf("UpsertDocument update: %v", err)
	}
	got2, err := vs.GetDocument(ctx, tid, aid, "notes/intro.md")
	if err != nil {
		t.Fatalf("GetDocument after update: %v", err)
	}
	if got2.Title != "Introduction Updated" {
		t.Errorf("expected Title='Introduction Updated', got %q", got2.Title)
	}
}

func TestStoreVault_DeleteDocument(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	vs := newVaultStore(db)

	tid := tenantID.String()
	aid := agentID.String()

	doc := makeVaultDoc(tid, aid, "del/target.md", "To Delete")
	if err := vs.UpsertDocument(ctx, doc); err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}

	if err := vs.DeleteDocument(ctx, tid, aid, "del/target.md"); err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}

	got, err := vs.GetDocument(ctx, tid, aid, "del/target.md")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows after delete, got err=%v doc=%v", err, got)
	}
}

func TestStoreVault_ListDocuments(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	vs := newVaultStore(db)

	tid := tenantID.String()
	aid := agentID.String()

	for i, path := range []string{"list/a.md", "list/b.md", "list/c.md"} {
		_ = i
		doc := makeVaultDoc(tid, aid, path, "Doc "+path)
		if err := vs.UpsertDocument(ctx, doc); err != nil {
			t.Fatalf("UpsertDocument %s: %v", path, err)
		}
	}

	docs, err := vs.ListDocuments(ctx, tid, aid, store.VaultListOptions{Limit: 20})
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	if len(docs) < 3 {
		t.Errorf("expected at least 3 docs, got %d", len(docs))
	}
}

func TestStoreVault_LinkManagement(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	vs := newVaultStore(db)

	tid := tenantID.String()
	aid := agentID.String()

	docA := makeVaultDoc(tid, aid, "links/a.md", "Doc A")
	docB := makeVaultDoc(tid, aid, "links/b.md", "Doc B")
	if err := vs.UpsertDocument(ctx, docA); err != nil {
		t.Fatalf("UpsertDocument A: %v", err)
	}
	if err := vs.UpsertDocument(ctx, docB); err != nil {
		t.Fatalf("UpsertDocument B: %v", err)
	}

	link := &store.VaultLink{
		FromDocID: docA.ID,
		ToDocID:   docB.ID,
		LinkType:  "wikilink",
		Context:   "mentioned in A",
	}
	if err := vs.CreateLink(ctx, link); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	// GetOutLinks from A — expect 1.
	outLinks, err := vs.GetOutLinks(ctx, tid, docA.ID)
	if err != nil {
		t.Fatalf("GetOutLinks: %v", err)
	}
	if len(outLinks) != 1 {
		t.Errorf("expected 1 outlink from A, got %d", len(outLinks))
	}
	if outLinks[0].ToDocID != docB.ID {
		t.Errorf("outlink ToDocID: expected %s, got %s", docB.ID, outLinks[0].ToDocID)
	}

	// GetBacklinks for B — expect 1.
	backlinks, err := vs.GetBacklinks(ctx, tid, docB.ID)
	if err != nil {
		t.Fatalf("GetBacklinks: %v", err)
	}
	if len(backlinks) != 1 {
		t.Errorf("expected 1 backlink to B, got %d", len(backlinks))
	}

	// DeleteDocLinks for A — clears all outgoing links from A.
	if err := vs.DeleteDocLinks(ctx, tid, docA.ID); err != nil {
		t.Fatalf("DeleteDocLinks: %v", err)
	}
	outLinks2, err := vs.GetOutLinks(ctx, tid, docA.ID)
	if err != nil {
		t.Fatalf("GetOutLinks after delete: %v", err)
	}
	if len(outLinks2) != 0 {
		t.Errorf("expected 0 outlinks after DeleteDocLinks, got %d", len(outLinks2))
	}
}

func TestStoreVault_TenantIsolation(t *testing.T) {
	db := testDB(t)
	// seedTwoTenants returns (tenantA, tenantB, agentA, agentB).
	tenantA, tenantB, agentA, agentB := seedTwoTenants(t, db)
	ctx := tenantCtx(tenantA) // context for tenant A writes

	vs := newVaultStore(db)

	tidA := tenantA.String()
	tidB := tenantB.String()
	aidA := agentA.String()
	aidB := agentB.String()

	doc := makeVaultDoc(tidA, aidA, "iso/secret.md", "Tenant A Secret")
	if err := vs.UpsertDocument(ctx, doc); err != nil {
		t.Fatalf("UpsertDocument tenantA: %v", err)
	}

	// Tenant B querying its own agent should not find tenant A's document.
	got, err := vs.GetDocument(ctx, tidB, aidB, "iso/secret.md")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("tenant B should not access tenant A doc: err=%v doc=%v", err, got)
	}
}
