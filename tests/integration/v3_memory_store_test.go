//go:build integration

package integration

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func newMemoryStore(db *sql.DB) *pg.PGMemoryStore {
	ms := pg.NewPGMemoryStore(db, pg.DefaultPGMemoryConfig())
	ms.SetEmbeddingProvider(newMockEmbedProvider())
	return ms
}

func TestStoreMemory_PutAndGetDocument(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := newMemoryStore(db)

	aid := agentID.String()
	uid := "user-mem-" + agentID.String()[:8]

	if err := ms.PutDocument(ctx, aid, uid, "notes/hello.md", "Hello World"); err != nil {
		t.Fatalf("PutDocument: %v", err)
	}

	content, err := ms.GetDocument(ctx, aid, uid, "notes/hello.md")
	if err != nil {
		t.Fatalf("GetDocument: %v", err)
	}
	if content != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", content)
	}

	// Overwrite same path — ON CONFLICT DO UPDATE.
	if err := ms.PutDocument(ctx, aid, uid, "notes/hello.md", "Updated Content"); err != nil {
		t.Fatalf("PutDocument overwrite: %v", err)
	}
	content2, err := ms.GetDocument(ctx, aid, uid, "notes/hello.md")
	if err != nil {
		t.Fatalf("GetDocument after overwrite: %v", err)
	}
	if content2 != "Updated Content" {
		t.Errorf("expected 'Updated Content', got %q", content2)
	}
}

func TestStoreMemory_DeleteDocument(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := newMemoryStore(db)

	aid := agentID.String()
	uid := "user-del-" + agentID.String()[:8]

	if err := ms.PutDocument(ctx, aid, uid, "del/doc.md", "to delete"); err != nil {
		t.Fatalf("PutDocument: %v", err)
	}
	if err := ms.DeleteDocument(ctx, aid, uid, "del/doc.md"); err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}

	_, err := ms.GetDocument(ctx, aid, uid, "del/doc.md")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows after delete, got %v", err)
	}
}

func TestStoreMemory_ListDocuments(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := newMemoryStore(db)

	aid := agentID.String()
	uid := "user-list-" + agentID.String()[:8]

	for _, path := range []string{"a.md", "b.md", "c.md"} {
		if err := ms.PutDocument(ctx, aid, uid, path, "content "+path); err != nil {
			t.Fatalf("PutDocument %s: %v", path, err)
		}
	}

	// ListDocuments with a non-empty userID returns global + per-user docs.
	docs, err := ms.ListDocuments(ctx, aid, uid)
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	if len(docs) < 3 {
		t.Errorf("expected at least 3 docs, got %d", len(docs))
	}
}

func TestStoreMemory_Search(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := newMemoryStore(db)

	aid := agentID.String()
	uid := "user-srch-" + agentID.String()[:8]

	docs := []struct{ path, content string }{
		{"golang.md", "Go programming language concurrency goroutines channels"},
		{"python.md", "Python scripting data science machine learning"},
		{"rust.md", "Rust systems programming memory safety ownership"},
	}
	for _, d := range docs {
		if err := ms.PutDocument(ctx, aid, uid, d.path, d.content); err != nil {
			t.Fatalf("PutDocument %s: %v", d.path, err)
		}
		// Index document to populate memory_chunks for search.
		if err := ms.IndexDocument(ctx, aid, uid, d.path); err != nil {
			t.Fatalf("IndexDocument %s: %v", d.path, err)
		}
	}

	results, err := ms.Search(ctx, "Go programming", aid, uid, store.MemorySearchOptions{
		MaxResults: 5,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected search results, got none")
	}
}

func TestStoreMemory_UserScopeIsolation(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := newMemoryStore(db)

	aid := agentID.String()
	user1 := "user1-" + agentID.String()[:8]
	user2 := "user2-" + agentID.String()[:8]

	if err := ms.PutDocument(ctx, aid, user1, "private/u1.md", "user1 content"); err != nil {
		t.Fatalf("PutDocument user1: %v", err)
	}
	if err := ms.PutDocument(ctx, aid, user2, "private/u2.md", "user2 content"); err != nil {
		t.Fatalf("PutDocument user2: %v", err)
	}

	// user1 can retrieve their own doc.
	_, err := ms.GetDocument(ctx, aid, user1, "private/u1.md")
	if err != nil {
		t.Fatalf("user1 cannot get own doc: %v", err)
	}

	// user1 must not access user2's doc (user_id filter).
	_, err = ms.GetDocument(ctx, aid, user1, "private/u2.md")
	if err == nil {
		t.Error("user1 should not be able to get user2's doc")
	}
}

func TestStoreMemory_TenantIsolation(t *testing.T) {
	db := testDB(t)
	// seedTwoTenants returns (tenantA, tenantB, agentA, agentB).
	tenantA, tenantB, agentA, agentB := seedTwoTenants(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)
	ms := newMemoryStore(db)

	aidA := agentA.String()
	aidB := agentB.String()
	uid := "iso-user-" + agentA.String()[:8]

	if err := ms.PutDocument(ctxA, aidA, uid, "iso/secret.md", "tenant A secret"); err != nil {
		t.Fatalf("PutDocument tenantA: %v", err)
	}

	// Tenant B querying its own agent should not find tenant A's document.
	_, err := ms.GetDocument(ctxB, aidB, uid, "iso/secret.md")
	if err == nil {
		t.Error("tenant B should not access tenant A's document")
	}
}

func TestStoreMemory_ConcurrentPutDocument(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := newMemoryStore(db)

	aid := agentID.String()
	uid := "user-conc-" + agentID.String()[:8]

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errChan := make(chan error, goroutines)

	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			path := fmt.Sprintf("conc/doc-%d.md", i)
			if err := ms.PutDocument(ctx, aid, uid, path, fmt.Sprintf("content-%d", i)); err != nil {
				errChan <- fmt.Errorf("PutDocument %d: %w", i, err)
			}
		}(i)
	}
	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Error(err)
	}

	// Verify all documents exist
	docs, err := ms.ListDocuments(ctx, aid, uid)
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}

	foundCount := 0
	for _, d := range docs {
		for i := range goroutines {
			if d.Path == fmt.Sprintf("conc/doc-%d.md", i) {
				foundCount++
				break
			}
		}
	}
	if foundCount != goroutines {
		t.Errorf("expected %d concurrent docs, found %d", goroutines, foundCount)
	}
}

func TestStoreMemory_ConcurrentSamePath(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := newMemoryStore(db)

	aid := agentID.String()
	uid := "user-race-" + agentID.String()[:8]
	path := "race/single.md"

	const goroutines = 15
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			ms.PutDocument(ctx, aid, uid, path, fmt.Sprintf("version-%d", i))
		}(i)
	}
	wg.Wait()

	// Last-write-wins: content should be one of the versions
	content, err := ms.GetDocument(ctx, aid, uid, path)
	if err != nil {
		t.Fatalf("GetDocument: %v", err)
	}
	if content == "" {
		t.Error("expected non-empty content after concurrent writes")
	}

	// Verify content matches one of the written versions
	validVersion := false
	for i := range goroutines {
		if content == fmt.Sprintf("version-%d", i) {
			validVersion = true
			break
		}
	}
	if !validVersion {
		t.Errorf("content %q doesn't match any written version", content)
	}
}

func TestStoreMemory_ConcurrentIndexDocument(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ms := newMemoryStore(db)

	aid := agentID.String()
	uid := "user-idx-" + agentID.String()[:8]

	// Create documents first
	for i := range 5 {
		path := fmt.Sprintf("idx/doc-%d.md", i)
		if err := ms.PutDocument(ctx, aid, uid, path, fmt.Sprintf("indexable content %d for search", i)); err != nil {
			t.Fatalf("PutDocument %d: %v", i, err)
		}
	}

	// Concurrent indexing
	const goroutines = 5
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errChan := make(chan error, goroutines)

	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			path := fmt.Sprintf("idx/doc-%d.md", i)
			if err := ms.IndexDocument(ctx, aid, uid, path); err != nil {
				errChan <- fmt.Errorf("IndexDocument %d: %w", i, err)
			}
		}(i)
	}
	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Error(err)
	}
}
