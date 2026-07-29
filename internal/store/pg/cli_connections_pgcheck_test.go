//go:build pgcheck

package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Exercises the PG implementation against a real Postgres. Run with:
//   TEST_DATABASE_URL=... go test -tags pgcheck -run TestPGCLIConnections ./internal/store/pg/
func TestPGCLIConnections(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	s := NewPGCLIConnectionStore(db, "0123456789abcdef0123456789abcdef")
	ctx := context.Background()
	t1 := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	t2 := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	// create tenant-scoped + global
	c1 := &store.CLIConnection{TenantID: &t1, Name: "Claude Code", Kind: "external_cli", Provider: "claude_code", Mode: "delegate", Enabled: true, Config: json.RawMessage(`{"a":1}`)}
	if err := s.Upsert(ctx, c1); err != nil {
		t.Fatalf("upsert tenant conn: %v", err)
	}
	if c1.ID == uuid.Nil {
		t.Fatal("expected generated id")
	}
	g := &store.CLIConnection{Name: "Platform Codex", Kind: "external_cli", Provider: "codex", Mode: "delegate", Enabled: true}
	if err := s.Upsert(ctx, g); err != nil {
		t.Fatalf("upsert global conn: %v", err)
	}
	dis := &store.CLIConnection{TenantID: &t1, Name: "Disabled One", Provider: "aider", Kind: "external_cli", Mode: "delegate", Enabled: false}
	if err := s.Upsert(ctx, dis); err != nil {
		t.Fatalf("upsert disabled: %v", err)
	}

	// t1 sees its own + global; enabledOnly hides the disabled one
	got, err := s.ListForTenant(ctx, &t1, true)
	if err != nil {
		t.Fatalf("list t1: %v", err)
	}
	names := map[string]bool{}
	for _, c := range got {
		names[c.Name] = true
	}
	if !names["Claude Code"] || !names["Platform Codex"] {
		t.Fatalf("t1 should see own+global, got %v", names)
	}
	if names["Disabled One"] {
		t.Fatal("enabledOnly must hide disabled connections")
	}

	// t2 must NOT see t1's connection, but must see the global one
	got2, err := s.ListForTenant(ctx, &t2, true)
	if err != nil {
		t.Fatalf("list t2: %v", err)
	}
	for _, c := range got2 {
		if c.Name == "Claude Code" {
			t.Fatal("TENANT LEAK: t2 sees t1's connection")
		}
	}
	foundGlobal := false
	for _, c := range got2 {
		if c.Name == "Platform Codex" {
			foundGlobal = true
		}
	}
	if !foundGlobal {
		t.Fatal("t2 should see the global connection")
	}

	// GetByID respects scope
	if c, err := s.GetByID(ctx, &t2, c1.ID); err != nil || c != nil {
		t.Fatalf("t2 must not read t1's conn (err=%v c=%v)", err, c)
	}
	if c, err := s.GetByID(ctx, &t1, c1.ID); err != nil || c == nil {
		t.Fatalf("t1 must read its own conn (err=%v)", err)
	}

	// credentials: shared fallback, then per-user override
	if err := s.PutCredential(ctx, store.CLIConnectionCredential{ConnectionID: c1.ID, UserID: store.SharedCredentialUserID, TenantID: &t1, Type: "oauth", Inject: "env:CLAUDE_CODE_OAUTH_TOKEN", Secret: "SHARED"}); err != nil {
		t.Fatalf("put shared cred: %v", err)
	}
	cr, err := s.GetCredential(ctx, c1.ID, "user-A")
	if err != nil || cr == nil {
		t.Fatalf("expected shared fallback (err=%v)", err)
	}
	if cr.Secret != "SHARED" {
		t.Fatalf("want SHARED, got %q", cr.Secret)
	}
	if err := s.PutCredential(ctx, store.CLIConnectionCredential{ConnectionID: c1.ID, UserID: "user-A", TenantID: &t1, Type: "api_key", Inject: "env:ANTHROPIC_API_KEY", Secret: "MINE"}); err != nil {
		t.Fatalf("put user cred: %v", err)
	}
	cr, _ = s.GetCredential(ctx, c1.ID, "user-A")
	if cr == nil || cr.Secret != "MINE" {
		t.Fatalf("per-user must win, got %v", cr)
	}
	crB, _ := s.GetCredential(ctx, c1.ID, "user-B")
	if crB == nil || crB.Secret != "SHARED" {
		t.Fatalf("other user must still get SHARED, got %v", crB)
	}

	// encrypted at rest
	var raw string
	if err := db.QueryRow(`SELECT secret_enc FROM cli_connection_credentials WHERE connection_id=$1 AND user_id='user-A'`, c1.ID).Scan(&raw); err != nil {
		t.Fatalf("read raw: %v", err)
	}
	if raw == "MINE" {
		t.Fatal("SECRET STORED IN PLAINTEXT")
	}

	// delete cascades credentials
	if err := s.Delete(ctx, &t1, c1.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM cli_connection_credentials WHERE connection_id=$1`, c1.ID).Scan(&n)
	if n != 0 {
		t.Fatalf("credentials should cascade, %d left", n)
	}
	t.Log("PG store verified: scoping, global visibility, enabledOnly, cred fallback/override, encryption, cascade")
}
