//go:build integration

package integration

// Phase 1 (issue #82): verify the credential-type schema delta on PostgreSQL.
//   - adapter_name column on secure_cli_binaries
//   - credential_type, host_scope columns on secure_cli_user_credentials
//   - SetUserCredentialsTyped writes the new columns
//   - LookupByBinary projects AdapterName + UserCredentialType + UserHostScope
//   - Legacy paths (no adapter, SetUserCredentials with no type) keep all
//     new columns NULL for backward compat
//
// These tests run against a freshly-migrated test DB (migration 73 applied).

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// pgColumnExists checks information_schema for a column on the given table
// in the public schema.
func pgColumnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	var exists bool
	err := db.QueryRow(
		`SELECT EXISTS (
		   SELECT 1 FROM information_schema.columns
		   WHERE table_schema = 'public'
		     AND table_name = $1
		     AND column_name = $2
		 )`, table, column,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("information_schema lookup %s.%s: %v", table, column, err)
	}
	return exists
}

func TestPG_SchemaHasPhase1Columns(t *testing.T) {
	db := testDB(t)

	if !pgColumnExists(t, db, "secure_cli_binaries", "adapter_name") {
		t.Fatalf("secure_cli_binaries.adapter_name missing — migration 73 not applied")
	}
	if !pgColumnExists(t, db, "secure_cli_user_credentials", "credential_type") {
		t.Fatalf("secure_cli_user_credentials.credential_type missing")
	}
	if !pgColumnExists(t, db, "secure_cli_user_credentials", "host_scope") {
		t.Fatalf("secure_cli_user_credentials.host_scope missing")
	}
}

func TestPG_CreateBinary_AdapterNameRoundTrip(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := store.WithTenantID(context.Background(), tenantID)
	s := pg.NewPGSecureCLIStore(db, testEncryptionKey)

	adapter := "git"
	bin := &store.SecureCLIBinary{
		BinaryName:   "git-phase1-test-" + uuid.New().String()[:8],
		Description:  "Phase 1 adapter binary",
		IsGlobal:     true,
		Enabled:      true,
		CreatedBy:    "u-tester",
		AdapterName:  &adapter,
		EncryptedEnv: []byte(`{}`),
	}
	if err := s.Create(ctx, bin); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM secure_cli_binaries WHERE id = $1", bin.ID)
	})

	got, err := s.Get(ctx, bin.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AdapterName == nil || *got.AdapterName != "git" {
		t.Fatalf("expected adapter_name=git, got %v", got.AdapterName)
	}
}

func TestPG_SetUserCredentialsTyped_RoundTrip(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := store.WithTenantID(context.Background(), tenantID)
	s := pg.NewPGSecureCLIStore(db, testEncryptionKey)

	binID := seedGateBinary(t, db, tenantID, "git-typed-cred-"+uuid.New().String()[:8], true)

	credType := "pat"
	hostScope := "github.com"
	plaintext := []byte(`{"token":"ghp_xxx"}`)

	if err := s.SetUserCredentialsTyped(ctx, binID, "u-1", plaintext, &credType, &hostScope); err != nil {
		t.Fatalf("SetUserCredentialsTyped: %v", err)
	}

	got, err := s.GetUserCredentials(ctx, binID, "u-1")
	if err != nil {
		t.Fatalf("GetUserCredentials: %v", err)
	}
	if got == nil {
		t.Fatalf("expected credential, got nil")
	}
	if got.CredentialType == nil || *got.CredentialType != "pat" {
		t.Fatalf("expected credential_type=pat, got %v", got.CredentialType)
	}
	if got.HostScope == nil || *got.HostScope != "github.com" {
		t.Fatalf("expected host_scope=github.com, got %v", got.HostScope)
	}
	if string(got.EncryptedEnv) != string(plaintext) {
		t.Fatalf("decrypted env mismatch: got %q want %q", got.EncryptedEnv, plaintext)
	}
}

// Legacy SetUserCredentials (untyped) must store NULL for credential_type / host_scope.
func TestPG_SetUserCredentials_LegacyLeavesTypeColumnsNull(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := store.WithTenantID(context.Background(), tenantID)
	s := pg.NewPGSecureCLIStore(db, testEncryptionKey)

	binID := seedGateBinary(t, db, tenantID, "git-legacy-cred-"+uuid.New().String()[:8], true)

	if err := s.SetUserCredentials(ctx, binID, "u-legacy", []byte(`{"GITHUB_TOKEN":"x"}`)); err != nil {
		t.Fatalf("SetUserCredentials: %v", err)
	}

	got, err := s.GetUserCredentials(ctx, binID, "u-legacy")
	if err != nil {
		t.Fatalf("GetUserCredentials: %v", err)
	}
	if got == nil {
		t.Fatalf("expected credential, got nil")
	}
	if got.CredentialType != nil {
		t.Fatalf("expected credential_type NULL for legacy, got %q", *got.CredentialType)
	}
	if got.HostScope != nil {
		t.Fatalf("expected host_scope NULL for legacy, got %q", *got.HostScope)
	}
}

// LookupByBinary must populate AdapterName + UserCredentialType + UserHostScope
// from the LEFT JOIN onto secure_cli_user_credentials.
func TestPG_LookupByBinary_ProjectsNewColumns(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := store.WithTenantID(context.Background(), tenantID)
	s := pg.NewPGSecureCLIStore(db, testEncryptionKey)

	binName := "git-lookup-proj-" + uuid.New().String()[:8]
	adapter := "git"
	bin := &store.SecureCLIBinary{
		BinaryName:   binName,
		Description:  "Phase 1 lookup projection",
		IsGlobal:     true,
		Enabled:      true,
		CreatedBy:    "u-tester",
		AdapterName:  &adapter,
		EncryptedEnv: []byte(`{}`),
	}
	if err := s.Create(ctx, bin); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM secure_cli_user_credentials WHERE binary_id = $1", bin.ID)
		db.Exec("DELETE FROM secure_cli_binaries WHERE id = $1", bin.ID)
	})

	credType := "pat"
	hostScope := "github.com"
	if err := s.SetUserCredentialsTyped(ctx, bin.ID, "u-1", []byte(`{"token":"x"}`), &credType, &hostScope); err != nil {
		t.Fatalf("SetUserCredentialsTyped: %v", err)
	}

	got, err := s.LookupByBinary(ctx, binName, nil, "u-1")
	if err != nil {
		t.Fatalf("LookupByBinary: %v", err)
	}
	if got == nil {
		t.Fatalf("expected binary, got nil")
	}
	if got.AdapterName == nil || *got.AdapterName != "git" {
		t.Fatalf("expected AdapterName=git, got %v", got.AdapterName)
	}
	if got.UserCredentialType == nil || *got.UserCredentialType != "pat" {
		t.Fatalf("expected UserCredentialType=pat, got %v", got.UserCredentialType)
	}
	if got.UserHostScope == nil || *got.UserHostScope != "github.com" {
		t.Fatalf("expected UserHostScope=github.com, got %v", got.UserHostScope)
	}
}

// Backward-compat: binary with no adapter + no user credential must return
// successfully with all three new fields NULL.
func TestPG_LookupByBinary_BackwardCompatibleNulls(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := store.WithTenantID(context.Background(), tenantID)
	s := pg.NewPGSecureCLIStore(db, testEncryptionKey)

	binName := "gh-bc-null-" + uuid.New().String()[:8]
	seedGateBinary(t, db, tenantID, binName, true)

	got, err := s.LookupByBinary(ctx, binName, nil, "")
	if err != nil {
		t.Fatalf("LookupByBinary: %v", err)
	}
	if got == nil {
		t.Fatalf("expected binary, got nil")
	}
	if got.AdapterName != nil {
		t.Fatalf("expected AdapterName NULL, got %q", *got.AdapterName)
	}
	if got.UserCredentialType != nil {
		t.Fatalf("expected UserCredentialType NULL, got %q", *got.UserCredentialType)
	}
	if got.UserHostScope != nil {
		t.Fatalf("expected UserHostScope NULL, got %q", *got.UserHostScope)
	}
}
