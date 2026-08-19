//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// seedApprovalTenant inserts a minimal tenant + agent row for FK satisfaction.
func seedApprovalTenant(t *testing.T, db *sql.DB) (tenantID, agentID uuid.UUID) {
	t.Helper()
	tenantID = uuid.Must(uuid.NewV7())
	agentID = uuid.Must(uuid.NewV7())
	_, err := db.Exec(
		`INSERT INTO tenants (id, name, slug, status) VALUES (?,?,?,'active')`,
		tenantID.String(), "approval-test-"+tenantID.String()[:8], "at"+tenantID.String()[:8])
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES (?,?,?,'predefined','active','test','test-model','owner')`,
		agentID.String(), tenantID.String(), "aa-"+agentID.String()[:8])
	if err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	return tenantID, agentID
}

// newApprovalTestDB opens a scratch SQLite DB with the full schema applied.
func newApprovalTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "approval_test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	if err := EnsureSchema(db); err != nil {
		db.Close()
		t.Fatalf("EnsureSchema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSQLiteApprovalStore_CRUD(t *testing.T) {
	db := newApprovalTestDB(t)
	tenantID, agentID := seedApprovalTenant(t, db)
	s := NewSQLiteApprovalStore(db)
	ctx := store.WithTenantID(context.Background(), tenantID)

	// Create
	req := &store.ApprovalRequest{
		AgentID:        &agentID,
		RequesterType:  "agent",
		ActionType:     "exec",
		Payload:        []byte(`{"command":"pip install foo"}`),
		Command:        "pip install foo",
		Status:         store.ApprovalStatusPending,
		TimeoutSeconds: 120,
	}
	if err := s.CreateRequest(ctx, req); err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	if req.ID == uuid.Nil {
		t.Fatal("CreateRequest should assign an ID")
	}
	if req.TenantID != tenantID {
		t.Fatalf("CreateRequest tenant = %s, want %s", req.TenantID, tenantID)
	}
	if req.Status != store.ApprovalStatusPending {
		t.Fatalf("CreateRequest status = %q, want pending", req.Status)
	}

	// GetByID
	got, err := s.GetByID(ctx, req.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("GetByID returned nil for existing row")
	}
	if got.Command != "pip install foo" {
		t.Fatalf("GetByID command = %q, want %q", got.Command, "pip install foo")
	}
	if got.AgentID == nil || *got.AgentID != agentID {
		t.Fatalf("GetByID agent = %v, want %v", got.AgentID, agentID)
	}

	// ListPending
	pending, err := s.ListPending(ctx, tenantID)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("ListPending count = %d, want 1", len(pending))
	}

	// Resolve → approved
	decidedBy := uuid.Must(uuid.NewV7())
	if err := s.Resolve(ctx, req.ID, store.ApprovalDecisionAllowOnce, &decidedBy, true, false); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Idempotent: second resolve must fail closed.
	err = s.Resolve(ctx, req.ID, store.ApprovalDecisionDeny, &decidedBy, false, false)
	if !errors.Is(err, store.ErrApprovalAlreadyResolved) {
		t.Fatalf("second Resolve error = %v, want ErrApprovalAlreadyResolved", err)
	}

	// History
	hist, err := s.ListHistory(ctx, tenantID, store.ApprovalListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(hist) != 1 {
		t.Fatalf("ListHistory count = %d, want 1", len(hist))
	}
	if hist[0].Decision != store.ApprovalDecisionAllowOnce {
		t.Fatalf("history decision = %q, want allow-once", hist[0].Decision)
	}
	if hist[0].DecidedBy == nil || *hist[0].DecidedBy != decidedBy {
		t.Fatalf("history decided_by = %v, want %v", hist[0].DecidedBy, decidedBy)
	}
}

func TestSQLiteApprovalStore_MarkExpired(t *testing.T) {
	db := newApprovalTestDB(t)
	tenantID, _ := seedApprovalTenant(t, db)
	s := NewSQLiteApprovalStore(db)
	ctx := store.WithTenantID(context.Background(), tenantID)

	req := &store.ApprovalRequest{
		ActionType: "exec",
		Command:    "deploy --prod",
		Status:     store.ApprovalStatusPending,
	}
	if err := s.CreateRequest(ctx, req); err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}

	if err := s.MarkExpired(ctx, req.ID); err != nil {
		t.Fatalf("MarkExpired: %v", err)
	}

	got, err := s.GetByID(ctx, req.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("GetByID returned nil")
	}
	if got.Status != store.ApprovalStatusExpired {
		t.Fatalf("status = %q, want expired", got.Status)
	}
	if got.DecidedAt == nil {
		t.Fatal("expired row should have decided_at set")
	}

	// Expired rows are not pending.
	pending, err := s.ListPending(ctx, tenantID)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("ListPending count = %d, want 0 after expire", len(pending))
	}

	// Cannot resolve an already-expired row.
	err = s.Resolve(ctx, req.ID, store.ApprovalDecisionDeny, nil, false, false)
	if !errors.Is(err, store.ErrApprovalAlreadyResolved) {
		t.Fatalf("resolve after expire error = %v, want ErrApprovalAlreadyResolved", err)
	}
}

func TestSQLiteApprovalStore_TenantIsolation(t *testing.T) {
	db := newApprovalTestDB(t)
	tenantA, _ := seedApprovalTenant(t, db)
	tenantB := uuid.Must(uuid.NewV7())
	_, err := db.Exec(
		`INSERT INTO tenants (id, name, slug, status) VALUES (?,?,?,'active')`,
		tenantB.String(), "approval-test-b-"+tenantB.String()[:8], "atb"+tenantB.String()[:8])
	if err != nil {
		t.Fatalf("seed tenant B: %v", err)
	}

	s := NewSQLiteApprovalStore(db)
	ctxA := store.WithTenantID(context.Background(), tenantA)

	req := &store.ApprovalRequest{ActionType: "exec", Command: "whoami"}
	if err := s.CreateRequest(ctxA, req); err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}

	// Tenant B cannot see tenant A's request.
	ctxB := store.WithTenantID(context.Background(), tenantB)
	got, err := s.GetByID(ctxB, req.ID)
	if err != nil {
		t.Fatalf("GetByID cross-tenant: %v", err)
	}
	if got != nil {
		t.Fatalf("tenant B read tenant A approval: %+v", got)
	}

	pending, err := s.ListPending(ctxB, tenantB)
	if err != nil {
		t.Fatalf("ListPending tenant B: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("tenant B ListPending count = %d, want 0", len(pending))
	}

	// Tenant B cannot resolve tenant A's request.
	err = s.Resolve(ctxB, req.ID, store.ApprovalDecisionDeny, nil, false, false)
	if !errors.Is(err, store.ErrApprovalAlreadyResolved) {
		t.Fatalf("tenant B resolve error = %v, want ErrApprovalAlreadyResolved", err)
	}
}