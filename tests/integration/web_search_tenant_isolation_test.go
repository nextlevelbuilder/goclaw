//go:build integration

package integration

import (
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// TestWebSearch_TenantIsolation verifies that two tenants with different
// provider keys get independent chains, with no cross-tenant key leakage.
func TestWebSearch_TenantIsolation(t *testing.T) {
	db := testDB(t)
	tenantA, _ := seedTenantAgent(t, db)
	tenantB, _ := seedTenantAgent(t, db)

	encKey := testEncryptionKey

	secretsStore := pg.NewPGConfigSecretsStore(db, encKey)

	// Seed Brave key for tenant A
	braveKeyA := "test-key-brave-tenant-a-" + uuid.New().String()
	ctxA := tenantCtx(tenantA)

	enc, err := crypto.Encrypt(braveKeyA, encKey)
	if err != nil {
		t.Fatalf("encrypt tenantA key: %v", err)
	}

	_, err = db.ExecContext(ctxA,
		`INSERT INTO config_secrets (key, value, updated_at, tenant_id)
		 VALUES ($1, $2, NOW(), $3)
		 ON CONFLICT (key, tenant_id) DO NOTHING`,
		"tools.web.brave.api_key", []byte(enc), tenantA)
	if err != nil {
		t.Fatalf("insert tenantA brave key: %v", err)
	}

	t.Cleanup(func() {
		db.ExecContext(ctxA,
			`DELETE FROM config_secrets WHERE key=$1 AND tenant_id=$2`,
			"tools.web.brave.api_key", tenantA)
	})

	// Seed Exa key for tenant B
	exaKeyB := "test-key-exa-tenant-b-" + uuid.New().String()
	ctxB := tenantCtx(tenantB)

	encB, err := crypto.Encrypt(exaKeyB, encKey)
	if err != nil {
		t.Fatalf("encrypt tenantB key: %v", err)
	}

	_, err = db.ExecContext(ctxB,
		`INSERT INTO config_secrets (key, value, updated_at, tenant_id)
		 VALUES ($1, $2, NOW(), $3)
		 ON CONFLICT (key, tenant_id) DO NOTHING`,
		"tools.web.exa.api_key", []byte(encB), tenantB)
	if err != nil {
		t.Fatalf("insert tenantB exa key: %v", err)
	}

	t.Cleanup(func() {
		db.ExecContext(ctxB,
			`DELETE FROM config_secrets WHERE key=$1 AND tenant_id=$2`,
			"tools.web.exa.api_key", tenantB)
	})

	// Resolve chains for each tenant
	chainA := tools.BuildChainFromStorage(ctxA, secretsStore)
	chainB := tools.BuildChainFromStorage(ctxB, secretsStore)

	// Verify tenant A has Brave
	foundBraveInA := false
	for _, p := range chainA {
		if p.Name() == "brave" {
			foundBraveInA = true
			break
		}
	}
	if !foundBraveInA {
		t.Error("tenant A chain should contain Brave")
	}

	// Verify tenant B has Exa
	foundExaInB := false
	for _, p := range chainB {
		if p.Name() == "exa" {
			foundExaInB = true
			break
		}
	}
	if !foundExaInB {
		t.Error("tenant B chain should contain Exa")
	}

	// Verify tenant A does NOT have Exa
	foundExaInA := false
	for _, p := range chainA {
		if p.Name() == "exa" {
			foundExaInA = true
			break
		}
	}
	if foundExaInA {
		t.Error("tenant A chain should NOT contain Exa (cross-tenant leak)")
	}

	// Verify tenant B does NOT have Brave
	foundBraveInB := false
	for _, p := range chainB {
		if p.Name() == "brave" {
			foundBraveInB = true
			break
		}
	}
	if foundBraveInB {
		t.Error("tenant B chain should NOT contain Brave (cross-tenant leak)")
	}

	// Both should have DDG as fallback
	foundDDGInA := false
	for _, p := range chainA {
		if p.Name() == "duckduckgo" {
			foundDDGInA = true
			break
		}
	}
	if !foundDDGInA {
		t.Error("tenant A chain should have DDG fallback")
	}

	foundDDGInB := false
	for _, p := range chainB {
		if p.Name() == "duckduckgo" {
			foundDDGInB = true
			break
		}
	}
	if !foundDDGInB {
		t.Error("tenant B chain should have DDG fallback")
	}
}
