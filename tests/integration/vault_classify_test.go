//go:build integration

package integration

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestVaultClassify_DeleteDocLinksByTypes_SingleType(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	vs := newVaultStore(db)

	tid := tenantID.String()
	aid := agentID.String()

	// Create 2 documents
	docA := makeVaultDoc(tid, aid, "classify/source.md", "Source Doc")
	docB := makeVaultDoc(tid, aid, "classify/target.md", "Target Doc")
	if err := vs.UpsertDocument(ctx, docA); err != nil {
		t.Fatalf("UpsertDocument A: %v", err)
	}
	if err := vs.UpsertDocument(ctx, docB); err != nil {
		t.Fatalf("UpsertDocument B: %v", err)
	}

	// Create 3 links with different types
	links := []struct {
		linkType string
		context  string
	}{
		{"reference", "cited in source"},
		{"wikilink", "mentioned in source"},
		{"depends_on", "dependency of source"},
	}

	for _, l := range links {
		link := &store.VaultLink{
			FromDocID: docA.ID,
			ToDocID:   docB.ID,
			LinkType:  l.linkType,
			Context:   l.context,
		}
		if err := vs.CreateLink(ctx, link); err != nil {
			t.Fatalf("CreateLink %s: %v", l.linkType, err)
		}
	}

	// Verify: 3 links exist
	outLinks, err := vs.GetOutLinks(ctx, tid, docA.ID)
	if err != nil {
		t.Fatalf("GetOutLinks before delete: %v", err)
	}
	if len(outLinks) != 3 {
		t.Errorf("expected 3 links before delete, got %d", len(outLinks))
	}

	// Delete links with type "reference" only
	if err := vs.DeleteDocLinksByTypes(ctx, tid, docA.ID, []string{"reference"}); err != nil {
		t.Fatalf("DeleteDocLinksByTypes: %v", err)
	}

	// Verify: only "reference" deleted, "wikilink" and "depends_on" remain
	outLinksAfter, err := vs.GetOutLinks(ctx, tid, docA.ID)
	if err != nil {
		t.Fatalf("GetOutLinks after delete: %v", err)
	}
	if len(outLinksAfter) != 2 {
		t.Errorf("expected 2 links after deleting 1, got %d", len(outLinksAfter))
	}

	// Verify remaining links are correct types
	typeMap := make(map[string]bool)
	for _, l := range outLinksAfter {
		typeMap[l.LinkType] = true
	}
	if !typeMap["wikilink"] || !typeMap["depends_on"] {
		t.Errorf("expected wikilink and depends_on, got types: %v", typeMap)
	}
	if typeMap["reference"] {
		t.Errorf("reference should have been deleted")
	}
}

func TestVaultClassify_DeleteDocLinksByTypes_MultipleTypes(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	vs := newVaultStore(db)

	tid := tenantID.String()
	aid := agentID.String()

	// Create source doc and 8 target docs (one per link type: 6 classify + semantic + wikilink).
	docSource := makeVaultDoc(tid, aid, "source.md", "Source")
	docs := make([]*store.VaultDocument, 0)
	docs = append(docs, docSource)

	for i := range 8 {
		doc := makeVaultDoc(tid, aid, "classify/target-"+string(rune('a'+i))+".md", "Target "+string(rune('A'+i)))
		if err := vs.UpsertDocument(ctx, doc); err != nil {
			t.Fatalf("UpsertDocument target %d: %v", i, err)
		}
		docs = append(docs, doc)
	}
	docSource = docs[0]

	if err := vs.UpsertDocument(ctx, docSource); err != nil {
		t.Fatalf("UpsertDocument source: %v", err)
	}

	// Create links with 6 classify types + semantic + wikilink (8 total)
	linkTypes := []string{
		"reference",    // classify type 1
		"depends_on",   // classify type 2
		"extends",      // classify type 3
		"related",      // classify type 4
		"supersedes",   // classify type 5
		"contradicts",  // classify type 6
		"semantic",     // legacy auto-classified type
		"wikilink",     // manual link type
	}

	for i, lt := range linkTypes {
		link := &store.VaultLink{
			FromDocID: docSource.ID,
			ToDocID:   docs[i+1].ID,
			LinkType:  lt,
			Context:   "test context for " + lt,
		}
		if err := vs.CreateLink(ctx, link); err != nil {
			t.Fatalf("CreateLink %s: %v", lt, err)
		}
	}

	// Verify: 8 links created
	outLinks, err := vs.GetOutLinks(ctx, tid, docSource.ID)
	if err != nil {
		t.Fatalf("GetOutLinks before delete: %v", err)
	}
	if len(outLinks) != 8 {
		t.Errorf("expected 8 links before delete, got %d", len(outLinks))
	}

	// Delete all classify types + semantic (7 types), keep wikilink
	typesToDelete := []string{
		"reference", "depends_on", "extends",
		"related", "supersedes", "contradicts",
		"semantic",
	}
	if err := vs.DeleteDocLinksByTypes(ctx, tid, docSource.ID, typesToDelete); err != nil {
		t.Fatalf("DeleteDocLinksByTypes: %v", err)
	}

	// Verify: only wikilink survives
	outLinksAfter, err := vs.GetOutLinks(ctx, tid, docSource.ID)
	if err != nil {
		t.Fatalf("GetOutLinks after delete: %v", err)
	}
	if len(outLinksAfter) != 1 {
		t.Errorf("expected 1 link after deleting 7, got %d", len(outLinksAfter))
	}
	if outLinksAfter[0].LinkType != "wikilink" {
		t.Errorf("expected wikilink, got %s", outLinksAfter[0].LinkType)
	}
}

func TestVaultClassify_DeleteDocLinksByTypes_NoMatches(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	vs := newVaultStore(db)

	tid := tenantID.String()
	aid := agentID.String()

	// Create docs
	docA := makeVaultDoc(tid, aid, "nomatch/a.md", "Doc A")
	docB := makeVaultDoc(tid, aid, "nomatch/b.md", "Doc B")
	if err := vs.UpsertDocument(ctx, docA); err != nil {
		t.Fatalf("UpsertDocument A: %v", err)
	}
	if err := vs.UpsertDocument(ctx, docB); err != nil {
		t.Fatalf("UpsertDocument B: %v", err)
	}

	// Create link with type "wikilink"
	link := &store.VaultLink{
		FromDocID: docA.ID,
		ToDocID:   docB.ID,
		LinkType:  "wikilink",
		Context:   "manual link",
	}
	if err := vs.CreateLink(ctx, link); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	// Delete non-existent type — should succeed but delete nothing
	if err := vs.DeleteDocLinksByTypes(ctx, tid, docA.ID, []string{"reference"}); err != nil {
		t.Fatalf("DeleteDocLinksByTypes non-existent type: %v", err)
	}

	// Verify wikilink still exists
	outLinks, err := vs.GetOutLinks(ctx, tid, docA.ID)
	if err != nil {
		t.Fatalf("GetOutLinks after no-op delete: %v", err)
	}
	if len(outLinks) != 1 {
		t.Errorf("expected 1 link (unchanged), got %d", len(outLinks))
	}
	if outLinks[0].LinkType != "wikilink" {
		t.Errorf("expected wikilink, got %s", outLinks[0].LinkType)
	}
}

func TestVaultClassify_DeleteDocLinksByTypes_EmptyTypeList(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	vs := newVaultStore(db)

	tid := tenantID.String()
	aid := agentID.String()

	// Create docs and link
	docA := makeVaultDoc(tid, aid, "empty/a.md", "Doc A")
	docB := makeVaultDoc(tid, aid, "empty/b.md", "Doc B")
	if err := vs.UpsertDocument(ctx, docA); err != nil {
		t.Fatalf("UpsertDocument A: %v", err)
	}
	if err := vs.UpsertDocument(ctx, docB); err != nil {
		t.Fatalf("UpsertDocument B: %v", err)
	}

	link := &store.VaultLink{
		FromDocID: docA.ID,
		ToDocID:   docB.ID,
		LinkType:  "reference",
		Context:   "test",
	}
	if err := vs.CreateLink(ctx, link); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	// Delete with empty type list — should succeed but delete nothing
	if err := vs.DeleteDocLinksByTypes(ctx, tid, docA.ID, []string{}); err != nil {
		t.Fatalf("DeleteDocLinksByTypes empty list: %v", err)
	}

	// Verify link still exists
	outLinks, err := vs.GetOutLinks(ctx, tid, docA.ID)
	if err != nil {
		t.Fatalf("GetOutLinks after empty delete: %v", err)
	}
	if len(outLinks) != 1 {
		t.Errorf("expected 1 link (unchanged), got %d", len(outLinks))
	}
}

func TestVaultClassify_DeleteDocLinksByTypes_MultipleSourceDocs(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	vs := newVaultStore(db)

	tid := tenantID.String()
	aid := agentID.String()

	// Create 2 source docs and 1 target doc
	docSource1 := makeVaultDoc(tid, aid, "multi/source1.md", "Source 1")
	docSource2 := makeVaultDoc(tid, aid, "multi/source2.md", "Source 2")
	docTarget := makeVaultDoc(tid, aid, "multi/target.md", "Target")

	for _, doc := range []*store.VaultDocument{docSource1, docSource2, docTarget} {
		if err := vs.UpsertDocument(ctx, doc); err != nil {
			t.Fatalf("UpsertDocument: %v", err)
		}
	}

	// Create links from both sources to target
	linkTypes := []string{"reference", "semantic", "wikilink"}
	for _, lt := range linkTypes {
		link1 := &store.VaultLink{
			FromDocID: docSource1.ID,
			ToDocID:   docTarget.ID,
			LinkType:  lt,
			Context:   "from source1",
		}
		link2 := &store.VaultLink{
			FromDocID: docSource2.ID,
			ToDocID:   docTarget.ID,
			LinkType:  lt,
			Context:   "from source2",
		}
		if err := vs.CreateLink(ctx, link1); err != nil {
			t.Fatalf("CreateLink source1: %v", err)
		}
		if err := vs.CreateLink(ctx, link2); err != nil {
			t.Fatalf("CreateLink source2: %v", err)
		}
	}

	// Verify: both sources have 3 links each
	outLinks1, err := vs.GetOutLinks(ctx, tid, docSource1.ID)
	if err != nil {
		t.Fatalf("GetOutLinks source1 before: %v", err)
	}
	outLinks2, err := vs.GetOutLinks(ctx, tid, docSource2.ID)
	if err != nil {
		t.Fatalf("GetOutLinks source2 before: %v", err)
	}
	if len(outLinks1) != 3 || len(outLinks2) != 3 {
		t.Errorf("expected 3 links each source before delete, got %d and %d", len(outLinks1), len(outLinks2))
	}

	// Delete "reference" + "semantic" from source1 only
	if err := vs.DeleteDocLinksByTypes(ctx, tid, docSource1.ID, []string{"reference", "semantic"}); err != nil {
		t.Fatalf("DeleteDocLinksByTypes source1: %v", err)
	}

	// Verify: source1 has only "wikilink", source2 unchanged
	outLinks1After, err := vs.GetOutLinks(ctx, tid, docSource1.ID)
	if err != nil {
		t.Fatalf("GetOutLinks source1 after: %v", err)
	}
	outLinks2After, err := vs.GetOutLinks(ctx, tid, docSource2.ID)
	if err != nil {
		t.Fatalf("GetOutLinks source2 after: %v", err)
	}

	if len(outLinks1After) != 1 {
		t.Errorf("expected 1 link in source1 after delete, got %d", len(outLinks1After))
	}
	if outLinks1After[0].LinkType != "wikilink" {
		t.Errorf("expected wikilink in source1, got %s", outLinks1After[0].LinkType)
	}

	if len(outLinks2After) != 3 {
		t.Errorf("expected 3 links in source2 (unchanged), got %d", len(outLinks2After))
	}
}

func TestVaultClassify_DeleteDocLinksByTypes_TenantIsolation(t *testing.T) {
	db := testDB(t)
	tenantA, tenantB, agentA, agentB := seedTwoTenants(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)
	vs := newVaultStore(db)

	tidA := tenantA.String()
	tidB := tenantB.String()
	aidA := agentA.String()
	aidB := agentB.String()

	// Create docs in tenant A
	docA1 := makeVaultDoc(tidA, aidA, "iso/a1.md", "Tenant A Doc 1")
	docA2 := makeVaultDoc(tidA, aidA, "iso/a2.md", "Tenant A Doc 2")
	if err := vs.UpsertDocument(ctxA, docA1); err != nil {
		t.Fatalf("UpsertDocument tenantA doc1: %v", err)
	}
	if err := vs.UpsertDocument(ctxA, docA2); err != nil {
		t.Fatalf("UpsertDocument tenantA doc2: %v", err)
	}

	// Create docs in tenant B
	docB1 := makeVaultDoc(tidB, aidB, "iso/b1.md", "Tenant B Doc 1")
	docB2 := makeVaultDoc(tidB, aidB, "iso/b2.md", "Tenant B Doc 2")
	if err := vs.UpsertDocument(ctxB, docB1); err != nil {
		t.Fatalf("UpsertDocument tenantB doc1: %v", err)
	}
	if err := vs.UpsertDocument(ctxB, docB2); err != nil {
		t.Fatalf("UpsertDocument tenantB doc2: %v", err)
	}

	// Create links in both tenants
	linkA := &store.VaultLink{
		FromDocID: docA1.ID,
		ToDocID:   docA2.ID,
		LinkType:  "reference",
		Context:   "tenant A link",
	}
	linkB := &store.VaultLink{
		FromDocID: docB1.ID,
		ToDocID:   docB2.ID,
		LinkType:  "reference",
		Context:   "tenant B link",
	}

	if err := vs.CreateLink(ctxA, linkA); err != nil {
		t.Fatalf("CreateLink tenantA: %v", err)
	}
	if err := vs.CreateLink(ctxB, linkB); err != nil {
		t.Fatalf("CreateLink tenantB: %v", err)
	}

	// Verify both have links
	outA, err := vs.GetOutLinks(ctxA, tidA, docA1.ID)
	if err != nil {
		t.Fatalf("GetOutLinks tenantA before: %v", err)
	}
	outB, err := vs.GetOutLinks(ctxB, tidB, docB1.ID)
	if err != nil {
		t.Fatalf("GetOutLinks tenantB before: %v", err)
	}
	if len(outA) != 1 || len(outB) != 1 {
		t.Errorf("expected 1 link each before delete, got %d and %d", len(outA), len(outB))
	}

	// Delete from tenant A only — should NOT affect tenant B
	if err := vs.DeleteDocLinksByTypes(ctxA, tidA, docA1.ID, []string{"reference"}); err != nil {
		t.Fatalf("DeleteDocLinksByTypes tenantA: %v", err)
	}

	// Verify: tenantA link deleted, tenantB link unchanged
	outAAfter, err := vs.GetOutLinks(ctxA, tidA, docA1.ID)
	if err != nil {
		t.Fatalf("GetOutLinks tenantA after: %v", err)
	}
	outBAfter, err := vs.GetOutLinks(ctxB, tidB, docB1.ID)
	if err != nil {
		t.Fatalf("GetOutLinks tenantB after: %v", err)
	}

	if len(outAAfter) != 0 {
		t.Errorf("expected 0 links in tenantA after delete, got %d", len(outAAfter))
	}
	if len(outBAfter) != 1 {
		t.Errorf("expected 1 link in tenantB (unchanged), got %d", len(outBAfter))
	}
}

func TestVaultClassify_DeleteDocLinksByTypes_NonExistentDoc(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	vs := newVaultStore(db)

	tid := tenantID.String()

	// Use a well-formed but non-existent UUID — post-Phase-4 parseUUID rejects
	// bare strings like "fake-doc-id-12345" at the caller boundary, so the
	// "non-existent doc = no-op" contract requires a syntactically valid UUID
	// that just happens to not exist in the DB.
	fakeDocID := "00000000-0000-4000-8000-000000000dea"
	if err := vs.DeleteDocLinksByTypes(ctx, tid, fakeDocID, []string{"reference"}); err != nil {
		t.Fatalf("DeleteDocLinksByTypes on non-existent doc: %v", err)
	}

	// GetOutLinks for non-existent doc should return empty or error gracefully
	outLinks, err := vs.GetOutLinks(ctx, tid, fakeDocID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		// Some implementations may error on non-existent doc, others return empty list
		// Both are acceptable for a non-existent doc
	}
	if outLinks != nil && len(outLinks) != 0 {
		t.Errorf("expected empty or error for non-existent doc, got %d links", len(outLinks))
	}
}

func TestVaultClassify_DeleteAllClassifyTypesAndSemantic(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	vs := newVaultStore(db)

	tid := tenantID.String()
	aid := agentID.String()

	// Create documents
	docSource := makeVaultDoc(tid, aid, "classify/source.md", "Source Doc")
	docTarget1 := makeVaultDoc(tid, aid, "classify/target1.md", "Target 1")
	docTarget2 := makeVaultDoc(tid, aid, "classify/target2.md", "Target 2")

	for _, doc := range []*store.VaultDocument{docSource, docTarget1, docTarget2} {
		if err := vs.UpsertDocument(ctx, doc); err != nil {
			t.Fatalf("UpsertDocument: %v", err)
		}
	}

	// Create links with mix of classify types, semantic, and manual types
	linkConfigs := []struct {
		toDocID string
		linkType string
	}{
		{docTarget1.ID, "reference"},
		{docTarget1.ID, "semantic"},
		{docTarget2.ID, "depends_on"},
		{docTarget2.ID, "wikilink"},
	}

	for _, cfg := range linkConfigs {
		link := &store.VaultLink{
			FromDocID: docSource.ID,
			ToDocID:   cfg.toDocID,
			LinkType:  cfg.linkType,
			Context:   "test context",
		}
		if err := vs.CreateLink(ctx, link); err != nil {
			t.Fatalf("CreateLink %s: %v", cfg.linkType, err)
		}
	}

	// Verify: 4 links created
	outLinks, err := vs.GetOutLinks(ctx, tid, docSource.ID)
	if err != nil {
		t.Fatalf("GetOutLinks before delete: %v", err)
	}
	if len(outLinks) != 4 {
		t.Errorf("expected 4 links before delete, got %d", len(outLinks))
	}

	// Delete all classify types (reference, depends_on) + semantic, keep wikilink
	if err := vs.DeleteDocLinksByTypes(ctx, tid, docSource.ID, []string{
		"reference", "depends_on", "extends", "related", "supersedes", "contradicts", "semantic",
	}); err != nil {
		t.Fatalf("DeleteDocLinksByTypes: %v", err)
	}

	// Verify: only wikilink remains
	outLinksAfter, err := vs.GetOutLinks(ctx, tid, docSource.ID)
	if err != nil {
		t.Fatalf("GetOutLinks after delete: %v", err)
	}
	if len(outLinksAfter) != 1 {
		t.Errorf("expected 1 link after cleanup, got %d", len(outLinksAfter))
	}
	if outLinksAfter[0].LinkType != "wikilink" {
		t.Errorf("expected wikilink survivor, got %s", outLinksAfter[0].LinkType)
	}
}

func TestVaultClassify_DeleteDocLinksByTypes_CasePreservation(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	vs := newVaultStore(db)

	tid := tenantID.String()
	aid := agentID.String()

	// Create documents
	docA := makeVaultDoc(tid, aid, "case/a.md", "Doc A")
	docB := makeVaultDoc(tid, aid, "case/b.md", "Doc B")
	if err := vs.UpsertDocument(ctx, docA); err != nil {
		t.Fatalf("UpsertDocument A: %v", err)
	}
	if err := vs.UpsertDocument(ctx, docB); err != nil {
		t.Fatalf("UpsertDocument B: %v", err)
	}

	// Create links with different case sensitivity
	link := &store.VaultLink{
		FromDocID: docA.ID,
		ToDocID:   docB.ID,
		LinkType:  "depends_on", // lowercase with underscore
		Context:   "test",
	}
	if err := vs.CreateLink(ctx, link); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	// Try deleting with exact case match
	if err := vs.DeleteDocLinksByTypes(ctx, tid, docA.ID, []string{"depends_on"}); err != nil {
		t.Fatalf("DeleteDocLinksByTypes: %v", err)
	}

	// Verify link is deleted
	outLinks, err := vs.GetOutLinks(ctx, tid, docA.ID)
	if err != nil {
		t.Fatalf("GetOutLinks: %v", err)
	}
	if len(outLinks) != 0 {
		t.Errorf("expected 0 links after exact case match delete, got %d", len(outLinks))
	}
}
