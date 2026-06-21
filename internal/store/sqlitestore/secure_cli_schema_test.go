//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Phase 1 schema delta tests:
//   - adapter_name column on secure_cli_binaries
//   - credential_type, host_scope columns on secure_cli_user_credentials
//   - LookupByBinary projects all three onto the returned struct
//   - Legacy callers (no adapter, no typed credential) still produce NULL columns
//
// These tests must stay green for any future schema migration touching the
// secure-cli surface to ensure backward compatibility.

// newPhase1Store opens a fresh SQLite DB with an empty encryption key.
// Phase 1 round-trip tests verify column projection, not crypto behavior — the
// shared testEncKey constant is not 32 bytes so crypto.Encrypt rejects it.
func newPhase1Store(t *testing.T) (*SQLiteSecureCLIStore, *sql.DB) {
	t.Helper()
	_, db := newTestSQLiteSecureCLI(t)
	return NewSQLiteSecureCLIStore(db, ""), db
}

// columnExists checks PRAGMA table_info for a column on the given SQLite table.
func columnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info(%s): %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info row: %v", err)
		}
		if strings.EqualFold(name, column) {
			return true
		}
	}
	return false
}

func TestSQLite_SchemaHasPhase1Columns(t *testing.T) {
	_, db := newTestSQLiteSecureCLI(t)

	if !columnExists(t, db, "secure_cli_binaries", "adapter_name") {
		t.Fatalf("secure_cli_binaries.adapter_name missing — migration not applied")
	}
	if !columnExists(t, db, "secure_cli_user_credentials", "credential_type") {
		t.Fatalf("secure_cli_user_credentials.credential_type missing")
	}
	if !columnExists(t, db, "secure_cli_user_credentials", "host_scope") {
		t.Fatalf("secure_cli_user_credentials.host_scope missing")
	}
}

func TestSQLite_CreateBinary_AdapterNameRoundTrip(t *testing.T) {
	s, db := newPhase1Store(t)
	tid := seedTenant(t, db, "t-adapter")
	ctx := store.WithTenantID(context.Background(), tid)

	adapter := "git"
	bin := &store.SecureCLIBinary{
		BinaryName:   "git",
		Description:  "git with PAT adapter",
		IsGlobal:     true,
		Enabled:      true,
		CreatedBy:    "u-tester",
		AdapterName:  &adapter,
		EncryptedEnv: []byte(`{}`),
	}
	if err := s.Create(ctx, bin); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Get(ctx, bin.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AdapterName == nil || *got.AdapterName != "git" {
		t.Fatalf("expected adapter_name=git, got %v", got.AdapterName)
	}
}

func TestSQLite_CreateBinary_AdapterNameNilStaysNull(t *testing.T) {
	s, db := newPhase1Store(t)
	tid := seedTenant(t, db, "t-no-adapter")
	ctx := store.WithTenantID(context.Background(), tid)

	bin := &store.SecureCLIBinary{
		BinaryName:   "gh",
		Description:  "legacy env-only binary",
		IsGlobal:     true,
		Enabled:      true,
		CreatedBy:    "u-tester",
		EncryptedEnv: []byte(`{}`),
	}
	if err := s.Create(ctx, bin); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s.Get(ctx, bin.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AdapterName != nil {
		t.Fatalf("expected adapter_name NULL, got %q", *got.AdapterName)
	}
}

func TestSQLite_SetUserCredentialsTyped_RoundTrip(t *testing.T) {
	s, db := newPhase1Store(t)
	tid := seedTenant(t, db, "t-typed-cred")
	ctx := store.WithTenantID(context.Background(), tid)

	binID := uuid.New()
	seedBinary(t, db, tid, "git", true, true)
	// Re-fetch the just-seeded binary's ID since seedBinary doesn't return it.
	if err := db.QueryRow(
		`SELECT id FROM secure_cli_binaries WHERE binary_name = ? AND tenant_id = ?`,
		"git", tid,
	).Scan(&binID); err != nil {
		t.Fatalf("lookup seeded binary: %v", err)
	}

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

// SetUserCredentials (legacy entrypoint) must leave credential_type / host_scope NULL.
func TestSQLite_SetUserCredentials_LegacyLeavesTypeColumnsNull(t *testing.T) {
	s, db := newPhase1Store(t)
	tid := seedTenant(t, db, "t-legacy-cred")
	ctx := store.WithTenantID(context.Background(), tid)

	seedBinary(t, db, tid, "git", true, true)
	var binID uuid.UUID
	if err := db.QueryRow(
		`SELECT id FROM secure_cli_binaries WHERE binary_name = ? AND tenant_id = ?`,
		"git", tid,
	).Scan(&binID); err != nil {
		t.Fatalf("lookup seeded binary: %v", err)
	}

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

// LookupByBinary must populate AdapterName (from binary row) AND
// UserCredentialType + UserHostScope (from joined user-credential row).
func TestSQLite_LookupByBinary_ProjectsNewColumns(t *testing.T) {
	s, db := newPhase1Store(t)
	tid := seedTenant(t, db, "t-lookup-proj")
	ctx := store.WithTenantID(context.Background(), tid)

	adapter := "git"
	bin := &store.SecureCLIBinary{
		BinaryName:   "git",
		Description:  "git adapter binary",
		IsGlobal:     true,
		Enabled:      true,
		CreatedBy:    "u-tester",
		AdapterName:  &adapter,
		EncryptedEnv: []byte(`{}`),
	}
	if err := s.Create(ctx, bin); err != nil {
		t.Fatalf("Create: %v", err)
	}

	credType := "pat"
	hostScope := "github.com"
	if err := s.SetUserCredentialsTyped(ctx, bin.ID, "u-1", []byte(`{"token":"x"}`), &credType, &hostScope); err != nil {
		t.Fatalf("SetUserCredentialsTyped: %v", err)
	}

	got, err := s.LookupByBinary(ctx, "git", nil, "u-1")
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

// Backward-compat: a binary with no adapter + no user credential must still
// return successfully from LookupByBinary with all three new fields NULL.
func TestSQLite_LookupByBinary_BackwardCompatibleNulls(t *testing.T) {
	s, db := newPhase1Store(t)
	tid := seedTenant(t, db, "t-bc-null")
	ctx := store.WithTenantID(context.Background(), tid)

	seedBinary(t, db, tid, "gh", true, true)

	got, err := s.LookupByBinary(ctx, "gh", nil, "")
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

func seedAgent(t *testing.T, db *sql.DB, tenantID uuid.UUID, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(
		`INSERT INTO agents (id, tenant_id, agent_key, display_name, owner_id, provider, model, agent_type, status)
		 VALUES (?, ?, ?, ?, 'owner', 'openai', 'gpt-5', 'predefined', 'active')`,
		id, tenantID, key, key,
	)
	if err != nil {
		t.Fatalf("seed agent %s: %v", key, err)
	}
	return id
}

func TestSQLite_SetAgentCredentialsTyped_RoundTrip(t *testing.T) {
	s, db := newPhase1Store(t)
	tid := seedTenant(t, db, "t-agent-typed")
	ctx := store.WithTenantID(context.Background(), tid)
	agentID := seedAgent(t, db, tid, "builder")

	seedBinary(t, db, tid, "git", true, true)
	var binID uuid.UUID
	if err := db.QueryRow(`SELECT id FROM secure_cli_binaries WHERE binary_name = ? AND tenant_id = ?`, "git", tid).Scan(&binID); err != nil {
		t.Fatalf("lookup seeded binary: %v", err)
	}

	credType := "pat"
	hostScope := "github.com"
	plaintext := []byte(`{"token":"ghp_agent"}`)
	if err := s.SetAgentCredentialsTyped(ctx, binID, agentID, plaintext, &credType, &hostScope, "admin"); err != nil {
		t.Fatalf("SetAgentCredentialsTyped: %v", err)
	}

	got, err := s.GetAgentCredentials(ctx, binID, agentID)
	if err != nil {
		t.Fatalf("GetAgentCredentials: %v", err)
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

func TestSQLite_LookupByBinary_UsesAgentCredentialWithoutUserID(t *testing.T) {
	s, db := newPhase1Store(t)
	tid := seedTenant(t, db, "t-agent-lookup")
	ctx := store.WithTenantID(context.Background(), tid)
	agentID := seedAgent(t, db, tid, "builder")

	adapter := "git"
	bin := &store.SecureCLIBinary{
		BinaryName:   "git",
		Description:  "git adapter binary",
		IsGlobal:     true,
		Enabled:      true,
		CreatedBy:    "u-tester",
		AdapterName:  &adapter,
		EncryptedEnv: []byte(`{}`),
	}
	if err := s.Create(ctx, bin); err != nil {
		t.Fatalf("Create: %v", err)
	}

	credType := "pat"
	hostScope := "github.com"
	if err := s.SetAgentCredentialsTyped(ctx, bin.ID, agentID, []byte(`{"token":"agent"}`), &credType, &hostScope, "admin"); err != nil {
		t.Fatalf("SetAgentCredentialsTyped: %v", err)
	}

	got, err := s.LookupByBinary(ctx, "git", &agentID, "")
	if err != nil {
		t.Fatalf("LookupByBinary: %v", err)
	}
	if got == nil {
		t.Fatalf("expected binary, got nil")
	}
	if got.CredentialSource != "agent" {
		t.Fatalf("expected agent source, got %q", got.CredentialSource)
	}
	if got.CredentialType == nil || *got.CredentialType != "pat" {
		t.Fatalf("expected CredentialType=pat, got %v", got.CredentialType)
	}
	if string(got.CredentialEnv) != `{"token":"agent"}` {
		t.Fatalf("expected agent credential env, got %q", got.CredentialEnv)
	}
}

func TestSQLite_LookupByBinary_UserCredentialOverridesAgentCredential(t *testing.T) {
	s, db := newPhase1Store(t)
	tid := seedTenant(t, db, "t-user-over-agent")
	ctx := store.WithTenantID(context.Background(), tid)
	agentID := seedAgent(t, db, tid, "builder")

	adapter := "git"
	bin := &store.SecureCLIBinary{
		BinaryName:   "git",
		Description:  "git adapter binary",
		IsGlobal:     true,
		Enabled:      true,
		CreatedBy:    "u-tester",
		AdapterName:  &adapter,
		EncryptedEnv: []byte(`{}`),
	}
	if err := s.Create(ctx, bin); err != nil {
		t.Fatalf("Create: %v", err)
	}

	credType := "pat"
	hostScope := "github.com"
	if err := s.SetAgentCredentialsTyped(ctx, bin.ID, agentID, []byte(`{"token":"agent"}`), &credType, &hostScope, "admin"); err != nil {
		t.Fatalf("SetAgentCredentialsTyped: %v", err)
	}
	if err := s.SetUserCredentialsTyped(ctx, bin.ID, "u-1", []byte(`{"token":"user"}`), &credType, &hostScope); err != nil {
		t.Fatalf("SetUserCredentialsTyped: %v", err)
	}

	got, err := s.LookupByBinary(ctx, "git", &agentID, "u-1")
	if err != nil {
		t.Fatalf("LookupByBinary: %v", err)
	}
	if got == nil {
		t.Fatalf("expected binary, got nil")
	}
	if got.CredentialSource != "user" {
		t.Fatalf("expected user source, got %q", got.CredentialSource)
	}
	if string(got.CredentialEnv) != `{"token":"user"}` {
		t.Fatalf("expected user credential env, got %q", got.CredentialEnv)
	}
}

func TestSQLite_SetAgentCredentialsRejectsCrossTenantAgent(t *testing.T) {
	s, db := newPhase1Store(t)
	tenantA := seedTenant(t, db, "t-agent-cred-a")
	tenantB := seedTenant(t, db, "t-agent-cred-b")
	ctxA := store.WithTenantID(context.Background(), tenantA)
	agentB := seedAgent(t, db, tenantB, "other-tenant-agent")

	bin := &store.SecureCLIBinary{
		BinaryName:   "git",
		Description:  "git adapter binary",
		IsGlobal:     true,
		Enabled:      true,
		CreatedBy:    "u-tester",
		EncryptedEnv: []byte(`{}`),
	}
	if err := s.Create(ctxA, bin); err != nil {
		t.Fatalf("Create: %v", err)
	}

	credType := "pat"
	hostScope := "github.com"
	err := s.SetAgentCredentialsTyped(ctxA, bin.ID, agentB, []byte(`{"token":"agent"}`), &credType, &hostScope, "admin")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows for cross-tenant agent, got %v", err)
	}
}
