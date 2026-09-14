package vault

import (
	"context"
	"sync"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type fakeVaultStoreLinks struct {
	store.VaultStore
	mu          sync.Mutex
	docsByPath  map[string]*store.VaultDocument
	created     []store.VaultLink
	deletedType bool
}

func (f *fakeVaultStoreLinks) GetDocument(_ context.Context, _, _, path string) (*store.VaultDocument, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if doc, ok := f.docsByPath[path]; ok {
		return doc, nil
	}
	return nil, nil
}

func (f *fakeVaultStoreLinks) GetDocumentByBasename(_ context.Context, _, _, basename string) (*store.VaultDocument, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, doc := range f.docsByPath {
		if doc.Path == basename || doc.Title == basename {
			return doc, nil
		}
	}
	return nil, nil
}

func (f *fakeVaultStoreLinks) DeleteDocLinksByType(_ context.Context, _, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletedType = true
	return nil
}

func (f *fakeVaultStoreLinks) CreateLinks(_ context.Context, links []store.VaultLink) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, links...)
	return nil
}

func TestSyncDocLinks_DeduplicatesSameTarget(t *testing.T) {
	targetDoc := &store.VaultDocument{ID: "target-id-1", Title: "products", Path: "products.md"}
	sourceDoc := &store.VaultDocument{ID: "source-id-1", Title: "notes", Path: "notes.md"}

	fake := &fakeVaultStoreLinks{
		docsByPath: map[string]*store.VaultDocument{
			"products":   targetDoc,
			"products.md": targetDoc,
		},
	}

	content := "First mention [[products]] here and later [[products]] again."

	err := SyncDocLinks(context.Background(), fake, sourceDoc, content, "tenant-1", "agent-1")
	if err != nil {
		t.Fatalf("SyncDocLinks() error = %v", err)
	}

	if len(fake.created) != 1 {
		t.Fatalf("expected 1 deduplicated link, got %d", len(fake.created))
	}

	link := fake.created[0]
	if link.FromDocID != sourceDoc.ID {
		t.Errorf("FromDocID = %q, want %q", link.FromDocID, sourceDoc.ID)
	}
	if link.ToDocID != targetDoc.ID {
		t.Errorf("ToDocID = %q, want %q", link.ToDocID, targetDoc.ID)
	}
	if link.LinkType != "wikilink" {
		t.Errorf("LinkType = %q, want %q", link.LinkType, "wikilink")
	}
}

func TestSyncDocLinks_PreservesDistinctTargets(t *testing.T) {
	targetA := &store.VaultDocument{ID: "target-a", Title: "alpha", Path: "alpha.md"}
	targetB := &store.VaultDocument{ID: "target-b", Title: "beta", Path: "beta.md"}
	sourceDoc := &store.VaultDocument{ID: "source-id-2", Title: "notes", Path: "notes.md"}

	fake := &fakeVaultStoreLinks{
		docsByPath: map[string]*store.VaultDocument{
			"alpha":   targetA,
			"alpha.md": targetA,
			"beta":    targetB,
			"beta.md":  targetB,
		},
	}

	content := "See [[alpha]] and [[beta]] and [[alpha]] again."

	err := SyncDocLinks(context.Background(), fake, sourceDoc, content, "tenant-1", "agent-1")
	if err != nil {
		t.Fatalf("SyncDocLinks() error = %v", err)
	}

	if len(fake.created) != 2 {
		t.Fatalf("expected 2 links (alpha + beta), got %d", len(fake.created))
	}

	targetIDs := map[string]bool{}
	for _, l := range fake.created {
		targetIDs[l.ToDocID] = true
	}
	if !targetIDs["target-a"] {
		t.Error("missing link to target-a (alpha)")
	}
	if !targetIDs["target-b"] {
		t.Error("missing link to target-b (beta)")
	}
}
