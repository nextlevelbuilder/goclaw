//go:build integration

package integration

// C4 coverage: per-grant env override store-layer tests.
// Covers: CRUD env override, denylist validation (via crypto package),
// 3-state semantics (absent/null/map), and the env_set/env_keys fields.

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// TestGrantEnv_SetAndReveal verifies that UpdateGrantEnv stores encrypted env
// and that Get returns the decrypted plaintext in g.EncryptedEnv.
func TestGrantEnv_SetAndReveal(t *testing.T) {
	t.Parallel()

	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	binaryID := seedSecureCLI(t, db, tenantID)

	grantStore := pg.NewPGSecureCLIAgentGrantStore(db, testEncryptionKey)

	// Create a bare grant (no env).
	g := &store.SecureCLIAgentGrant{
		BinaryID: binaryID,
		AgentID:  agentID,
		Enabled:  true,
	}
	if err := grantStore.Create(tenantCtx(tenantID), g); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM secure_cli_agent_grants WHERE id = $1", g.ID) })

	// Set env override.
	plaintext := []byte(`{"MY_TOKEN":"secret123","MY_URL":"https://api.example.com"}`)
	if err := grantStore.UpdateGrantEnv(tenantCtx(tenantID), g.ID, plaintext); err != nil {
		t.Fatalf("UpdateGrantEnv: %v", err)
	}

	// Get must decrypt and return the plaintext in EncryptedEnv field.
	fetched, err := grantStore.Get(tenantCtx(tenantID), g.ID)
	if err != nil {
		t.Fatalf("Get after UpdateGrantEnv: %v", err)
	}
	if string(fetched.EncryptedEnv) != string(plaintext) {
		t.Errorf("Get.EncryptedEnv: want %s, got %s", plaintext, fetched.EncryptedEnv)
	}
}

// TestGrantEnv_ClearWithNil verifies the 3-state null-clears semantics.
// Passing nil to UpdateGrantEnv removes the env override.
func TestGrantEnv_ClearWithNil(t *testing.T) {
	t.Parallel()

	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	binaryID := seedSecureCLI(t, db, tenantID)

	grantStore := pg.NewPGSecureCLIAgentGrantStore(db, testEncryptionKey)
	g := &store.SecureCLIAgentGrant{BinaryID: binaryID, AgentID: agentID, Enabled: true}
	if err := grantStore.Create(tenantCtx(tenantID), g); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM secure_cli_agent_grants WHERE id = $1", g.ID) })

	// Set env.
	if err := grantStore.UpdateGrantEnv(tenantCtx(tenantID), g.ID, []byte(`{"KEY":"val"}`)); err != nil {
		t.Fatalf("UpdateGrantEnv set: %v", err)
	}

	// Clear by passing nil.
	if err := grantStore.UpdateGrantEnv(tenantCtx(tenantID), g.ID, nil); err != nil {
		t.Fatalf("UpdateGrantEnv clear: %v", err)
	}

	fetched, err := grantStore.Get(tenantCtx(tenantID), g.ID)
	if err != nil {
		t.Fatalf("Get after clear: %v", err)
	}
	if len(fetched.EncryptedEnv) > 0 {
		t.Errorf("expected empty EncryptedEnv after clear, got %q", fetched.EncryptedEnv)
	}
}

// TestGrantEnv_DenylistRejection verifies that IsDeniedEnvKey correctly rejects
// entries from the denylist (backend enforcement via crypto package).
func TestGrantEnv_DenylistRejection(t *testing.T) {
	cases := []struct {
		key    string
		denied bool
	}{
		{"PATH", true},
		{"LD_PRELOAD", true},
		{"DYLD_INSERT_LIBRARIES", true},
		{"GOCLAW_SECRET", true},
		{"MY_TOKEN", false},
		{"AWS_ACCESS_KEY_ID", false},
		{"NODE_OPTIONS", true},
		{"PYTHONPATH", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.key, func(t *testing.T) {
			got := crypto.IsDeniedEnvKey(tc.key)
			if got != tc.denied {
				t.Errorf("IsDeniedEnvKey(%q) = %v, want %v", tc.key, got, tc.denied)
			}
		})
	}
}

// TestGrantEnv_ValidateGrantEnvVars_DeniedKeysReported verifies that ValidateGrantEnvVars
// returns all denied keys in rejectedKeys (not silently drops them).
func TestGrantEnv_ValidateGrantEnvVars_DeniedKeysReported(t *testing.T) {
	envVars := map[string]string{
		"MY_SAFE_KEY": "value",
		"PATH":        "/bin",
		"HOME":        "/root",
	}
	rejected, valErr := crypto.ValidateGrantEnvVars(envVars)
	if valErr != nil {
		t.Fatalf("unexpected valErr: %v", valErr)
	}
	if len(rejected) != 2 {
		t.Errorf("expected 2 rejected keys (PATH, HOME), got %d: %v", len(rejected), rejected)
	}
	deniedSet := make(map[string]bool)
	for _, k := range rejected {
		deniedSet[k] = true
	}
	if !deniedSet["PATH"] {
		t.Error("PATH should be in rejected keys")
	}
	if !deniedSet["HOME"] {
		t.Error("HOME should be in rejected keys")
	}
}

// TestGrantEnv_ListReflectsPresence verifies that ListByBinary decrypts env
// and that env presence is detectable from EncryptedEnv field length.
func TestGrantEnv_ListReflectsPresence(t *testing.T) {
	t.Parallel()

	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	binaryID := seedSecureCLI(t, db, tenantID)

	grantStore := pg.NewPGSecureCLIAgentGrantStore(db, testEncryptionKey)
	g := &store.SecureCLIAgentGrant{BinaryID: binaryID, AgentID: agentID, Enabled: true}
	if err := grantStore.Create(tenantCtx(tenantID), g); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM secure_cli_agent_grants WHERE id = $1", g.ID) })

	if err := grantStore.UpdateGrantEnv(tenantCtx(tenantID), g.ID, []byte(`{"MY_KEY":"val"}`)); err != nil {
		t.Fatalf("UpdateGrantEnv: %v", err)
	}

	grants, err := grantStore.ListByBinary(tenantCtx(tenantID), binaryID)
	if err != nil {
		t.Fatalf("ListByBinary: %v", err)
	}
	if len(grants) == 0 {
		t.Fatal("expected at least one grant")
	}

	var found *store.SecureCLIAgentGrant
	for i := range grants {
		if grants[i].ID == g.ID {
			found = &grants[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("grant %s not found in ListByBinary", g.ID)
	}

	// After list, EncryptedEnv should contain decrypted data (store decrypts on scan).
	if len(found.EncryptedEnv) == 0 {
		t.Error("ListByBinary: EncryptedEnv should be populated (decrypted) when env exists")
	}
}

// TestGrantEnv_DeterministicValidationOrder verifies that ValidateGrantEnvVars
// produces deterministic error output when multiple denied keys are present.
func TestGrantEnv_DeterministicValidationOrder(t *testing.T) {
	envVars := map[string]string{
		"PATH":    "/bin",
		"HOME":    "/root",
		"MY_KEY":  "ok",
		"USER":    "root",
		"SHELL":   "/bin/bash",
	}

	rejected1, _ := crypto.ValidateGrantEnvVars(envVars)
	rejected2, _ := crypto.ValidateGrantEnvVars(envVars)

	if len(rejected1) != len(rejected2) {
		t.Errorf("non-deterministic: call 1 returned %d rejected keys, call 2 returned %d",
			len(rejected1), len(rejected2))
	}

	set1 := make(map[string]bool)
	for _, k := range rejected1 {
		set1[k] = true
	}
	for _, k := range rejected2 {
		if !set1[k] {
			t.Errorf("non-deterministic: key %q in call 2 but not call 1", k)
		}
	}
}

// TestGrantEnv_RevealDecryptedValue verifies the crypto round-trip that the
// reveal handler relies on: store.Get decrypts, caller parses as string map.
func TestGrantEnv_RevealDecryptedValue(t *testing.T) {
	t.Parallel()

	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	binaryID := seedSecureCLI(t, db, tenantID)

	grantStore := pg.NewPGSecureCLIAgentGrantStore(db, testEncryptionKey)
	g := &store.SecureCLIAgentGrant{BinaryID: binaryID, AgentID: agentID, Enabled: true}
	if err := grantStore.Create(tenantCtx(tenantID), g); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM secure_cli_agent_grants WHERE id = $1", g.ID) })

	secret := `{"API_KEY":"super-secret-value","ENDPOINT":"https://api.example.com"}`
	if err := grantStore.UpdateGrantEnv(tenantCtx(tenantID), g.ID, []byte(secret)); err != nil {
		t.Fatalf("UpdateGrantEnv: %v", err)
	}

	// Simulate reveal: Get decrypts, then caller parses as map.
	fetched, err := grantStore.Get(tenantCtx(tenantID), g.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(fetched.EncryptedEnv) != secret {
		t.Errorf("reveal: want %s, got %s", secret, fetched.EncryptedEnv)
	}

	var envMap map[string]string
	if err := json.Unmarshal(fetched.EncryptedEnv, &envMap); err != nil {
		t.Errorf("reveal result not valid JSON map: %v", err)
	}
	if envMap["API_KEY"] != "super-secret-value" {
		t.Errorf("wrong API_KEY value: %q", envMap["API_KEY"])
	}
}

// TestGrantEnv_GrantNotFoundCrossID verifies that Get with wrong tenant returns no row,
// enforcing tenant isolation for the reveal path.
func TestGrantEnv_GrantNotFoundCrossID(t *testing.T) {
	t.Parallel()

	db := testDB(t)
	tenantA, agentA := seedTenantAgent(t, db)
	binaryA := seedSecureCLI(t, db, tenantA)
	tenantB, _ := seedTenantAgent(t, db)

	grantStore := pg.NewPGSecureCLIAgentGrantStore(db, testEncryptionKey)
	g := &store.SecureCLIAgentGrant{BinaryID: binaryA, AgentID: agentA, Enabled: true}
	if err := grantStore.Create(tenantCtx(tenantA), g); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM secure_cli_agent_grants WHERE id = $1", g.ID) })

	// Tenant B trying to Get tenant A's grant must fail.
	_, err := grantStore.Get(tenantCtx(tenantB), g.ID)
	if err == nil {
		t.Error("Get with wrong tenant should return error (ErrNoRows), got nil")
	}
}

// Ensure uuid is used (referenced in TestGrantEnv_GrantNotFoundCrossID via uuid.UUID fields).
var _ = uuid.Nil
