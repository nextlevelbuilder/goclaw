//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestSQLiteArtifactStoreCreateGetRoundtrip(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	as := NewSQLiteArtifactStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	seedSQLiteRunTimelineTenant(t, db, store.MasterTenantID)

	art := store.Artifact{
		RunID:       "run-1",
		AuthorAgent: "agent-ws-b",
		Type:        store.ArtifactTypePlan,
		Status:      store.ArtifactStatusDraft,
		Title:       "native layer plan",
		Content:     "phase 3 plan body",
	}
	if err := as.CreateArtifact(ctx, &art); err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}
	if art.ID == uuid.Nil {
		t.Fatal("CreateArtifact did not assign ID")
	}
	if art.Version != 1 {
		t.Fatalf("version = %d, want 1", art.Version)
	}
	if art.Checksum != store.ArtifactChecksum("phase 3 plan body") {
		t.Fatalf("checksum = %q, want computed sha256", art.Checksum)
	}

	got, err := as.GetArtifact(ctx, art.ID)
	if err != nil {
		t.Fatalf("GetArtifact: %v", err)
	}
	if got.Type != store.ArtifactTypePlan || got.Title != "native layer plan" {
		t.Fatalf("got = %+v, want plan + title", got)
	}
	if got.Content != "phase 3 plan body" {
		t.Fatalf("content = %q", got.Content)
	}
	if got.Checksum != art.Checksum {
		t.Fatalf("checksum roundtrip = %q, want %q", got.Checksum, art.Checksum)
	}
}

func TestSQLiteArtifactStoreVersionChain(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	as := NewSQLiteArtifactStore(db)
	tenantID := uuid.Must(uuid.NewV7())
	ctx := store.WithTenantID(context.Background(), tenantID)
	seedSQLiteRunTimelineTenant(t, db, tenantID)

	root := store.Artifact{
		AuthorAgent: "a1", Type: store.ArtifactTypeArchitecture,
		Title: "arch v1", Content: "design v1",
	}
	if err := as.CreateArtifact(ctx, &root); err != nil {
		t.Fatalf("CreateArtifact root: %v", err)
	}
	if root.Version != 1 {
		t.Fatalf("root version = %d, want 1", root.Version)
	}

	child := store.Artifact{
		AuthorAgent: "a2", Type: store.ArtifactTypeArchitecture,
		ParentID: &root.ID, Title: "arch v2", Content: "design v2",
	}
	if err := as.CreateArtifact(ctx, &child); err != nil {
		t.Fatalf("CreateArtifact child: %v", err)
	}
	if child.Version != 2 {
		t.Fatalf("child version = %d, want 2", child.Version)
	}

	chain, err := as.GetVersionChain(ctx, tenantID, &root.ID)
	if err != nil {
		t.Fatalf("GetVersionChain children: %v", err)
	}
	if len(chain) != 1 || chain[0].ID != child.ID || chain[0].Version != 2 {
		t.Fatalf("children = %+v, want single child v2", chain)
	}

	roots, err := as.GetVersionChain(ctx, tenantID, nil)
	if err != nil {
		t.Fatalf("GetVersionChain roots: %v", err)
	}
	if len(roots) != 1 || roots[0].ID != root.ID {
		t.Fatalf("roots = %+v, want single root", roots)
	}
}

func TestSQLiteArtifactStoreListFilters(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	as := NewSQLiteArtifactStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	seedSQLiteRunTimelineTenant(t, db, store.MasterTenantID)

	plan := store.Artifact{RunID: "run-a", Type: store.ArtifactTypePlan, Title: "p1", Content: "p1"}
	code := store.Artifact{RunID: "run-a", Type: store.ArtifactTypeCode, Title: "c1", Content: "c1"}
	report := store.Artifact{RunID: "run-b", Type: store.ArtifactTypeReport, Title: "r1", Content: "r1"}
	for _, a := range []store.Artifact{plan, code, report} {
		if err := as.CreateArtifact(ctx, &a); err != nil {
			t.Fatalf("CreateArtifact(%s): %v", a.Title, err)
		}
	}

	byRun, err := as.ListArtifacts(ctx, store.ArtifactListOpts{RunID: "run-a", Limit: 10})
	if err != nil {
		t.Fatalf("ListArtifacts by run: %v", err)
	}
	if len(byRun) != 2 {
		t.Fatalf("run-a len = %d, want 2", len(byRun))
	}

	byType, err := as.ListArtifacts(ctx, store.ArtifactListOpts{Type: store.ArtifactTypeCode, Limit: 10})
	if err != nil {
		t.Fatalf("ListArtifacts by type: %v", err)
	}
	if len(byType) != 1 || byType[0].Title != "c1" {
		t.Fatalf("byType = %+v, want single c1", byType)
	}

	byStatus, err := as.ListArtifacts(ctx, store.ArtifactListOpts{Status: store.ArtifactStatusDraft, Limit: 10})
	if err != nil {
		t.Fatalf("ListArtifacts by status: %v", err)
	}
	if len(byStatus) != 3 {
		t.Fatalf("byStatus len = %d, want 3 (default draft)", len(byStatus))
	}
}

func TestSQLiteArtifactStoreMarkStatus(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	as := NewSQLiteArtifactStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	seedSQLiteRunTimelineTenant(t, db, store.MasterTenantID)

	art := store.Artifact{Type: store.ArtifactTypeADR, Title: "adr-1", Content: "adr body"}
	if err := as.CreateArtifact(ctx, &art); err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}

	if err := as.MarkArtifactStatus(ctx, art.ID, store.ArtifactStatusFinal); err != nil {
		t.Fatalf("MarkArtifactStatus: %v", err)
	}
	got, err := as.GetArtifact(ctx, art.ID)
	if err != nil {
		t.Fatalf("GetArtifact: %v", err)
	}
	if got.Status != store.ArtifactStatusFinal {
		t.Fatalf("status = %q, want final", got.Status)
	}

	if err := as.MarkArtifactStatus(ctx, art.ID, "not-a-status"); err == nil {
		t.Fatal("MarkArtifactStatus with invalid status succeeded, want error")
	}
}

func TestSQLiteArtifactStoreTenantScope(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	as := NewSQLiteArtifactStore(db)
	tenantA := uuid.Must(uuid.NewV7())
	tenantB := uuid.Must(uuid.NewV7())
	seedSQLiteRunTimelineTenant(t, db, tenantA)
	seedSQLiteRunTimelineTenant(t, db, tenantB)
	ctxA := store.WithTenantID(context.Background(), tenantA)
	ctxB := store.WithTenantID(context.Background(), tenantB)

	art := store.Artifact{Type: store.ArtifactTypeResearch, Title: "shared", Content: "visibility"}
	if err := as.CreateArtifact(ctxA, &art); err != nil {
		t.Fatalf("CreateArtifact tenant A: %v", err)
	}

	gotB, err := as.GetArtifact(ctxB, art.ID)
	if err == nil {
		t.Fatalf("tenant B GetArtifact succeeded with %+v, want error", gotB)
	}

	gotNoTenant, err := as.GetArtifact(context.Background(), art.ID)
	if err == nil {
		t.Fatalf("no-tenant GetArtifact succeeded with %+v, want fail-closed error", gotNoTenant)
	}

	listB, err := as.ListArtifacts(ctxB, store.ArtifactListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListArtifacts tenant B: %v", err)
	}
	if len(listB) != 0 {
		t.Fatalf("tenant B list len = %d, want 0", len(listB))
	}
}