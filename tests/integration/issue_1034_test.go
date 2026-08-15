//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	httppkg "github.com/nextlevelbuilder/goclaw/internal/http"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// TestIssue1034_Bug1_VerifyEmptyBody — after phase 03, empty body triggers
// ping mode. Without a real providerReg the handler returns 200 with
// {valid:false, error:"no provider registry available"} or
// {valid:false, error:"provider not registered: ..."} — the key contract
// is that empty body NO LONGER returns 400.
func TestIssue1034_Bug1_VerifyEmptyBody(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)

	pstore := pg.NewPGProviderStore(db, "")
	suffix := uuid.NewString()[:8]
	p := &store.LLMProviderData{
		Name:         "openai-compat-bug1-" + suffix,
		DisplayName:  "Bug1 test",
		ProviderType: "openai-compat",
		APIBase:      "http://127.0.0.1:0",
		APIKey:       "x",
		Enabled:      true,
	}
	ctx := tenantCtx(tenantID)
	if err := pstore.CreateProvider(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	t.Cleanup(func() {
		_ = pstore.DeleteProvider(tenantCtx(tenantID), p.ID)
	})

	h := httppkg.NewProvidersHandler(pstore, nil, nil, "")

	req := httptest.NewRequest("POST", "/v1/providers/"+p.ID.String()+"/verify", nil).WithContext(ctx)
	req.SetPathValue("id", p.ID.String())
	w := httptest.NewRecorder()
	h.HandleVerifyProviderForTest(w, req)

	if w.Code != 200 {
		t.Fatalf("ping-mode (empty body) must return 200, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestIssue1034_Bug1_VerifyEmptyBodyACPProvider — ping mode on an ACP provider
// returns {valid:true} without any binary check (no providerReg lookup).
func TestIssue1034_Bug1_VerifyEmptyBodyACPProvider(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)

	pstore := pg.NewPGProviderStore(db, "")
	suffix := uuid.NewString()[:8]
	p := &store.LLMProviderData{
		Name:         "acp-bug1-" + suffix,
		ProviderType: store.ProviderACP,
		APIBase:      "claude",
		Enabled:      true,
	}
	ctx := tenantCtx(tenantID)
	if err := pstore.CreateProvider(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	t.Cleanup(func() {
		_ = pstore.DeleteProvider(tenantCtx(tenantID), p.ID)
	})

	h := httppkg.NewProvidersHandler(pstore, nil, nil, "")

	req := httptest.NewRequest("POST", "/v1/providers/"+p.ID.String()+"/verify", nil).WithContext(ctx)
	req.SetPathValue("id", p.ID.String())
	w := httptest.NewRecorder()
	h.HandleVerifyProviderForTest(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if v, _ := body["valid"].(bool); !v {
		t.Fatalf("ACP ping must return valid=true, got body=%s", w.Body.String())
	}
}

// TestIssue1034_Bug1_VerifyMalformedBody — locks in that malformed JSON
// (truncated/invalid) keeps returning 400 even after the ping-mode change.
func TestIssue1034_Bug1_VerifyMalformedBody(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)

	pstore := pg.NewPGProviderStore(db, "")
	suffix := uuid.NewString()[:8]
	p := &store.LLMProviderData{
		Name:         "openai-compat-bug1m-" + suffix,
		ProviderType: "openai-compat",
		APIBase:      "http://127.0.0.1:0",
		APIKey:       "x",
		Enabled:      true,
	}
	ctx := tenantCtx(tenantID)
	if err := pstore.CreateProvider(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	t.Cleanup(func() {
		_ = pstore.DeleteProvider(tenantCtx(tenantID), p.ID)
	})

	h := httppkg.NewProvidersHandler(pstore, nil, nil, "")

	body := bytes.NewBufferString(`{"model":`) // truncated
	req := httptest.NewRequest("POST", "/v1/providers/"+p.ID.String()+"/verify", body).WithContext(ctx)
	req.SetPathValue("id", p.ID.String())
	w := httptest.NewRecorder()
	h.HandleVerifyProviderForTest(w, req)

	if w.Code != 400 {
		t.Fatalf("malformed body must stay 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestIssue1034_Bug2_DeleteProviderWithHeartbeat — after phase 02 the FK is
// ON DELETE SET NULL and DeleteProvider runs in a tx that also disables the
// stale heartbeat. Asserts: delete succeeds, provider_id IS NULL, enabled = false.
func TestIssue1034_Bug2_DeleteProviderWithHeartbeat(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)

	pstore := pg.NewPGProviderStore(db, "")
	suffix := uuid.NewString()[:8]
	p := &store.LLMProviderData{
		Name:         "openai-compat-bug2-" + suffix,
		ProviderType: "openai-compat",
		APIBase:      "http://127.0.0.1:0",
		APIKey:       "x",
		Enabled:      true,
	}
	ctx := tenantCtx(tenantID)
	if err := pstore.CreateProvider(ctx, p); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	hbID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO agent_heartbeats (id, agent_id, enabled, provider_id, model)
		 VALUES ($1, $2, true, $3, 'gpt-4')`,
		hbID, agentID, p.ID,
	); err != nil {
		t.Fatalf("seed heartbeat: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM agent_heartbeats WHERE agent_id = $1`, agentID)
		_, _ = db.Exec(`DELETE FROM llm_providers WHERE id = $1`, p.ID)
	})

	if err := pstore.DeleteProvider(ctx, p.ID); err != nil {
		t.Fatalf("delete provider should succeed after FK SET NULL + tx, got err=%v", err)
	}

	var providerID *string
	var enabled bool
	if err := db.QueryRow(
		`SELECT provider_id::text, enabled FROM agent_heartbeats WHERE id = $1`,
		hbID,
	).Scan(&providerID, &enabled); err != nil {
		t.Fatalf("scan heartbeat: %v", err)
	}
	if providerID != nil {
		t.Fatalf("expected provider_id NULL after delete, got %q", *providerID)
	}
	if enabled {
		t.Fatalf("expected heartbeat disabled after delete, got enabled=true")
	}
}

// TestIssue1034_Bug2_DeleteProviderCrossTenantIsolation — guarantees that
// even if tenant A possesses tenant B's provider UUID, calling DeleteProvider
// (without master scope) does NOT disable tenant B's heartbeats. Regression
// guard for the cross-tenant UPDATE concern raised in code review.
func TestIssue1034_Bug2_DeleteProviderCrossTenantIsolation(t *testing.T) {
	db := testDB(t)
	tenantA, _ := seedTenantAgent(t, db)
	tenantB, agentB := seedTenantAgent(t, db)

	pstore := pg.NewPGProviderStore(db, "")
	suffix := uuid.NewString()[:8]

	// Provider lives in tenant B.
	pB := &store.LLMProviderData{
		Name:         "openai-compat-tenantB-" + suffix,
		ProviderType: "openai-compat",
		APIBase:      "http://127.0.0.1:0",
		APIKey:       "x",
		Enabled:      true,
	}
	if err := pstore.CreateProvider(tenantCtx(tenantB), pB); err != nil {
		t.Fatalf("create provider B: %v", err)
	}
	t.Cleanup(func() {
		_ = pstore.DeleteProvider(tenantCtx(tenantB), pB.ID)
	})

	// Heartbeat in tenant B references provider B and is enabled.
	hbID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO agent_heartbeats (id, agent_id, enabled, provider_id, model)
		 VALUES ($1, $2, true, $3, 'gpt-4')`,
		hbID, agentB, pB.ID,
	); err != nil {
		t.Fatalf("seed heartbeat B: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM agent_heartbeats WHERE agent_id = $1`, agentB)
	})

	// Tenant A attempts delete with B's provider UUID. Must NOT touch B's heartbeat.
	_ = pstore.DeleteProvider(tenantCtx(tenantA), pB.ID)

	var enabled bool
	var providerID *string
	if err := db.QueryRow(
		`SELECT enabled, provider_id::text FROM agent_heartbeats WHERE id = $1`, hbID,
	).Scan(&enabled, &providerID); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !enabled {
		t.Fatalf("tenant A's DeleteProvider must NOT disable tenant B's heartbeat")
	}
	if providerID == nil || *providerID != pB.ID.String() {
		t.Fatalf("tenant B's heartbeat provider_id must be untouched, got %v", providerID)
	}
}

// TestIssue1034_Bug3_DoctorDisplayNameEmpty — after phase 05 doctor uses
// COALESCE(NULLIF(display_name, ''), name) so empty-string display_name
// falls back to the canonical name.
func TestIssue1034_Bug3_DoctorDisplayNameEmpty(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)

	pstore := pg.NewPGProviderStore(db, "")
	suffix := uuid.NewString()[:8]
	canonicalName := "openai-compat-ollama-" + suffix
	p := &store.LLMProviderData{
		Name:         canonicalName,
		DisplayName:  "", // empty (not NULL) — the bug condition
		ProviderType: "openai-compat",
		APIBase:      "http://127.0.0.1:0",
		APIKey:       "x",
		Enabled:      true,
	}
	if err := pstore.CreateProvider(tenantCtx(tenantID), p); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	t.Cleanup(func() {
		_ = pstore.DeleteProvider(tenantCtx(tenantID), p.ID)
	})

	// Mirror the fixed query in cmd/doctor.go.
	var name, displayName string
	row := db.QueryRow(
		`SELECT name, COALESCE(NULLIF(display_name, ''), name) FROM llm_providers WHERE id = $1`,
		p.ID,
	)
	if err := row.Scan(&name, &displayName); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if displayName != canonicalName {
		t.Fatalf("NULLIF-fixed query must fall back to name=%q, got %q", canonicalName, displayName)
	}

	// Non-empty display_name still wins.
	if _, err := db.Exec(
		`UPDATE llm_providers SET display_name = 'My Ollama' WHERE id = $1`, p.ID,
	); err != nil {
		t.Fatalf("update display_name: %v", err)
	}
	if err := db.QueryRow(
		`SELECT COALESCE(NULLIF(display_name, ''), name) FROM llm_providers WHERE id = $1`, p.ID,
	).Scan(&displayName); err != nil {
		t.Fatalf("scan2: %v", err)
	}
	if displayName != "My Ollama" {
		t.Fatalf("non-empty display_name must win, got %q", displayName)
	}
}
