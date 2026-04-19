package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// fakeVaultStore embeds store.VaultStore (nil) so the struct satisfies the
// interface at compile time. Methods used by vault_read.Execute are
// implemented; others would nil-panic if called.
type fakeVaultStore struct {
	store.VaultStore
	byID       map[string]*store.VaultDocument // key: tenantID + ":" + docID
	outlinks   map[string][]store.VaultLink    // key: tenantID + ":" + docID
	outLinkErr error                           // injected error for GetOutLinks
	targetsErr error                           // injected error for GetDocumentsByIDs
}

func (f *fakeVaultStore) GetDocumentByID(ctx context.Context, tenantID, id string) (*store.VaultDocument, error) {
	if f.byID == nil {
		return nil, os.ErrNotExist
	}
	doc, ok := f.byID[tenantID+":"+id]
	if !ok {
		return nil, os.ErrNotExist
	}
	return doc, nil
}

func (f *fakeVaultStore) GetOutLinks(ctx context.Context, tenantID, docID string) ([]store.VaultLink, error) {
	if f.outLinkErr != nil {
		return nil, f.outLinkErr
	}
	if f.outlinks == nil {
		return nil, nil
	}
	return f.outlinks[tenantID+":"+docID], nil
}

func (f *fakeVaultStore) GetDocumentsByIDs(ctx context.Context, tenantID string, docIDs []string) ([]store.VaultDocument, error) {
	if f.targetsErr != nil {
		return nil, f.targetsErr
	}
	out := make([]store.VaultDocument, 0, len(docIDs))
	for _, id := range docIDs {
		if d, ok := f.byID[tenantID+":"+id]; ok {
			out = append(out, *d)
		}
	}
	return out, nil
}

// newVaultReadTestTool builds a VaultReadTool with a temp workspace and a
// fake store pre-seeded with docs.
func newVaultReadTestTool(t *testing.T, docs ...*store.VaultDocument) (*VaultReadTool, string) {
	t.Helper()
	ws := t.TempDir()
	fake := &fakeVaultStore{byID: make(map[string]*store.VaultDocument)}
	for _, d := range docs {
		fake.byID[d.TenantID+":"+d.ID] = d
	}
	tool := NewVaultReadTool()
	tool.SetVaultStore(fake)
	tool.SetWorkspace(ws)
	return tool, ws
}

// writeFile creates a file under workspace with the given relative path.
func writeFile(t *testing.T, ws, rel, content string) {
	t.Helper()
	full := filepath.Join(ws, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// writeBytes creates a file with raw bytes (for binary content tests).
func writeBytes(t *testing.T, ws, rel string, b []byte) {
	t.Helper()
	full := filepath.Join(ws, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, b, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func makeCtx(tenantID, agentID uuid.UUID) context.Context {
	ctx := context.Background()
	if tenantID != uuid.Nil {
		ctx = store.WithTenantID(ctx, tenantID)
	}
	if agentID != uuid.Nil {
		ctx = store.WithAgentID(ctx, agentID)
	}
	return ctx
}

func makeCtxWithTeam(tenantID, agentID uuid.UUID, teamID string) context.Context {
	ctx := makeCtx(tenantID, agentID)
	return store.WithRunContext(ctx, &store.RunContext{
		TenantID: tenantID,
		AgentID:  agentID,
		TeamID:   teamID,
	})
}

// --- 1. shared scope → allow, content returned. ---
func TestVaultRead_SharedScope_Allow(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	docID := uuid.New()
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantID.String(),
		Scope: "shared", Path: "shared/notes.md", Title: "Notes",
		DocType: "note",
	}
	tool, ws := newVaultReadTestTool(t, doc)
	writeFile(t, ws, "shared/notes.md", "hello world")

	res := tool.Execute(makeCtx(tenantID, agentID),
		map[string]any{"doc_id": docID.String()})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "hello world") {
		t.Fatalf("content missing: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "Notes") {
		t.Fatalf("title missing: %s", res.ForLLM)
	}
}

// --- 2. personal scope, AgentID matches ctx → allow. ---
func TestVaultRead_PersonalScope_Match_Allow(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	docID := uuid.New()
	aid := agentID.String()
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantID.String(),
		AgentID: &aid, Scope: "personal", Path: "memo.md",
		Title: "Memo", DocType: "note",
	}
	tool, ws := newVaultReadTestTool(t, doc)
	writeFile(t, ws, "memo.md", "personal body")

	res := tool.Execute(makeCtx(tenantID, agentID),
		map[string]any{"doc_id": docID.String()})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "personal body") {
		t.Fatalf("content missing: %s", res.ForLLM)
	}
}

// --- 3. personal scope, AgentID mismatch → deny. ---
func TestVaultRead_PersonalScope_Mismatch_Deny(t *testing.T) {
	tenantID := uuid.New()
	agentA := uuid.New()
	agentB := uuid.New()
	docID := uuid.New()
	aid := agentA.String()
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantID.String(),
		AgentID: &aid, Scope: "personal", Path: "memo.md",
		Title: "Memo", DocType: "note",
	}
	tool, ws := newVaultReadTestTool(t, doc)
	writeFile(t, ws, "memo.md", "personal body")

	res := tool.Execute(makeCtx(tenantID, agentB),
		map[string]any{"doc_id": docID.String()})
	if !res.IsError || !strings.Contains(res.ForLLM, "not accessible") {
		t.Fatalf("expected access denied, got: %s", res.ForLLM)
	}
}

// --- 4. team scope, RunContext.TeamID matches → allow. ---
func TestVaultRead_TeamScope_Match_Allow(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	docID := uuid.New()
	teamID := uuid.New().String()
	tid := teamID
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantID.String(),
		TeamID: &tid, Scope: "team", Path: "team/doc.md",
		Title: "Team Doc", DocType: "note",
	}
	tool, ws := newVaultReadTestTool(t, doc)
	writeFile(t, ws, "team/doc.md", "team body")

	res := tool.Execute(makeCtxWithTeam(tenantID, agentID, teamID),
		map[string]any{"doc_id": docID.String()})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "team body") {
		t.Fatalf("content missing: %s", res.ForLLM)
	}
}

// --- 5. team scope, no run-context / mismatch → deny. ---
func TestVaultRead_TeamScope_NoContext_Deny(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	docID := uuid.New()
	tid := uuid.New().String()
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantID.String(),
		TeamID: &tid, Scope: "team", Path: "team/doc.md",
		Title: "Team Doc", DocType: "note",
	}
	tool, ws := newVaultReadTestTool(t, doc)
	writeFile(t, ws, "team/doc.md", "team body")

	// no team in ctx.
	res := tool.Execute(makeCtx(tenantID, agentID),
		map[string]any{"doc_id": docID.String()})
	if !res.IsError {
		t.Fatalf("expected deny, got: %s", res.ForLLM)
	}

	// mismatched team in ctx.
	res2 := tool.Execute(makeCtxWithTeam(tenantID, agentID, uuid.New().String()),
		map[string]any{"doc_id": docID.String()})
	if !res2.IsError {
		t.Fatalf("expected deny on mismatch, got: %s", res2.ForLLM)
	}
}

// --- 6. cross-tenant (different tenant in ctx) → not-found. ---
func TestVaultRead_CrossTenant_NotFound(t *testing.T) {
	tenantA := uuid.New()
	tenantB := uuid.New()
	agentID := uuid.New()
	docID := uuid.New()
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantA.String(),
		Scope: "shared", Path: "a.md", Title: "A", DocType: "note",
	}
	tool, ws := newVaultReadTestTool(t, doc)
	writeFile(t, ws, "a.md", "body")

	res := tool.Execute(makeCtx(tenantB, agentID),
		map[string]any{"doc_id": docID.String()})
	if !res.IsError || !strings.Contains(res.ForLLM, "not found") {
		t.Fatalf("expected not found, got: %s", res.ForLLM)
	}
}

// --- 7. missing doc id → not-found. ---
func TestVaultRead_MissingDoc_NotFound(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	tool, _ := newVaultReadTestTool(t)

	res := tool.Execute(makeCtx(tenantID, agentID),
		map[string]any{"doc_id": uuid.New().String()})
	if !res.IsError || !strings.Contains(res.ForLLM, "not found") {
		t.Fatalf("expected not found, got: %s", res.ForLLM)
	}
}

// --- 8. invalid UUID → arg error. ---
func TestVaultRead_InvalidUUID(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	tool, _ := newVaultReadTestTool(t)

	res := tool.Execute(makeCtx(tenantID, agentID),
		map[string]any{"doc_id": "not-a-uuid"})
	if !res.IsError || !strings.Contains(res.ForLLM, "invalid doc_id") {
		t.Fatalf("expected invalid doc_id error, got: %s", res.ForLLM)
	}
}

// --- 9. media DocType → rejected with hint. ---
func TestVaultRead_MediaDocType_Rejected(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	docID := uuid.New()
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantID.String(),
		Scope: "shared", Path: "pic.png", Title: "Pic", DocType: "media",
	}
	tool, ws := newVaultReadTestTool(t, doc)
	writeFile(t, ws, "pic.png", "pretend-png")

	res := tool.Execute(makeCtx(tenantID, agentID),
		map[string]any{"doc_id": docID.String()})
	if !res.IsError || !strings.Contains(res.ForLLM, "read_image") {
		t.Fatalf("expected media-handler hint, got: %s", res.ForLLM)
	}
}

// --- 10. binary extension even when DocType!=media → rejected by blocklist. ---
func TestVaultRead_BinaryExtension_Rejected(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	docID := uuid.New()
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantID.String(),
		Scope: "shared", Path: "report.PDF", Title: "Rep", DocType: "document",
	}
	tool, ws := newVaultReadTestTool(t, doc)
	writeFile(t, ws, "report.PDF", "%PDF-1.7")

	res := tool.Execute(makeCtx(tenantID, agentID),
		map[string]any{"doc_id": docID.String()})
	if !res.IsError || !strings.Contains(res.ForLLM, ".pdf") {
		t.Fatalf("expected pdf blocklist error, got: %s", res.ForLLM)
	}
}

// --- 11. text extension but binary bytes → rejected by UTF-8 sniff. ---
func TestVaultRead_BinaryContent_UTF8Rejected(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	docID := uuid.New()
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantID.String(),
		Scope: "shared", Path: "blob.txt", Title: "Blob", DocType: "note",
	}
	tool, ws := newVaultReadTestTool(t, doc)
	// Invalid UTF-8: stray 0xC3 byte with no continuation.
	writeBytes(t, ws, "blob.txt", []byte{0xC3, 0x28, 0xFF, 0xFE, 0xFD})

	res := tool.Execute(makeCtx(tenantID, agentID),
		map[string]any{"doc_id": docID.String()})
	if !res.IsError || !strings.Contains(res.ForLLM, "UTF-8") {
		t.Fatalf("expected UTF-8 rejection, got: %s", res.ForLLM)
	}
}

// --- 12. oversize file → truncated with marker. ---
func TestVaultRead_Oversize_Truncated(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	docID := uuid.New()
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantID.String(),
		Scope: "shared", Path: "big.md", Title: "Big", DocType: "note",
	}
	tool, ws := newVaultReadTestTool(t, doc)
	// 30KB of 'a' — fits in UTF-8 sniff check, but larger than max_bytes below.
	big := strings.Repeat("a", 30_000)
	writeFile(t, ws, "big.md", big)

	res := tool.Execute(makeCtx(tenantID, agentID),
		map[string]any{"doc_id": docID.String(), "max_bytes": float64(10_000)})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "truncated") {
		t.Fatalf("expected truncation marker, got: %s", res.ForLLM)
	}
}

// --- 13. symlink escaping workspace → denied. ---
func TestVaultRead_SymlinkEscape_Denied(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	docID := uuid.New()
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantID.String(),
		Scope: "shared", Path: "escape.md", Title: "Esc", DocType: "note",
	}
	tool, ws := newVaultReadTestTool(t, doc)

	// Create target outside workspace.
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("leak"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	// Symlink ws/escape.md → outside/secret.txt.
	if err := os.Symlink(target, filepath.Join(ws, "escape.md")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	res := tool.Execute(makeCtx(tenantID, agentID),
		map[string]any{"doc_id": docID.String()})
	if !res.IsError || !strings.Contains(res.ForLLM, "outside workspace") {
		t.Fatalf("expected symlink escape denied, got: %s", res.ForLLM)
	}
}

// --- Outlinks footer tests -------------------------------------------------

// sharedDoc returns a minimal shared-scope vault doc.
func sharedDoc(tenantID uuid.UUID, title, path string) *store.VaultDocument {
	return &store.VaultDocument{
		ID: uuid.New().String(), TenantID: tenantID.String(),
		Scope: "shared", Path: path, Title: title, DocType: "note",
	}
}

// personalDoc returns a personal-scope doc owned by agentID.
func personalDoc(tenantID, agentID uuid.UUID, title, path string) *store.VaultDocument {
	aid := agentID.String()
	return &store.VaultDocument{
		ID: uuid.New().String(), TenantID: tenantID.String(),
		AgentID: &aid, Scope: "personal", Path: path, Title: title, DocType: "note",
	}
}

// teamDoc returns a team-scope doc bound to teamID.
func teamDoc(tenantID uuid.UUID, teamID, title, path string) *store.VaultDocument {
	tid := teamID
	return &store.VaultDocument{
		ID: uuid.New().String(), TenantID: tenantID.String(),
		TeamID: &tid, Scope: "team", Path: path, Title: title, DocType: "note",
	}
}

// seedWithLinks extends the fake store's outlinks map.
func seedLinks(tool *VaultReadTool, tenantID, fromID string, links []store.VaultLink) {
	f := tool.vaultStore.(*fakeVaultStore)
	if f.outlinks == nil {
		f.outlinks = make(map[string][]store.VaultLink)
	}
	f.outlinks[tenantID+":"+fromID] = links
}

// --- Case 1: outlinks present, all in scope → listed in order. ---
func TestVaultReadOutlinks_AllInScope_Listed(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	src := sharedDoc(tenantID, "Source", "src.md")
	tA := sharedDoc(tenantID, "Target A", "a.md")
	tB := sharedDoc(tenantID, "Target B", "b.md")
	tool, ws := newVaultReadTestTool(t, src, tA, tB)
	writeFile(t, ws, "src.md", "body")
	seedLinks(tool, tenantID.String(), src.ID, []store.VaultLink{
		{ToDocID: tA.ID, LinkType: "wikilink"},
		{ToDocID: tB.ID, LinkType: "reference"},
	})

	res := tool.Execute(makeCtx(tenantID, agentID), map[string]any{"doc_id": src.ID})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "## Links") {
		t.Fatalf("missing Links heading: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "Target A — id: "+tA.ID+" (wikilink)") {
		t.Fatalf("target A line missing/wrong: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "Target B — id: "+tB.ID+" (reference)") {
		t.Fatalf("target B line missing/wrong: %s", res.ForLLM)
	}
	// Order preserved (A before B).
	if strings.Index(res.ForLLM, "Target A") > strings.Index(res.ForLLM, "Target B") {
		t.Fatalf("order not preserved: %s", res.ForLLM)
	}
}

// --- Case 2: outlink to personal doc owned by another agent → dropped. ---
func TestVaultReadOutlinks_PersonalOtherAgent_Dropped(t *testing.T) {
	tenantID := uuid.New()
	agentSelf := uuid.New()
	agentOther := uuid.New()
	src := sharedDoc(tenantID, "Source", "src.md")
	other := personalDoc(tenantID, agentOther, "Other Memo", "other.md")
	tool, ws := newVaultReadTestTool(t, src, other)
	writeFile(t, ws, "src.md", "body")
	seedLinks(tool, tenantID.String(), src.ID, []store.VaultLink{
		{ToDocID: other.ID, LinkType: "wikilink"},
	})

	res := tool.Execute(makeCtx(tenantID, agentSelf), map[string]any{"doc_id": src.ID})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if strings.Contains(res.ForLLM, "Other Memo") {
		t.Fatalf("leaked other-agent personal doc: %s", res.ForLLM)
	}
	if strings.Contains(res.ForLLM, "## Links") {
		t.Fatalf("empty Links heading should not appear: %s", res.ForLLM)
	}
}

// --- Case 3: outlink to team doc from different team → dropped. ---
func TestVaultReadOutlinks_TeamOtherTeam_Dropped(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	teamSelf := uuid.New().String()
	teamOther := uuid.New().String()
	src := sharedDoc(tenantID, "Source", "src.md")
	target := teamDoc(tenantID, teamOther, "Other Team Doc", "ot.md")
	tool, ws := newVaultReadTestTool(t, src, target)
	writeFile(t, ws, "src.md", "body")
	seedLinks(tool, tenantID.String(), src.ID, []store.VaultLink{
		{ToDocID: target.ID, LinkType: "wikilink"},
	})

	res := tool.Execute(makeCtxWithTeam(tenantID, agentID, teamSelf),
		map[string]any{"doc_id": src.ID})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if strings.Contains(res.ForLLM, "Other Team Doc") {
		t.Fatalf("leaked other-team doc: %s", res.ForLLM)
	}
}

// --- Case 4: outlink to shared doc → always included. ---
func TestVaultReadOutlinks_Shared_Included(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	src := sharedDoc(tenantID, "Source", "src.md")
	tgt := sharedDoc(tenantID, "Shared Target", "st.md")
	tool, ws := newVaultReadTestTool(t, src, tgt)
	writeFile(t, ws, "src.md", "body")
	seedLinks(tool, tenantID.String(), src.ID, []store.VaultLink{
		{ToDocID: tgt.ID, LinkType: "wikilink"},
	})

	res := tool.Execute(makeCtx(tenantID, agentID), map[string]any{"doc_id": src.ID})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "Shared Target") {
		t.Fatalf("shared target missing: %s", res.ForLLM)
	}
}

// --- Case 5: task_attachment link type → dropped by type filter. ---
func TestVaultReadOutlinks_TaskAttachment_Dropped(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	src := sharedDoc(tenantID, "Source", "src.md")
	tgt := sharedDoc(tenantID, "Task Target", "tk.md")
	tool, ws := newVaultReadTestTool(t, src, tgt)
	writeFile(t, ws, "src.md", "body")
	seedLinks(tool, tenantID.String(), src.ID, []store.VaultLink{
		{ToDocID: tgt.ID, LinkType: "task_attachment"},
	})

	res := tool.Execute(makeCtx(tenantID, agentID), map[string]any{"doc_id": src.ID})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if strings.Contains(res.ForLLM, "## Links") {
		t.Fatalf("footer should be absent for task_attachment only: %s", res.ForLLM)
	}
}

// --- Case 6: broken link (target doc deleted) → dropped silently. ---
func TestVaultReadOutlinks_BrokenLink_Dropped(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	src := sharedDoc(tenantID, "Source", "src.md")
	ghostID := uuid.New().String()
	tool, ws := newVaultReadTestTool(t, src) // ghost not seeded
	writeFile(t, ws, "src.md", "body")
	seedLinks(tool, tenantID.String(), src.ID, []store.VaultLink{
		{ToDocID: ghostID, LinkType: "wikilink"},
	})

	res := tool.Execute(makeCtx(tenantID, agentID), map[string]any{"doc_id": src.ID})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if strings.Contains(res.ForLLM, "## Links") {
		t.Fatalf("footer should be absent when only broken links: %s", res.ForLLM)
	}
	if strings.Contains(res.ForLLM, ghostID) {
		t.Fatalf("ghost id leaked: %s", res.ForLLM)
	}
}

// --- Case 7: self-link → dropped. ---
func TestVaultReadOutlinks_SelfLink_Dropped(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	src := sharedDoc(tenantID, "Source", "src.md")
	tool, ws := newVaultReadTestTool(t, src)
	writeFile(t, ws, "src.md", "body")
	seedLinks(tool, tenantID.String(), src.ID, []store.VaultLink{
		{ToDocID: src.ID, LinkType: "wikilink"},
	})

	res := tool.Execute(makeCtx(tenantID, agentID), map[string]any{"doc_id": src.ID})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if strings.Contains(res.ForLLM, "## Links") {
		t.Fatalf("self-link should not produce footer: %s", res.ForLLM)
	}
}

// --- Case 8: >20 valid links → capped with overflow marker. ---
func TestVaultReadOutlinks_Overflow_Capped(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	src := sharedDoc(tenantID, "Source", "src.md")
	docs := []*store.VaultDocument{src}
	links := []store.VaultLink{}
	for i := range 25 {
		d := sharedDoc(tenantID, fmt.Sprintf("T%02d", i), fmt.Sprintf("t%02d.md", i))
		docs = append(docs, d)
		links = append(links, store.VaultLink{ToDocID: d.ID, LinkType: "wikilink"})
	}
	tool, ws := newVaultReadTestTool(t, docs...)
	writeFile(t, ws, "src.md", "body")
	seedLinks(tool, tenantID.String(), src.ID, links)

	res := tool.Execute(makeCtx(tenantID, agentID), map[string]any{"doc_id": src.ID})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "…[5 more links omitted]") {
		t.Fatalf("expected overflow marker for 25→20 cap: %s", res.ForLLM)
	}
	// Count rendered link lines (prefix "- T").
	n := strings.Count(res.ForLLM, "\n- T")
	if n != 20 {
		t.Fatalf("expected 20 kept links, got %d", n)
	}
}

// --- Case 9: zero valid links → no heading at all. ---
func TestVaultReadOutlinks_ZeroValid_NoHeading(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	src := sharedDoc(tenantID, "Source", "src.md")
	tool, ws := newVaultReadTestTool(t, src)
	writeFile(t, ws, "src.md", "body")
	// no links seeded.

	res := tool.Execute(makeCtx(tenantID, agentID), map[string]any{"doc_id": src.ID})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if strings.Contains(res.ForLLM, "## Links") {
		t.Fatalf("should not emit empty heading: %s", res.ForLLM)
	}
}

// --- Case 10: dedup same target via wikilink + reference → shown once. ---
func TestVaultReadOutlinks_Dedup_ByTarget(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	src := sharedDoc(tenantID, "Source", "src.md")
	tgt := sharedDoc(tenantID, "DupTarget", "dup.md")
	tool, ws := newVaultReadTestTool(t, src, tgt)
	writeFile(t, ws, "src.md", "body")
	seedLinks(tool, tenantID.String(), src.ID, []store.VaultLink{
		{ToDocID: tgt.ID, LinkType: "wikilink"},
		{ToDocID: tgt.ID, LinkType: "reference"},
	})

	res := tool.Execute(makeCtx(tenantID, agentID), map[string]any{"doc_id": src.ID})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if n := strings.Count(res.ForLLM, "DupTarget"); n != 1 {
		t.Fatalf("expected 1 occurrence of DupTarget, got %d: %s", n, res.ForLLM)
	}
	// First link_type wins.
	if !strings.Contains(res.ForLLM, "(wikilink)") {
		t.Fatalf("expected first link_type wikilink to win: %s", res.ForLLM)
	}
}

// --- Case: GetOutLinks error → read succeeds, footer empty. ---
func TestVaultReadOutlinks_StoreError_NoFooter(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	src := sharedDoc(tenantID, "Source", "src.md")
	tool, ws := newVaultReadTestTool(t, src)
	writeFile(t, ws, "src.md", "body")
	tool.vaultStore.(*fakeVaultStore).outLinkErr = os.ErrInvalid

	res := tool.Execute(makeCtx(tenantID, agentID), map[string]any{"doc_id": src.ID})
	if res.IsError {
		t.Fatalf("store error must not fail read: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "body") {
		t.Fatalf("body missing: %s", res.ForLLM)
	}
	if strings.Contains(res.ForLLM, "## Links") {
		t.Fatalf("footer must be absent on store error: %s", res.ForLLM)
	}
}

// --- 14. max_bytes clamp to ceiling (1MB). ---
func TestVaultRead_MaxBytes_ClampCeiling(t *testing.T) {
	tenantID := uuid.New()
	agentID := uuid.New()
	docID := uuid.New()
	doc := &store.VaultDocument{
		ID: docID.String(), TenantID: tenantID.String(),
		Scope: "shared", Path: "c.md", Title: "C", DocType: "note",
	}
	tool, ws := newVaultReadTestTool(t, doc)
	// 1.2MB body — should be truncated to 1MB hard ceiling regardless of arg.
	body := bytes.Repeat([]byte("x"), 1_200_000)
	writeBytes(t, ws, "c.md", body)

	res := tool.Execute(makeCtx(tenantID, agentID), map[string]any{
		"doc_id":    docID.String(),
		"max_bytes": float64(5_000_000), // above ceiling
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "truncated") {
		t.Fatalf("expected truncation marker for ceiling clamp, got head: %s", res.ForLLM[:min(200, len(res.ForLLM))])
	}
}
