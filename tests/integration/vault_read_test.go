//go:build integration

package integration

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// TestVaultRead_SharedAtWorkspaceRoot reproduces issue #948:
// A file dropped at the tenant workspace root is registered with scope="shared".
// read_file refuses it (path outside agent canonical workspace); vault_read must
// return its full content when queried by doc_id.
func TestVaultRead_SharedAtWorkspaceRoot(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	vs := newVaultStore(db)

	ws := t.TempDir()
	body := strings.Repeat("KG_03 body line\n", 300) // ~4.8KB
	relPath := "KG_03_Test.md"
	if err := os.WriteFile(filepath.Join(ws, relPath), []byte(body), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Register doc directly (avoids spinning up the full rescan pipeline).
	doc := makeSharedVaultDoc(tenantID.String(), relPath, "KG_03 Test")
	ctx := tenantCtx(tenantID)
	ctx = store.WithAgentID(ctx, agentID)
	if err := vs.UpsertDocument(ctx, doc); err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}
	if doc.ID == "" {
		t.Fatal("expected doc.ID after upsert")
	}

	tool := tools.NewVaultReadTool()
	tool.SetVaultStore(vs)
	tool.SetWorkspace(ws)

	res := tool.Execute(ctx, map[string]any{"doc_id": doc.ID})
	if res.IsError {
		t.Fatalf("vault_read unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "KG_03 body line") {
		t.Fatalf("content missing in response (len=%d)", len(res.ForLLM))
	}
	if !strings.Contains(res.ForLLM, "KG_03 Test") {
		t.Fatalf("title missing in response header")
	}
}

// TestVaultRead_PersonalCrossAgentDenied — agent B must NOT be able to read
// agent A's personal doc even within the same tenant.
func TestVaultRead_PersonalCrossAgentDenied(t *testing.T) {
	db := testDB(t)
	tenantID, agentA := seedTenantAgent(t, db)
	_, agentB := seedTenantAgent(t, db)
	vs := newVaultStore(db)

	ws := t.TempDir()
	relPath := "private.md"
	if err := os.WriteFile(filepath.Join(ws, relPath), []byte("secret"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	doc := makeVaultDoc(tenantID.String(), agentA.String(), relPath, "Private")
	ctxA := store.WithAgentID(tenantCtx(tenantID), agentA)
	if err := vs.UpsertDocument(ctxA, doc); err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}

	tool := tools.NewVaultReadTool()
	tool.SetVaultStore(vs)
	tool.SetWorkspace(ws)

	// Agent B reads → must be denied.
	ctxB := store.WithAgentID(tenantCtx(tenantID), agentB)
	res := tool.Execute(ctxB, map[string]any{"doc_id": doc.ID})
	if !res.IsError {
		t.Fatalf("expected access denied for cross-agent personal read, got content")
	}
	if !strings.Contains(res.ForLLM, "not accessible") {
		t.Fatalf("expected 'not accessible' error, got: %s", res.ForLLM)
	}
}

// TestVaultRead_MediaRejected — doc with DocType=media must be rejected even
// when the caller has scope access.
func TestVaultRead_MediaRejected(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	vs := newVaultStore(db)

	ws := t.TempDir()
	relPath := "pic.png"
	if err := os.WriteFile(filepath.Join(ws, relPath), []byte("pretend-png"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	aid := agentID.String()
	doc := &store.VaultDocument{
		TenantID:    tenantID.String(),
		AgentID:     &aid,
		Scope:       "personal",
		Path:        relPath,
		Title:       "Pic",
		DocType:     "media",
		ContentHash: "abc",
		Metadata:    map[string]any{"mime_type": "image/png"},
	}
	ctx := store.WithAgentID(tenantCtx(tenantID), agentID)
	if err := vs.UpsertDocument(ctx, doc); err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}

	tool := tools.NewVaultReadTool()
	tool.SetVaultStore(vs)
	tool.SetWorkspace(ws)

	res := tool.Execute(ctx, map[string]any{"doc_id": doc.ID})
	if !res.IsError {
		t.Fatalf("expected media rejection, got content")
	}
	if !strings.Contains(res.ForLLM, "read_image") {
		t.Fatalf("expected media-handler hint, got: %s", res.ForLLM)
	}
}

// TestVaultRead_OversizeTruncation — a >max_bytes file is truncated with marker.
func TestVaultRead_OversizeTruncation(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	vs := newVaultStore(db)

	ws := t.TempDir()
	relPath := "big.md"
	big := bytes.Repeat([]byte("a"), 300_000)
	if err := os.WriteFile(filepath.Join(ws, relPath), big, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	doc := makeSharedVaultDoc(tenantID.String(), relPath, "Big Doc")
	ctx := store.WithAgentID(tenantCtx(tenantID), agentID)
	if err := vs.UpsertDocument(ctx, doc); err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}

	tool := tools.NewVaultReadTool()
	tool.SetVaultStore(vs)
	tool.SetWorkspace(ws)

	maxBytes := 100_000
	res := tool.Execute(ctx, map[string]any{
		"doc_id":    doc.ID,
		"max_bytes": float64(maxBytes),
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "truncated") {
		t.Fatalf("expected truncation marker, got none")
	}
	// Sanity: body length roughly bounded by maxBytes + header + marker (<200 chars overhead).
	if bodyLen := len(res.ForLLM); bodyLen < maxBytes-100 || bodyLen > maxBytes+500 {
		t.Fatalf("unexpected output length: %d (expected ~%d)", bodyLen, maxBytes)
	}
	// Issue #948 regression guard: identifier in test name helps future greps.
	_ = uuid.Nil
}

// TestVaultRead_OutlinksScopeMatrix verifies the inline ## Links footer honours
// the scope matrix end-to-end with a real VaultStore:
//   - shared target          → included
//   - personal target (self) → included
//   - personal target (other)→ dropped
//   - team target (other)    → dropped
//
// Uses wikilink type so the type-filter (wikilink+reference only) lets all
// test links through; the scope matrix is what we are exercising.
func TestVaultRead_OutlinksScopeMatrix(t *testing.T) {
	db := testDB(t)
	tenantID, agentSelf := seedTenantAgent(t, db)
	_, agentOther := seedTenantAgent(t, db)
	vs := newVaultStore(db)

	ws := t.TempDir()
	tid := tenantID.String()
	ctxSelf := store.WithAgentID(tenantCtx(tenantID), agentSelf)

	// Source file on disk + doc (shared so read is allowed).
	srcRel := "src.md"
	if err := os.WriteFile(filepath.Join(ws, srcRel), []byte("src body"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	src := makeSharedVaultDoc(tid, srcRel, "Source")
	if err := vs.UpsertDocument(ctxSelf, src); err != nil {
		t.Fatalf("Upsert src: %v", err)
	}

	// Targets.
	// Seed a real team belonging to a DIFFERENT agent so the "team-other"
	// target satisfies FK and is scope-denied from agentSelf's RunContext.
	otherTeamID, _ := seedTeam(t, db, tenantID, agentOther)
	shared := makeSharedVaultDoc(tid, "shared.md", "SharedTgt")
	personalSelf := makeVaultDoc(tid, agentSelf.String(), "self.md", "SelfTgt")
	personalOther := makeVaultDoc(tid, agentOther.String(), "other.md", "PersonalOtherTgt")
	teamOther := makeTeamVaultDoc(tid, otherTeamID.String(), "team.md", "TeamOtherTgt")
	for _, d := range []*store.VaultDocument{shared, personalSelf, personalOther, teamOther} {
		if err := vs.UpsertDocument(ctxSelf, d); err != nil {
			t.Fatalf("Upsert target %s: %v", d.Title, err)
		}
	}

	// Wikilinks src → each target.
	for _, toID := range []string{shared.ID, personalSelf.ID, personalOther.ID, teamOther.ID} {
		if err := vs.CreateLink(ctxSelf, &store.VaultLink{
			FromDocID: src.ID,
			ToDocID:   toID,
			LinkType:  "wikilink",
		}); err != nil {
			t.Fatalf("CreateLink → %s: %v", toID, err)
		}
	}

	tool := tools.NewVaultReadTool()
	tool.SetVaultStore(vs)
	tool.SetWorkspace(ws)

	res := tool.Execute(ctxSelf, map[string]any{"doc_id": src.ID})
	if res.IsError {
		t.Fatalf("vault_read error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "## Links") {
		t.Fatalf("missing Links section: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "SharedTgt") {
		t.Fatalf("expected shared target in footer: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "SelfTgt") {
		t.Fatalf("expected personal-self target in footer: %s", res.ForLLM)
	}
	if strings.Contains(res.ForLLM, "PersonalOtherTgt") {
		t.Fatalf("personal-other leaked: %s", res.ForLLM)
	}
	if strings.Contains(res.ForLLM, "TeamOtherTgt") {
		t.Fatalf("team-other leaked: %s", res.ForLLM)
	}
}
