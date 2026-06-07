package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/vault"
)

// masterTenant resolves TenantWorkspace to h.workspace unchanged, keeping the
// content-write tests pointed at a single temp dir.
var masterTenant = uuid.MustParse("0193a5b0-7000-7000-8000-000000000001")

func masterCtx() context.Context {
	return store.WithTenantID(context.Background(), masterTenant)
}

// ---- writeDocumentContent: path & symlink containment (the blocking finding) ----

func TestWriteDocumentContent_WritesInsideWorkspace(t *testing.T) {
	h := &VaultHandler{}
	ws := t.TempDir()

	hash, err := h.writeDocumentContent(ws, "notes/hello.md", []byte("hi"))
	if err != nil {
		t.Fatalf("writeDocumentContent: %v", err)
	}
	if want := vault.ContentHash([]byte("hi")); hash != want {
		t.Errorf("hash = %q, want %q", hash, want)
	}
	got, err := os.ReadFile(filepath.Join(ws, "notes", "hello.md"))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(got) != "hi" {
		t.Errorf("content = %q, want %q", got, "hi")
	}
}

func TestWriteDocumentContent_EmptyContent(t *testing.T) {
	h := &VaultHandler{}
	ws := t.TempDir()

	hash, err := h.writeDocumentContent(ws, "empty.md", []byte(""))
	if err != nil {
		t.Fatalf("writeDocumentContent: %v", err)
	}
	if want := vault.ContentHash([]byte("")); hash != want {
		t.Errorf("hash = %q, want %q", hash, want)
	}
	fi, err := os.Stat(filepath.Join(ws, "empty.md"))
	if err != nil {
		t.Fatalf("stat empty file: %v", err)
	}
	if fi.Size() != 0 {
		t.Errorf("size = %d, want 0", fi.Size())
	}
}

func TestWriteDocumentContent_RejectsParentEscape(t *testing.T) {
	h := &VaultHandler{}
	ws := t.TempDir()

	_, err := h.writeDocumentContent(ws, "../escape.md", []byte("x"))
	if !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("err = %v, want os.ErrInvalid", err)
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(ws), "escape.md")); statErr == nil {
		t.Error("file escaped the workspace via ../")
	}
}

// A symlink that lives inside the workspace but points outside it must not be
// followed — this is the exact escape the lexical prefix check missed.
func TestWriteDocumentContent_RejectsSymlinkDirEscape(t *testing.T) {
	h := &VaultHandler{}
	ws := t.TempDir()
	outside := t.TempDir()

	if err := os.Symlink(outside, filepath.Join(ws, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	_, err := h.writeDocumentContent(ws, "link/pwn.md", []byte("x"))
	if !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("err = %v, want os.ErrInvalid", err)
	}
	if _, statErr := os.Stat(filepath.Join(outside, "pwn.md")); statErr == nil {
		t.Error("file written outside workspace through symlinked directory")
	}
}

// A symlink planted at the final path component must be refused rather than
// followed, so an attacker can't redirect a write onto an existing outside file.
func TestWriteDocumentContent_RejectsFinalComponentSymlink(t *testing.T) {
	h := &VaultHandler{}
	ws := t.TempDir()
	outside := t.TempDir()

	outsideFile := filepath.Join(outside, "target.md")
	if err := os.WriteFile(outsideFile, []byte("original"), 0o644); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(ws, "evil.md")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	_, err := h.writeDocumentContent(ws, "evil.md", []byte("pwned"))
	if !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("err = %v, want os.ErrInvalid", err)
	}
	got, _ := os.ReadFile(outsideFile)
	if string(got) != "original" {
		t.Errorf("outside file clobbered through final-component symlink: %q", got)
	}
}

// ---- handler-level: extension whitelist, event ordering, empty PUT, orphan ----

// fakeVaultStore embeds the interface (unimplemented methods panic if hit) and
// overrides only the two methods the content path touches.
type fakeVaultStore struct {
	store.VaultStore
	docs       map[string]*store.VaultDocument
	upserted   bool
	upsertErr  error
	upsertCall int
}

func newFakeVaultStore() *fakeVaultStore {
	return &fakeVaultStore{docs: map[string]*store.VaultDocument{}}
}

func (f *fakeVaultStore) UpsertDocument(_ context.Context, doc *store.VaultDocument) error {
	f.upsertCall++
	if f.upsertErr != nil {
		return f.upsertErr
	}
	if doc.ID == "" {
		doc.ID = "doc-" + uuid.NewString()
	}
	f.upserted = true
	f.docs[doc.ID] = doc
	return nil
}

func (f *fakeVaultStore) GetDocumentByID(_ context.Context, _ /*tenantID*/, id string) (*store.VaultDocument, error) {
	return f.docs[id], nil
}

// fakeEventBus captures published events and records whether the DB upsert had
// already happened at publish time, to assert ordering.
type fakeEventBus struct {
	store          *fakeVaultStore
	published      []eventbus.DomainEvent
	upsertedAtFire bool
}

func (b *fakeEventBus) Publish(e eventbus.DomainEvent) {
	if b.store != nil {
		b.upsertedAtFire = b.store.upserted
	}
	b.published = append(b.published, e)
}
func (b *fakeEventBus) Subscribe(eventbus.EventType, eventbus.DomainEventHandler) func() {
	return func() {}
}
func (b *fakeEventBus) Start(context.Context)     {}
func (b *fakeEventBus) Drain(time.Duration) error { return nil }

func newJSONRequest(t *testing.T, method, target string, body any) *http.Request {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	return req.WithContext(masterCtx())
}

func TestHandleCreateDocument_RejectsDisallowedExtension(t *testing.T) {
	h := &VaultHandler{} // extension check fires before any store/workspace access
	req := newJSONRequest(t, http.MethodPost, "/v1/vault/documents", map[string]any{
		"path":    "payload.exe",
		"title":   "x",
		"content": "data",
	})
	rr := httptest.NewRecorder()

	h.handleCreateDocument(rr, req)

	assertBadRequest(t, rr, "unsupported file type")
}

func TestHandleCreateDocument_WritesContentAndPublishesAfterUpsert(t *testing.T) {
	st := newFakeVaultStore()
	bus := &fakeEventBus{store: st}
	ws := t.TempDir()
	h := &VaultHandler{store: st, eventBus: bus, workspace: ws}

	req := newJSONRequest(t, http.MethodPost, "/v1/vault/documents", map[string]any{
		"path":    "decisions/adr-001.md",
		"title":   "ADR 001",
		"content": "# Decision\nuse postgres",
	})
	rr := httptest.NewRecorder()

	h.handleCreateDocument(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", rr.Code, rr.Body.String())
	}
	// File materialised on disk.
	got, err := os.ReadFile(filepath.Join(ws, "decisions", "adr-001.md"))
	if err != nil {
		t.Fatalf("read written doc: %v", err)
	}
	if string(got) != "# Decision\nuse postgres" {
		t.Errorf("content = %q", got)
	}
	// Exactly one enrichment event, fired AFTER the DB upsert.
	if len(bus.published) != 1 {
		t.Fatalf("published %d events, want 1", len(bus.published))
	}
	if !bus.upsertedAtFire {
		t.Error("event published before DB upsert — enrichment worker could race ahead of the row")
	}
	if bus.published[0].Type != eventbus.EventVaultDocUpserted {
		t.Errorf("event type = %v, want %v", bus.published[0].Type, eventbus.EventVaultDocUpserted)
	}
}

func TestHandleCreateDocument_NoContentSkipsWriteAndEvent(t *testing.T) {
	st := newFakeVaultStore()
	bus := &fakeEventBus{store: st}
	ws := t.TempDir()
	h := &VaultHandler{store: st, eventBus: bus, workspace: ws}

	req := newJSONRequest(t, http.MethodPost, "/v1/vault/documents", map[string]any{
		"path":  "meta-only.md",
		"title": "Meta only",
	})
	rr := httptest.NewRecorder()

	h.handleCreateDocument(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", rr.Code, rr.Body.String())
	}
	if _, statErr := os.Stat(filepath.Join(ws, "meta-only.md")); statErr == nil {
		t.Error("metadata-only create must not write a file")
	}
	if len(bus.published) != 0 {
		t.Errorf("metadata-only create published %d events, want 0", len(bus.published))
	}
}

// Documents the orphan-file behaviour: when the DB upsert fails after the bytes
// are already on disk, the handler returns 500 and the file is intentionally
// left in place (no rollback). If that ever changes, this test should change
// with it.
func TestHandleCreateDocument_UpsertFailureLeavesFile(t *testing.T) {
	st := newFakeVaultStore()
	st.upsertErr = errors.New("db down")
	bus := &fakeEventBus{store: st}
	ws := t.TempDir()
	h := &VaultHandler{store: st, eventBus: bus, workspace: ws}

	req := newJSONRequest(t, http.MethodPost, "/v1/vault/documents", map[string]any{
		"path":    "orphan.md",
		"title":   "Orphan",
		"content": "written before upsert",
	})
	rr := httptest.NewRecorder()

	h.handleCreateDocument(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body: %s)", rr.Code, rr.Body.String())
	}
	if _, statErr := os.Stat(filepath.Join(ws, "orphan.md")); statErr != nil {
		t.Errorf("file should remain after upsert failure (orphan accepted): %v", statErr)
	}
	if len(bus.published) != 0 {
		t.Errorf("no event should fire when upsert fails, got %d", len(bus.published))
	}
}

func TestHandleUpdateDocument_EmptyContentClearsFile(t *testing.T) {
	st := newFakeVaultStore()
	st.docs["existing-1"] = &store.VaultDocument{
		ID:       "existing-1",
		TenantID: masterTenant.String(),
		Path:     "notes/keep.md",
		Title:    "Keep",
		DocType:  "note",
		Scope:    "shared",
	}
	bus := &fakeEventBus{store: st}
	ws := t.TempDir()
	h := &VaultHandler{store: st, eventBus: bus, workspace: ws}

	empty := ""
	req := newJSONRequest(t, http.MethodPut, "/v1/vault/documents/existing-1", map[string]any{
		"content": empty,
	})
	req.SetPathValue("docID", "existing-1")
	rr := httptest.NewRecorder()

	h.handleUpdateDocument(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rr.Code, rr.Body.String())
	}
	fi, err := os.Stat(filepath.Join(ws, "notes", "keep.md"))
	if err != nil {
		t.Fatalf("stat cleared file: %v", err)
	}
	if fi.Size() != 0 {
		t.Errorf("file size = %d, want 0 (empty content clears file)", fi.Size())
	}
	if st.docs["existing-1"].ContentHash != vault.ContentHash([]byte("")) {
		t.Errorf("content_hash = %q, want empty-content hash", st.docs["existing-1"].ContentHash)
	}
	if len(bus.published) != 1 {
		t.Errorf("published %d events, want 1", len(bus.published))
	}
}

// Locks the "no change" contract on PUT: omitting `content` (nil pointer in
// the body struct) must not touch the disk or fire an enrichment event, even
// when other metadata fields are present.
func TestHandleUpdateDocument_NilContentSkipsWriteAndEvent(t *testing.T) {
	st := newFakeVaultStore()
	st.docs["existing-2"] = &store.VaultDocument{
		ID:          "existing-2",
		TenantID:    masterTenant.String(),
		Path:        "notes/keep.md",
		Title:       "Old",
		DocType:     "note",
		Scope:       "shared",
		ContentHash: "preexisting-hash",
	}
	bus := &fakeEventBus{store: st}
	ws := t.TempDir()
	h := &VaultHandler{store: st, eventBus: bus, workspace: ws}

	// content field intentionally omitted → *string is nil → no write, no event.
	req := newJSONRequest(t, http.MethodPut, "/v1/vault/documents/existing-2", map[string]any{
		"title": "Renamed but content untouched",
	})
	req.SetPathValue("docID", "existing-2")
	rr := httptest.NewRecorder()

	h.handleUpdateDocument(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rr.Code, rr.Body.String())
	}
	if _, statErr := os.Stat(filepath.Join(ws, "notes", "keep.md")); statErr == nil {
		t.Error("nil content must not materialise a file")
	}
	if st.docs["existing-2"].ContentHash != "preexisting-hash" {
		t.Errorf("content_hash = %q, want unchanged 'preexisting-hash'", st.docs["existing-2"].ContentHash)
	}
	if len(bus.published) != 0 {
		t.Errorf("nil-content PUT must not publish events, got %d", len(bus.published))
	}
}

// Handler-level guard at vault_handler_documents.go path validation must reject
// "..", short-circuiting before writeDocumentContent even runs.
func TestHandleCreateDocument_RejectsParentTraversalPath(t *testing.T) {
	h := &VaultHandler{} // path check runs before any store/workspace access
	req := newJSONRequest(t, http.MethodPost, "/v1/vault/documents", map[string]any{
		"path":    "../../etc/passwd.md",
		"title":   "x",
		"content": "evil",
	})
	rr := httptest.NewRecorder()

	h.handleCreateDocument(rr, req)

	assertBadRequest(t, rr, "invalid path")
}

// Mirrors PUT semantics on POST: `content: ""` writes a 0-byte file and
// fires the enrichment event (the I4 fix that aligned POST/PUT).
func TestHandleCreateDocument_EmptyContentWritesEmptyFile(t *testing.T) {
	st := newFakeVaultStore()
	bus := &fakeEventBus{store: st}
	ws := t.TempDir()
	h := &VaultHandler{store: st, eventBus: bus, workspace: ws}

	req := newJSONRequest(t, http.MethodPost, "/v1/vault/documents", map[string]any{
		"path":    "placeholder.md",
		"title":   "Placeholder",
		"content": "",
	})
	rr := httptest.NewRecorder()

	h.handleCreateDocument(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", rr.Code, rr.Body.String())
	}
	fi, err := os.Stat(filepath.Join(ws, "placeholder.md"))
	if err != nil {
		t.Fatalf("stat empty file: %v", err)
	}
	if fi.Size() != 0 {
		t.Errorf("size = %d, want 0", fi.Size())
	}
	if len(bus.published) != 1 {
		t.Errorf("published %d events, want 1", len(bus.published))
	}
}

// B1 regression: non-master tenants resolve to workspace/tenants/{slug}/ which
// is created on demand. The very first JSON content write into a fresh tenant
// must NOT 500 because EvalSymlinks can't find that dir — writeDocumentContent
// is responsible for creating it (mirrors what handleUpload does).
func TestHandleCreateDocument_NonMasterTenantFirstContentWrite(t *testing.T) {
	st := newFakeVaultStore()
	bus := &fakeEventBus{store: st}
	ws := t.TempDir()
	h := &VaultHandler{store: st, eventBus: bus, workspace: ws}

	nonMaster := uuid.MustParse("01999999-1111-7000-8000-000000000abc")
	ctx := store.WithTenantSlug(store.WithTenantID(context.Background(), nonMaster), "acme")

	// newJSONRequest builds against masterCtx by default; WithContext swaps in
	// the non-master tenant context for this single test.
	req := newJSONRequest(t, http.MethodPost, "/v1/vault/documents", map[string]any{
		"path":    "notes/first.md",
		"title":   "First",
		"content": "hello from acme",
	}).WithContext(ctx)
	rr := httptest.NewRecorder()

	h.handleCreateDocument(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", rr.Code, rr.Body.String())
	}
	tenantDir := filepath.Join(ws, "tenants", "acme")
	got, err := os.ReadFile(filepath.Join(tenantDir, "notes", "first.md"))
	if err != nil {
		t.Fatalf("read non-master tenant doc: %v", err)
	}
	if string(got) != "hello from acme" {
		t.Errorf("content = %q", got)
	}
	if len(bus.published) != 1 {
		t.Errorf("published %d events, want 1", len(bus.published))
	}
}
