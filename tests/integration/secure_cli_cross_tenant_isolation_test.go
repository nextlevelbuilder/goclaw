//go:build integration

package integration

// C3 regression guard: verify tenant isolation at the store layer for
// secure_cli_binaries.List + agent_grants_summary aggregation.
//
// Scope: store-layer tests only. Isolation is enforced in SQL (WHERE
// b.tenant_id = $2 and g.tenant_id = $1 in the LEFT JOIN LATERAL subquery),
// so store-layer coverage catches regressions in the tenant-scoping predicate.
// HTTP-layer cross-tenant tests are deferred until gateway-token auth
// scaffolding is wired into the integration suite.

import (
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// TestSecureCLICrossTenant_ListDoesNotExposeForeignData verifies that
// store.List scoped to tenant B does not return tenant A's binaries.
func TestSecureCLICrossTenant_ListDoesNotExposeForeignData(t *testing.T) {
	t.Parallel()

	db := testDB(t)

	tenantA, agentA := seedTenantAgent(t, db)
	binaryA := seedSecureCLI(t, db, tenantA)
	grantA := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO secure_cli_agent_grants
			(id, binary_id, agent_id, tenant_id, encrypted_env, enabled)
		 VALUES ($1, $2, $3, $4, $5, true)`,
		grantA, binaryA, agentA, tenantA, []byte(`{"KEY":"val"}`),
	); err != nil {
		t.Fatalf("seed grant A: %v", err)
	}

	tenantB, _ := seedTenantAgent(t, db)
	binaryB := seedSecureCLI(t, db, tenantB)

	cliStore := pg.NewPGSecureCLIStore(db, testEncryptionKey)

	binsA, err := cliStore.List(tenantCtx(tenantA))
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	if len(binsA) != 1 || binsA[0].ID != binaryA {
		t.Errorf("tenant A should see exactly binary A; got %d binaries", len(binsA))
	}

	binsB, err := cliStore.List(tenantCtx(tenantB))
	if err != nil {
		t.Fatalf("list B: %v", err)
	}
	if len(binsB) != 1 || binsB[0].ID != binaryB {
		t.Errorf("tenant B should see exactly binary B; got %d binaries", len(binsB))
	}
	for _, b := range binsB {
		if b.ID == binaryA {
			t.Errorf("tenant B LEAKED: saw binary from tenant A (%s)", binaryA)
		}
	}
}

// TestSecureCLICrossTenant_AggregateListScopeIsolation verifies that the
// agent_grants_summary LEFT JOIN LATERAL subquery filters grants by caller
// tenant — each tenant only sees its own grants in the summary.
func TestSecureCLICrossTenant_AggregateListScopeIsolation(t *testing.T) {
	t.Parallel()

	db := testDB(t)

	tenantA, agentA := seedTenantAgent(t, db)
	binaryA := seedSecureCLI(t, db, tenantA)
	grantA := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO secure_cli_agent_grants
			(id, binary_id, agent_id, tenant_id, encrypted_env, enabled)
		 VALUES ($1, $2, $3, $4, $5, true)`,
		grantA, binaryA, agentA, tenantA, []byte(`{"KEY":"val"}`),
	); err != nil {
		t.Fatalf("seed grant A: %v", err)
	}

	tenantB, agentB := seedTenantAgent(t, db)
	binaryB := seedSecureCLI(t, db, tenantB)
	grantB := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO secure_cli_agent_grants
			(id, binary_id, agent_id, tenant_id, encrypted_env, enabled)
		 VALUES ($1, $2, $3, $4, $5, true)`,
		grantB, binaryB, agentB, tenantB, []byte(`{}`),
	); err != nil {
		t.Fatalf("seed grant B: %v", err)
	}

	cliStore := pg.NewPGSecureCLIStore(db, testEncryptionKey)

	binsA, err := cliStore.List(tenantCtx(tenantA))
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	if len(binsA) != 1 {
		t.Fatalf("tenant A expected 1 binary, got %d", len(binsA))
	}
	if got := len(binsA[0].AgentGrantsSummary); got != 1 {
		t.Errorf("tenant A binary expected 1 grant summary, got %d", got)
	}
	for _, g := range binsA[0].AgentGrantsSummary {
		if g.GrantID != grantA {
			t.Errorf("tenant A LEAKED grant from another tenant: %s", g.GrantID)
		}
	}

	binsB, err := cliStore.List(tenantCtx(tenantB))
	if err != nil {
		t.Fatalf("list B: %v", err)
	}
	if len(binsB) != 1 {
		t.Fatalf("tenant B expected 1 binary, got %d", len(binsB))
	}
	if got := len(binsB[0].AgentGrantsSummary); got != 1 {
		t.Errorf("tenant B binary expected 1 grant summary, got %d", got)
	}
	for _, g := range binsB[0].AgentGrantsSummary {
		if g.GrantID != grantB {
			t.Errorf("tenant B LEAKED grant from another tenant: %s", g.GrantID)
		}
	}
}
