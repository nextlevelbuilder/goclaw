//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestSQLiteCLIConnectionStore exercises the full CRUD + credential surface,
// including tenant/global visibility scoping and the tenant-shared credential
// fallback (user_id = ”).
func TestSQLiteCLIConnectionStore(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "cli.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	s := NewSQLiteCLIConnectionStore(db, "0123456789abcdef0123456789abcdef")
	ctx := context.Background()
	tid := uuid.Must(uuid.NewV7())

	// tenant row
	c := &store.CLIConnection{TenantID: &tid, Name: "Claude Code", Kind: "external_cli",
		Provider: "claude_code", Mode: "delegate", Enabled: true, Config: json.RawMessage(`{"a":1}`), CreatedBy: "u1"}
	if err := s.Upsert(ctx, c); err != nil {
		t.Fatal(err)
	}
	// global row
	g := &store.CLIConnection{Name: "Codex", Kind: "external_cli", Provider: "codex", Mode: "delegate", Enabled: false}
	if err := s.Upsert(ctx, g); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListForTenant(ctx, &tid, false)
	if err != nil || len(list) != 2 {
		t.Fatalf("list all: %v %d", err, len(list))
	}
	if list[0].Name != "Claude Code" || string(list[0].Config) != `{"a":1}` || !list[0].Enabled || list[0].TenantID == nil {
		t.Fatalf("bad row: %+v", list[0])
	}
	if list[1].TenantID != nil || list[1].Enabled {
		t.Fatalf("bad global row: %+v", list[1])
	}
	list, err = s.ListForTenant(ctx, &tid, true)
	if err != nil || len(list) != 1 {
		t.Fatalf("list enabled: %v %d", err, len(list))
	}
	// nil tenant sees only globals
	list, err = s.ListForTenant(ctx, nil, false)
	if err != nil || len(list) != 1 || list[0].Name != "Codex" {
		t.Fatalf("list nil tenant: %v %+v", err, list)
	}
	// other tenant sees only globals
	other := uuid.Must(uuid.NewV7())
	list, err = s.ListForTenant(ctx, &other, false)
	if err != nil || len(list) != 1 || list[0].Name != "Codex" {
		t.Fatalf("list other tenant: %v %+v", err, list)
	}

	got, err := s.GetByID(ctx, &tid, c.ID)
	if err != nil || got == nil || got.Name != "Claude Code" {
		t.Fatalf("get: %v %+v", err, got)
	}
	if got, err = s.GetByID(ctx, &tid, g.ID); err != nil || got == nil {
		t.Fatalf("get global via tenant: %v %+v", err, got)
	}
	if got, err = s.GetByID(ctx, nil, c.ID); err != nil || got != nil {
		t.Fatalf("nil tenant must not see tenant row: %v %+v", err, got)
	}
	if got, err = s.GetByID(ctx, &other, c.ID); err != nil || got != nil {
		t.Fatalf("cross tenant leak: %v %+v", err, got)
	}

	// update
	c.Name = "Claude Code 2"
	c.Enabled = false
	if err := s.Upsert(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetByID(ctx, &tid, c.ID)
	if got.Name != "Claude Code 2" || got.Enabled || !got.UpdatedAt.After(got.CreatedAt) {
		t.Fatalf("after update: %+v", got)
	}
	// wrong-owner update must fail
	bad := *c
	bad.TenantID = &other
	if err := s.Upsert(ctx, &bad); err == nil {
		t.Fatal("expected not-found on wrong-tenant update")
	}

	// credentials: shared fallback + per-user override
	if err := s.PutCredential(ctx, store.CLIConnectionCredential{ConnectionID: c.ID,
		UserID: store.SharedCredentialUserID, TenantID: &tid, Type: "api_key", Inject: "env:K", Secret: "shared"}); err != nil {
		t.Fatal(err)
	}
	cr, err := s.GetCredential(ctx, c.ID, "user-1")
	if err != nil || cr == nil || cr.Secret != "shared" || cr.UserID != "" {
		t.Fatalf("shared fallback: %v %+v", err, cr)
	}
	if err := s.PutCredential(ctx, store.CLIConnectionCredential{ConnectionID: c.ID,
		UserID: "user-1", TenantID: &tid, Type: "oauth", Inject: "env:T", Secret: "mine"}); err != nil {
		t.Fatal(err)
	}
	cr, err = s.GetCredential(ctx, c.ID, "user-1")
	if err != nil || cr.Secret != "mine" || cr.UserID != "user-1" || cr.UpdatedAt.IsZero() || cr.TenantID == nil {
		t.Fatalf("own cred: %v %+v", err, cr)
	}
	if cr, err = s.GetCredential(ctx, c.ID, "user-2"); err != nil || cr.Secret != "shared" {
		t.Fatalf("other user fallback: %v %+v", err, cr)
	}
	// stored encrypted?
	var raw string
	if err := db.QueryRow(`SELECT secret_enc FROM cli_connection_credentials WHERE user_id='user-1'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw == "mine" {
		t.Fatal("secret stored in plaintext")
	}
	// upsert same key
	if err := s.PutCredential(ctx, store.CLIConnectionCredential{ConnectionID: c.ID,
		UserID: "user-1", TenantID: &tid, Type: "oauth", Inject: "env:T", Secret: "mine2"}); err != nil {
		t.Fatal(err)
	}
	if cr, _ = s.GetCredential(ctx, c.ID, "user-1"); cr.Secret != "mine2" {
		t.Fatalf("re-put: %+v", cr)
	}
	if err := s.DeleteCredential(ctx, c.ID, "user-1"); err != nil {
		t.Fatal(err)
	}
	if cr, _ = s.GetCredential(ctx, c.ID, "user-1"); cr == nil || cr.Secret != "shared" {
		t.Fatalf("after delete: %+v", cr)
	}
	if err := s.DeleteCredential(ctx, c.ID, store.SharedCredentialUserID); err != nil {
		t.Fatal(err)
	}
	if cr, err = s.GetCredential(ctx, c.ID, "user-1"); err != nil || cr != nil {
		t.Fatalf("no creds: %v %+v", err, cr)
	}

	// delete: wrong tenant no-op, then correct
	if err := s.Delete(ctx, &other, c.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetByID(ctx, &tid, c.ID); got == nil {
		t.Fatal("wrong-tenant delete removed row")
	}
	if err := s.Delete(ctx, &tid, c.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetByID(ctx, &tid, c.ID); got != nil {
		t.Fatal("delete failed")
	}
	if err := s.Delete(ctx, nil, g.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetByID(ctx, nil, g.ID); got != nil {
		t.Fatal("global delete failed")
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM cli_connections`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("count: %v %d", err, n)
	}
}
