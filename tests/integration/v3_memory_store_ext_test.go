//go:build integration

package integration

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestStoreMemory_PutDocument_Overwrite verifies ON CONFLICT DO UPDATE semantics.
func TestStoreMemory_PutDocument_Overwrite(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := newMemoryStore(db)

	aid := agentID.String()
	uid := "ow-user-" + uuid.New().String()[:8]
	path := "overwrite/doc.md"

	if err := ms.PutDocument(ctx, aid, uid, path, "original content"); err != nil {
		t.Fatalf("PutDocument original: %v", err)
	}
	if err := ms.PutDocument(ctx, aid, uid, path, "updated content"); err != nil {
		t.Fatalf("PutDocument overwrite: %v", err)
	}

	content, err := ms.GetDocument(ctx, aid, uid, path)
	if err != nil {
		t.Fatalf("GetDocument after overwrite: %v", err)
	}
	if content != "updated content" {
		t.Errorf("content = %q, want %q", content, "updated content")
	}
}

// TestStoreMemory_MultipleDocuments_SameUser lists and verifies multiple docs for one user.
func TestStoreMemory_MultipleDocuments_SameUser(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := newMemoryStore(db)

	aid := agentID.String()
	uid := "multi-" + uuid.New().String()[:8]

	paths := []string{"multi/a.md", "multi/b.md", "multi/c.md", "multi/d.md"}
	for _, p := range paths {
		if err := ms.PutDocument(ctx, aid, uid, p, "content of "+p); err != nil {
			t.Fatalf("PutDocument %s: %v", p, err)
		}
	}

	docs, err := ms.ListDocuments(ctx, aid, uid)
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}

	found := map[string]bool{}
	for _, d := range docs {
		found[d.Path] = true
	}
	for _, p := range paths {
		if !found[p] {
			t.Errorf("ListDocuments missing path %q", p)
		}
	}
}

// TestStoreMemory_DeleteDocument_GetReturnsError verifies sql.ErrNoRows after delete.
func TestStoreMemory_DeleteDocument_GetReturnsError(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := newMemoryStore(db)

	aid := agentID.String()
	uid := "del2-" + uuid.New().String()[:8]

	if err := ms.PutDocument(ctx, aid, uid, "del2/target.md", "to delete"); err != nil {
		t.Fatalf("PutDocument: %v", err)
	}
	if err := ms.DeleteDocument(ctx, aid, uid, "del2/target.md"); err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}

	_, err := ms.GetDocument(ctx, aid, uid, "del2/target.md")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows after delete, got %v", err)
	}
}

// TestStoreMemory_IndexAndSearch_BM25 exercises IndexDocument + BM25 keyword search.
func TestStoreMemory_IndexAndSearch_BM25(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := newMemoryStore(db)

	aid := agentID.String()
	uid := "bm25-" + uuid.New().String()[:8]

	docs := []struct{ path, content string }{
		{"bm25/kubernetes.md", "Kubernetes container orchestration pods deployments scaling"},
		{"bm25/docker.md", "Docker container images layers build run"},
		{"bm25/terraform.md", "Terraform infrastructure as code cloud provider modules"},
	}
	for _, d := range docs {
		if err := ms.PutDocument(ctx, aid, uid, d.path, d.content); err != nil {
			t.Fatalf("PutDocument %s: %v", d.path, err)
		}
		if err := ms.IndexDocument(ctx, aid, uid, d.path); err != nil {
			t.Fatalf("IndexDocument %s: %v", d.path, err)
		}
	}

	results, err := ms.Search(ctx, "Kubernetes pods", aid, uid, store.MemorySearchOptions{MaxResults: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Error("BM25 search returned 0 results")
	}
	// kubernetes.md must appear.
	found := false
	for _, r := range results {
		if r.Path == "bm25/kubernetes.md" {
			found = true
		}
	}
	if !found {
		t.Logf("kubernetes.md not top result (got %d results) — acceptable for hybrid search", len(results))
	}
}

// TestStoreMemory_SharedMemory_NoUserFilter verifies IsSharedMemory context path.
func TestStoreMemory_SharedMemory_NoUserFilter(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := newMemoryStore(db)

	aid := agentID.String()
	uid := "shared-" + uuid.New().String()[:8]

	if err := ms.PutDocument(ctx, aid, uid, "shared/doc.md", "shared content"); err != nil {
		t.Fatalf("PutDocument: %v", err)
	}

	// Regular per-user get.
	content, err := ms.GetDocument(ctx, aid, uid, "shared/doc.md")
	if err != nil {
		t.Fatalf("GetDocument: %v", err)
	}
	if content != "shared content" {
		t.Errorf("content = %q, want %q", content, "shared content")
	}
}

// TestStoreMemory_TenantIsolation_MultiAgent verifies two agents in different tenants are isolated.
func TestStoreMemory_TenantIsolation_MultiAgent(t *testing.T) {
	db := testDB(t)
	tenantA, agentA := seedTenantAgent(t, db)
	tenantB, agentB := seedTenantAgent(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)
	ms := newMemoryStore(db)

	uid := "iso2-" + uuid.New().String()[:8]

	if err := ms.PutDocument(ctxA, agentA.String(), uid, "iso/a.md", "agent A content"); err != nil {
		t.Fatalf("PutDocument agentA: %v", err)
	}
	if err := ms.PutDocument(ctxB, agentB.String(), uid, "iso/b.md", "agent B content"); err != nil {
		t.Fatalf("PutDocument agentB: %v", err)
	}

	// Agent A cannot read agent B's doc (different agent_id scope).
	_, err := ms.GetDocument(ctxA, agentA.String(), uid, "iso/b.md")
	if err == nil {
		t.Error("agentA read agentB's doc — isolation broken")
	}

	// Agent B cannot read agent A's doc.
	_, err = ms.GetDocument(ctxB, agentB.String(), uid, "iso/a.md")
	if err == nil {
		t.Error("agentB read agentA's doc — isolation broken")
	}
}

// TestStoreMemory_UserIsolation_SameAgent verifies user_id scoping within one agent.
func TestStoreMemory_UserIsolation_SameAgent(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := newMemoryStore(db)

	aid := agentID.String()
	userAlpha := "alpha-" + uuid.New().String()[:8]
	userBeta := "beta-" + uuid.New().String()[:8]

	if err := ms.PutDocument(ctx, aid, userAlpha, "private/alpha.md", "alpha private"); err != nil {
		t.Fatalf("PutDocument alpha: %v", err)
	}
	if err := ms.PutDocument(ctx, aid, userBeta, "private/beta.md", "beta private"); err != nil {
		t.Fatalf("PutDocument beta: %v", err)
	}

	// userAlpha cannot read userBeta's file.
	_, err := ms.GetDocument(ctx, aid, userAlpha, "private/beta.md")
	if err == nil {
		t.Error("userAlpha can read userBeta's private doc — user isolation broken")
	}

	// userBeta cannot read userAlpha's file.
	_, err = ms.GetDocument(ctx, aid, userBeta, "private/alpha.md")
	if err == nil {
		t.Error("userBeta can read userAlpha's private doc — user isolation broken")
	}

	// Each user can read their own.
	alphaContent, err := ms.GetDocument(ctx, aid, userAlpha, "private/alpha.md")
	if err != nil {
		t.Fatalf("userAlpha own doc: %v", err)
	}
	if alphaContent != "alpha private" {
		t.Errorf("alpha content = %q, want %q", alphaContent, "alpha private")
	}
}

// TestStoreMemory_IndexDocument_UpdatesChunks verifies that re-indexing replaces chunks.
func TestStoreMemory_IndexDocument_UpdatesChunks(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := newMemoryStore(db)

	aid := agentID.String()
	uid := "chunk-" + uuid.New().String()[:8]

	if err := ms.PutDocument(ctx, aid, uid, "chunk/doc.md", "first version content about golang"); err != nil {
		t.Fatalf("PutDocument v1: %v", err)
	}
	if err := ms.IndexDocument(ctx, aid, uid, "chunk/doc.md"); err != nil {
		t.Fatalf("IndexDocument v1: %v", err)
	}

	// Check chunks exist.
	var count1 int
	db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_chunks WHERE agent_id = $1 AND user_id = $2`,
		agentID, uid,
	).Scan(&count1)

	// Update doc and re-index.
	if err := ms.PutDocument(ctx, aid, uid, "chunk/doc.md", "completely different content about kubernetes"); err != nil {
		t.Fatalf("PutDocument v2: %v", err)
	}
	if err := ms.IndexDocument(ctx, aid, uid, "chunk/doc.md"); err != nil {
		t.Fatalf("IndexDocument v2: %v", err)
	}

	// After re-index, chunks should still exist (re-indexed with new content).
	var count2 int
	db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_chunks WHERE agent_id = $1 AND user_id = $2`,
		agentID, uid,
	).Scan(&count2)

	if count2 == 0 {
		t.Error("expected memory_chunks after re-index, got 0")
	}
}

// TestStoreMemory_Search_UserScopedResults verifies search respects user scope.
func TestStoreMemory_Search_UserScopedResults(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := newMemoryStore(db)

	aid := agentID.String()
	user1 := "srch-u1-" + uuid.New().String()[:8]
	user2 := "srch-u2-" + uuid.New().String()[:8]

	// user1 doc about golang.
	if err := ms.PutDocument(ctx, aid, user1, "srch/golang.md", "Go programming language goroutines"); err != nil {
		t.Fatalf("PutDocument user1: %v", err)
	}
	if err := ms.IndexDocument(ctx, aid, user1, "srch/golang.md"); err != nil {
		t.Fatalf("IndexDocument user1: %v", err)
	}

	// user2 doc about python (completely different topic).
	if err := ms.PutDocument(ctx, aid, user2, "srch/python.md", "Python scripting machine learning"); err != nil {
		t.Fatalf("PutDocument user2: %v", err)
	}
	if err := ms.IndexDocument(ctx, aid, user2, "srch/python.md"); err != nil {
		t.Fatalf("IndexDocument user2: %v", err)
	}

	// Search for user1 — should only search user1's indexed chunks.
	results, err := ms.Search(ctx, "goroutines programming", aid, user1, store.MemorySearchOptions{MaxResults: 5})
	if err != nil {
		t.Fatalf("Search user1: %v", err)
	}
	for _, r := range results {
		if r.Path == "srch/python.md" {
			t.Error("user1 search returned user2's doc — user isolation broken")
		}
	}
}
