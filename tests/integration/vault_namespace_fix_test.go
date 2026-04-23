//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/internal/vault"
)

// TestVaultNamespaceFix_thuyTienScenario reproduces the thuy-tien bug:
// a vault doc and a KG entity share the same basename (KG_03_...). Prior to
// the fix, vault_search(types="context") returned the KG entity's id and the
// LLM passed it to vault_read, yielding "document not found".
//
// Validates:
//   A. types="context" → results contain only vault source (no kg leak).
//   B. empty types   → results contain both vault + kg sources with hint markers.
//   C. vault_read(KG id) → redirect error mentioning knowledge_graph_search.
//   D. vault_read(vault id) → success with content.
//   E. vault_read(random) → "document not found" (truly missing).
func TestVaultNamespaceFix_thuyTienScenario(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	vs := newVaultStore(db)
	kg := newKGStore(t)

	ws := t.TempDir()
	relPath := "KG_03_Danh_Muc_San_Pham.md"
	body := "Product catalog body"
	if err := os.WriteFile(filepath.Join(ws, relPath), []byte(body), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	userID := "kguser-" + agentID.String()[:8]
	ctx := store.WithUserID(store.WithAgentID(tenantCtx(tenantID), agentID), userID)

	// Seed vault doc.
	vdoc := makeSharedVaultDoc(tenantID.String(), relPath, "KG_03 Danh Muc San Pham")
	vdoc.DocType = "context"
	if err := vs.UpsertDocument(ctx, vdoc); err != nil {
		t.Fatalf("UpsertDocument: %v", err)
	}

	// Seed KG entity with same name.
	ent := &store.Entity{
		AgentID:    agentID.String(),
		UserID:     userID,
		ExternalID: "ext-kg03",
		Name:       "KG_03_Danh_Muc_San_Pham",
		EntityType: "document",
		Confidence: 0.9,
	}
	if err := kg.UpsertEntity(ctx, ent); err != nil {
		t.Fatalf("UpsertEntity: %v", err)
	}
	// Resolve the DB-assigned id.
	ents, err := kg.ListEntities(ctx, agentID.String(), userID, store.EntityListOptions{Limit: 10})
	if err != nil || len(ents) == 0 {
		t.Fatalf("ListEntities: %v (n=%d)", err, len(ents))
	}
	kgID := ents[0].ID

	// Build the search service the same way production wires it.
	svc := vault.NewVaultSearchService(vs, nil, kg)
	searchTool := tools.NewVaultSearchTool()
	searchTool.SetSearchService(svc)

	// vault_read mirrors production wiring with the namespace-fallback stores.
	readTool := tools.NewVaultReadTool()
	readTool.SetVaultStore(vs)
	readTool.SetKGStore(kg)
	readTool.SetWorkspace(ws)

	// --- Scenario A: types="context" → vault source only. ---
	res := searchTool.Execute(ctx, map[string]any{
		"query":      "KG_03",
		"types":      "context",
		"maxResults": float64(5),
	})
	if res.IsError {
		t.Fatalf("A: unexpected error: %s", res.ForLLM)
	}
	if strings.Contains(res.ForLLM, "[kg]") {
		t.Errorf("A: KG leaked into types=context search: %s", res.ForLLM)
	}

	// --- Scenario B: empty types → both sources present with per-source id fields. ---
	resB := searchTool.Execute(ctx, map[string]any{
		"query":      "KG_03",
		"maxResults": float64(10),
	})
	if resB.IsError {
		t.Fatalf("B: unexpected error: %s", resB.ForLLM)
	}
	// Each source must carry its tool-specific id field (doc_id / entity_id)
	// so the LLM cannot pattern-match a foreign uuid into vault_read.
	if !strings.Contains(resB.ForLLM, "doc_id:") {
		t.Errorf("B: vault result missing doc_id field: %s", resB.ForLLM)
	}
	if !strings.Contains(resB.ForLLM, "entity_id:") {
		t.Errorf("B: kg result missing entity_id field: %s", resB.ForLLM)
	}

	// --- Scenario C: vault_read(KG id) → redirect, not 'document not found'. ---
	resC := readTool.Execute(ctx, map[string]any{"doc_id": kgID})
	if !resC.IsError {
		t.Fatalf("C: expected error, got: %s", resC.ForLLM)
	}
	if strings.Contains(resC.ForLLM, "document not found") {
		t.Errorf("C: should redirect, not generic not-found: %s", resC.ForLLM)
	}
	if !strings.Contains(resC.ForLLM, "knowledge_graph") {
		t.Errorf("C: redirect must mention knowledge_graph: %s", resC.ForLLM)
	}

	// --- Scenario D: vault_read(vault id) → success. ---
	resD := readTool.Execute(ctx, map[string]any{"doc_id": vdoc.ID})
	if resD.IsError {
		t.Fatalf("D: unexpected error: %s", resD.ForLLM)
	}
	if !strings.Contains(resD.ForLLM, body) {
		t.Errorf("D: content missing: %s", resD.ForLLM)
	}

	// --- Scenario E: random UUID → truly not found. ---
	resE := readTool.Execute(ctx, map[string]any{"doc_id": uuid.New().String()})
	if !resE.IsError || !strings.Contains(resE.ForLLM, "not found") {
		t.Errorf("E: expected 'not found', got: %s", resE.ForLLM)
	}
}
