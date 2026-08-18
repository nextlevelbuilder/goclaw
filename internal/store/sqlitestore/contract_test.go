//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestSQLiteContractStoreCreateGetRoundtrip(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	cs := NewSQLiteContractStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	seedSQLiteRunTimelineTenant(t, db, store.MasterTenantID)

	rec := store.ContractRecord{
		RunID: "contract-run-1",
		Kind:  store.ContractRecordHandoff,
		Body:  `{"task":"hand off design","verdicts":[]}`,
	}
	if err := cs.CreateContractRecord(ctx, &rec); err != nil {
		t.Fatalf("CreateContractRecord: %v", err)
	}
	if rec.ID == uuid.Nil {
		t.Fatal("CreateContractRecord did not assign ID")
	}
	if rec.Status != store.ContractRecordDraft {
		t.Fatalf("status = %q, want default draft", rec.Status)
	}
	if rec.TenantID != store.MasterTenantID {
		t.Fatalf("tenant_id = %v, want master %v", rec.TenantID, store.MasterTenantID)
	}

	got, err := cs.GetContractRecord(ctx, rec.ID)
	if err != nil {
		t.Fatalf("GetContractRecord: %v", err)
	}
	if got.Kind != store.ContractRecordHandoff || got.RunID != "contract-run-1" {
		t.Fatalf("got = %+v, want handoff + run id", got)
	}
	if got.Body != rec.Body {
		t.Fatalf("body roundtrip = %q, want %q", got.Body, rec.Body)
	}
	if got.Status != store.ContractRecordDraft {
		t.Fatalf("status roundtrip = %q, want draft", got.Status)
	}
}

func TestSQLiteContractStoreListFilters(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	cs := NewSQLiteContractStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	seedSQLiteRunTimelineTenant(t, db, store.MasterTenantID)

	handoff := store.ContractRecord{RunID: "run-a", Kind: store.ContractRecordHandoff, Body: `{"n":1}`}
	jury := store.ContractRecord{RunID: "run-a", Kind: store.ContractRecordJury, Body: `{"n":2}`}
	closed := store.ContractRecord{RunID: "run-b", Kind: store.ContractRecordNegotiation, Body: `{"n":3}`}
	recs := []store.ContractRecord{handoff, jury, closed}
	for i := range recs {
		if err := cs.CreateContractRecord(ctx, &recs[i]); err != nil {
			t.Fatalf("CreateContractRecord(%s): %v", recs[i].Kind, err)
		}
	}
	if err := cs.UpdateContractRecordStatus(ctx, recs[2].ID, store.ContractRecordClosed); err != nil {
		t.Fatalf("UpdateContractRecordStatus closed: %v", err)
	}

	byRun, err := cs.ListContractRecords(ctx, store.ContractRecordListOpts{RunID: "run-a", Limit: 10})
	if err != nil {
		t.Fatalf("ListContractRecords by run: %v", err)
	}
	if len(byRun) != 2 {
		t.Fatalf("run-a len = %d, want 2", len(byRun))
	}

	byKind, err := cs.ListContractRecords(ctx, store.ContractRecordListOpts{Kind: store.ContractRecordJury, Limit: 10})
	if err != nil {
		t.Fatalf("ListContractRecords by kind: %v", err)
	}
	if len(byKind) != 1 || byKind[0].Kind != store.ContractRecordJury {
		t.Fatalf("byKind = %+v, want single jury", byKind)
	}

	byStatus, err := cs.ListContractRecords(ctx, store.ContractRecordListOpts{Status: store.ContractRecordClosed, Limit: 10})
	if err != nil {
		t.Fatalf("ListContractRecords by status: %v", err)
	}
	if len(byStatus) != 1 || byStatus[0].ID != recs[2].ID {
		t.Fatalf("byStatus = %+v, want closed negotiation", byStatus)
	}
}

func TestSQLiteContractStoreUpdateStatus(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	cs := NewSQLiteContractStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	seedSQLiteRunTimelineTenant(t, db, store.MasterTenantID)

	rec := store.ContractRecord{Kind: store.ContractRecordCompetition, Body: `{"rounds":3}`}
	if err := cs.CreateContractRecord(ctx, &rec); err != nil {
		t.Fatalf("CreateContractRecord: %v", err)
	}

	if err := cs.UpdateContractRecordStatus(ctx, rec.ID, store.ContractRecordActive); err != nil {
		t.Fatalf("UpdateContractRecordStatus: %v", err)
	}
	got, err := cs.GetContractRecord(ctx, rec.ID)
	if err != nil {
		t.Fatalf("GetContractRecord: %v", err)
	}
	if got.Status != store.ContractRecordActive {
		t.Fatalf("status = %q, want active", got.Status)
	}

	if err := cs.UpdateContractRecordStatus(ctx, rec.ID, "not-a-status"); err == nil {
		t.Fatal("UpdateContractRecordStatus with invalid status succeeded, want error")
	}
}

func TestSQLiteContractStoreTenantScope(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	cs := NewSQLiteContractStore(db)
	tenantA := uuid.Must(uuid.NewV7())
	tenantB := uuid.Must(uuid.NewV7())
	seedSQLiteRunTimelineTenant(t, db, tenantA)
	seedSQLiteRunTimelineTenant(t, db, tenantB)
	ctxA := store.WithTenantID(context.Background(), tenantA)
	ctxB := store.WithTenantID(context.Background(), tenantB)

	rec := store.ContractRecord{Kind: store.ContractRecordHandoff, Body: `{"scope":"a"}`}
	if err := cs.CreateContractRecord(ctxA, &rec); err != nil {
		t.Fatalf("CreateContractRecord tenant A: %v", err)
	}

	gotB, err := cs.GetContractRecord(ctxB, rec.ID)
	if err == nil {
		t.Fatalf("tenant B GetContractRecord succeeded with %+v, want error", gotB)
	}
	gotNoTenant, err := cs.GetContractRecord(context.Background(), rec.ID)
	if err == nil {
		t.Fatalf("no-tenant GetContractRecord succeeded with %+v, want fail-closed error", gotNoTenant)
	}

	listB, err := cs.ListContractRecords(ctxB, store.ContractRecordListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListContractRecords tenant B: %v", err)
	}
	if len(listB) != 0 {
		t.Fatalf("tenant B list len = %d, want 0", len(listB))
	}
}

func TestSQLiteContractStoreBodyJSONRoundtrip(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	cs := NewSQLiteContractStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	seedSQLiteRunTimelineTenant(t, db, store.MasterTenantID)

	body := `{"kind":"jury","verdicts":[{"contender_id":"a","decision":"approve","score":0.9}],"meta":{"deep":"nested value"}}`
	rec := store.ContractRecord{
		RunID: "contract-json",
		Kind:  store.ContractRecordJury,
		Body:  body,
	}
	if err := cs.CreateContractRecord(ctx, &rec); err != nil {
		t.Fatalf("CreateContractRecord: %v", err)
	}
	got, err := cs.GetContractRecord(ctx, rec.ID)
	if err != nil {
		t.Fatalf("GetContractRecord: %v", err)
	}
	if got.Body != body {
		t.Fatalf("body JSON roundtrip = %q, want %q", got.Body, body)
	}

	// Empty body normalizes to a valid empty JSON object.
	empty := store.ContractRecord{Kind: store.ContractRecordNegotiation}
	if err := cs.CreateContractRecord(ctx, &empty); err != nil {
		t.Fatalf("CreateContractRecord empty body: %v", err)
	}
	gotEmpty, err := cs.GetContractRecord(ctx, empty.ID)
	if err != nil {
		t.Fatalf("GetContractRecord empty body: %v", err)
	}
	if gotEmpty.Body != "{}" {
		t.Fatalf("empty body roundtrip = %q, want {}", gotEmpty.Body)
	}
}