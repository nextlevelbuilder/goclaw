package memory

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// fakeStore implements just the MemoryStore methods DiskSeeder uses.
// We only care about ListDocuments (for the hash short-circuit),
// PutDocument (for capturing what got upserted), and IndexDocument.
type fakeStore struct {
	mu        sync.Mutex
	docs      map[string]string // path → content
	hashes    map[string]string // path → hash
	indexed   []string
	putErr    error
	indexErr  error
	listErr   error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		docs:   map[string]string{},
		hashes: map[string]string{},
	}
}

func (f *fakeStore) ListDocuments(_ context.Context, _, _ string) ([]store.DocumentInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]store.DocumentInfo, 0, len(f.hashes))
	for p, h := range f.hashes {
		out = append(out, store.DocumentInfo{Path: p, Hash: h})
	}
	return out, nil
}

func (f *fakeStore) PutDocument(_ context.Context, _, _, path, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.putErr != nil {
		return f.putErr
	}
	f.docs[path] = content
	f.hashes[path] = ContentHash(content)
	return nil
}

func (f *fakeStore) IndexDocument(_ context.Context, _, _, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.indexErr != nil {
		return f.indexErr
	}
	f.indexed = append(f.indexed, path)
	return nil
}

// Stub the rest of MemoryStore so we satisfy the interface without
// implementing anything we don't exercise.
func (f *fakeStore) GetDocument(context.Context, string, string, string) (string, error) {
	return "", nil
}
func (f *fakeStore) DeleteDocument(context.Context, string, string, string) error { return nil }
func (f *fakeStore) ListAllDocumentsGlobal(context.Context) ([]store.DocumentInfo, error) {
	return nil, nil
}
func (f *fakeStore) ListAllDocuments(context.Context, string) ([]store.DocumentInfo, error) {
	return nil, nil
}
func (f *fakeStore) GetDocumentDetail(context.Context, string, string, string) (*store.DocumentDetail, error) {
	return nil, nil
}
func (f *fakeStore) ListChunks(context.Context, string, string, string) ([]store.ChunkInfo, error) {
	return nil, nil
}
func (f *fakeStore) Search(context.Context, string, string, string, store.MemorySearchOptions) ([]store.MemorySearchResult, error) {
	return nil, nil
}
func (f *fakeStore) IndexAll(context.Context, string, string) error { return nil }
func (f *fakeStore) GetBacklinks(context.Context, string, string, string) ([]store.BacklinkInfo, error) {
	return nil, nil
}
func (f *fakeStore) SetEmbeddingProvider(store.EmbeddingProvider) {}
func (f *fakeStore) Close() error                                 { return nil }

// Compile-time interface assertion.
var _ store.MemoryStore = (*fakeStore)(nil)

func TestDiskSeeder_FreshSweepIndexesAllMd(t *testing.T) {
	ws := t.TempDir()
	memDir := filepath.Join(ws, "memory")
	mustWrite(t, filepath.Join(memDir, "a.md"), "# A")
	mustWrite(t, filepath.Join(memDir, "sub/b.md"), "# B")
	mustWrite(t, filepath.Join(memDir, "ignore.txt"), "not markdown")

	store := newFakeStore()
	seeder := &DiskSeeder{Store: store, Workspace: ws, AgentID: "agent-id"}
	res, err := seeder.Sweep(context.Background())
	if err != nil {
		t.Fatalf("sweep err: %v", err)
	}
	if res.Indexed != 2 || res.Skipped != 0 {
		t.Errorf("indexed=%d skipped=%d, want 2/0", res.Indexed, res.Skipped)
	}
	want := map[string]bool{"memory/a.md": true, "memory/sub/b.md": true}
	got := map[string]bool{}
	for _, p := range store.indexed {
		got[p] = true
	}
	if len(got) != len(want) {
		t.Errorf("indexed paths mismatch\n got: %v\nwant: %v", got, want)
	}
	for k := range want {
		if !got[k] {
			t.Errorf("missing indexed path: %s", k)
		}
	}
}

func TestDiskSeeder_SkipsUnchangedFiles(t *testing.T) {
	ws := t.TempDir()
	memDir := filepath.Join(ws, "memory")
	mustWrite(t, filepath.Join(memDir, "stable.md"), "stable content")
	mustWrite(t, filepath.Join(memDir, "changed.md"), "old content")

	st := newFakeStore()
	st.hashes["memory/stable.md"] = ContentHash("stable content")
	st.hashes["memory/changed.md"] = ContentHash("OLD content (different from disk)")

	seeder := &DiskSeeder{Store: st, Workspace: ws, AgentID: "agent-id"}
	res, err := seeder.Sweep(context.Background())
	if err != nil {
		t.Fatalf("sweep err: %v", err)
	}
	if res.Indexed != 1 || res.Skipped != 1 {
		t.Errorf("indexed=%d skipped=%d, want 1/1", res.Indexed, res.Skipped)
	}
	if len(st.indexed) != 1 || st.indexed[0] != "memory/changed.md" {
		t.Errorf("only changed.md should be re-indexed; got %v", st.indexed)
	}
}

func TestDiskSeeder_NoMemoryDir_NotAnError(t *testing.T) {
	ws := t.TempDir() // no memory/ subdir
	st := newFakeStore()
	seeder := &DiskSeeder{Store: st, Workspace: ws, AgentID: "agent-id"}
	res, err := seeder.Sweep(context.Background())
	if err != nil {
		t.Fatalf("sweep err: %v", err)
	}
	if res.Indexed != 0 || res.Skipped != 0 {
		t.Errorf("expected zero result, got %+v", res)
	}
}

func TestDiskSeeder_SkipsOversizedFiles(t *testing.T) {
	ws := t.TempDir()
	memDir := filepath.Join(ws, "memory")
	big := make([]byte, 1024)
	mustWrite(t, filepath.Join(memDir, "big.md"), string(big))

	st := newFakeStore()
	seeder := &DiskSeeder{Store: st, Workspace: ws, AgentID: "agent-id", MaxFileBytes: 100}
	res, err := seeder.Sweep(context.Background())
	if err != nil {
		t.Fatalf("sweep err: %v", err)
	}
	if res.Indexed != 0 || res.Skipped != 1 {
		t.Errorf("indexed=%d skipped=%d, want 0/1", res.Indexed, res.Skipped)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
