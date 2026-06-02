//go:build integration

package integration

// C4 characterization test: lock the GET /v1/cli-credentials list response shape.
// Asserts that agent_grants_summary aggregate fields and env_set boolean are
// present in the store-layer response. This catches schema regressions where
// new columns or computed fields disappear from the list output.

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// TestSecureCLIListShape_AgentGrantsSummaryFields verifies that List returns
// agent_grants_summary entries with all required fields: grant_id, agent_id,
// agent_key, name, enabled, env_set.
func TestSecureCLIListShape_AgentGrantsSummaryFields(t *testing.T) {
	t.Parallel()

	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	binaryID := seedSecureCLI(t, db, tenantID)

	// Insert a grant with encrypted_env to set env_set=true.
	grantID := uuid.New()
	encEnvBytes := `{"SECRET_KEY":"value"}`
	if _, err := db.Exec(
		`INSERT INTO secure_cli_agent_grants
			(id, binary_id, agent_id, tenant_id, encrypted_env, enabled)
		 VALUES ($1, $2, $3, $4, $5, true)`,
		grantID, binaryID, agentID, tenantID, []byte(encEnvBytes),
	); err != nil {
		t.Fatalf("seed grant with env: %v", err)
	}

	cliStore := pg.NewPGSecureCLIStore(db, testEncryptionKey)
	bins, err := cliStore.List(tenantCtx(tenantID))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(bins) == 0 {
		t.Fatal("expected at least one binary in list")
	}

	// Find our binary.
	var target *store.SecureCLIBinary
	for i := range bins {
		if bins[i].ID == binaryID {
			target = &bins[i]
			break
		}
	}
	if target == nil {
		t.Fatalf("binary %s not found in list", binaryID)
	}

	// agent_grants_summary must be populated.
	if len(target.AgentGrantsSummary) == 0 {
		t.Fatal("AgentGrantsSummary: expected at least one entry, got none")
	}

	g := target.AgentGrantsSummary[0]

	// Lock grant_id field.
	if g.GrantID == uuid.Nil {
		t.Error("AgentGrantsSummary[0].GrantID: must not be nil")
	}
	if g.GrantID != grantID {
		t.Errorf("AgentGrantsSummary[0].GrantID: want %s, got %s", grantID, g.GrantID)
	}

	// Lock agent_id field.
	if g.AgentID == uuid.Nil {
		t.Error("AgentGrantsSummary[0].AgentID: must not be nil")
	}
	if g.AgentID != agentID {
		t.Errorf("AgentGrantsSummary[0].AgentID: want %s, got %s", agentID, g.AgentID)
	}

	// Lock agent_key field — must be non-empty string.
	if g.AgentKey == "" {
		t.Error("AgentGrantsSummary[0].AgentKey: must be non-empty")
	}

	// Lock enabled field — grant was seeded with enabled=true.
	if !g.Enabled {
		t.Error("AgentGrantsSummary[0].Enabled: want true, got false")
	}

	// Lock env_set field — grant has encrypted_env, so env_set must be true.
	if !g.EnvSet {
		t.Error("AgentGrantsSummary[0].EnvSet: want true (grant has encrypted_env), got false")
	}
}

// TestSecureCLIListShape_EnvSetFalseWhenNoEnv verifies that a grant with no
// encrypted_env reports env_set=false in the agent_grants_summary.
func TestSecureCLIListShape_EnvSetFalseWhenNoEnv(t *testing.T) {
	t.Parallel()

	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	binaryID := seedSecureCLI(t, db, tenantID)

	// Insert a grant WITHOUT encrypted_env (NULL).
	grantID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO secure_cli_agent_grants
			(id, binary_id, agent_id, tenant_id, encrypted_env, enabled)
		 VALUES ($1, $2, $3, $4, NULL, true)`,
		grantID, binaryID, agentID, tenantID,
	); err != nil {
		t.Fatalf("seed grant without env: %v", err)
	}

	cliStore := pg.NewPGSecureCLIStore(db, testEncryptionKey)
	bins, err := cliStore.List(tenantCtx(tenantID))
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	var target *store.SecureCLIBinary
	for i := range bins {
		if bins[i].ID == binaryID {
			target = &bins[i]
			break
		}
	}
	if target == nil {
		t.Fatalf("binary %s not found in list", binaryID)
	}
	if len(target.AgentGrantsSummary) == 0 {
		t.Fatal("AgentGrantsSummary: expected at least one entry")
	}

	g := target.AgentGrantsSummary[0]
	if g.GrantID != grantID {
		t.Fatalf("wrong grant in summary: want %s got %s", grantID, g.GrantID)
	}
	if g.EnvSet {
		t.Error("AgentGrantsSummary[0].EnvSet: want false (no encrypted_env), got true")
	}
}

// TestSecureCLIListShape_JSONFieldNames verifies the JSON serialized field names
// match the documented API contract: snake_case per Go struct json tags.
func TestSecureCLIListShape_JSONFieldNames(t *testing.T) {
	t.Parallel()

	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	binaryID := seedSecureCLI(t, db, tenantID)

	grantID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO secure_cli_agent_grants
			(id, binary_id, agent_id, tenant_id, encrypted_env, enabled)
		 VALUES ($1, $2, $3, $4, $5, true)`,
		grantID, binaryID, agentID, tenantID, []byte(`{"K":"v"}`),
	); err != nil {
		t.Fatalf("seed grant: %v", err)
	}

	cliStore := pg.NewPGSecureCLIStore(db, testEncryptionKey)
	bins, err := cliStore.List(tenantCtx(tenantID))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var target *store.SecureCLIBinary
	for i := range bins {
		if bins[i].ID == binaryID {
			target = &bins[i]
			break
		}
	}
	if target == nil || len(target.AgentGrantsSummary) == 0 {
		t.Fatal("binary or summary not found")
	}

	// Re-serialize to verify JSON field names.
	raw, err := json.Marshal(target.AgentGrantsSummary[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	requiredKeys := []string{"grant_id", "agent_id", "agent_key", "name", "enabled", "env_set"}
	for _, k := range requiredKeys {
		if _, ok := m[k]; !ok {
			t.Errorf("AgentGrantsSummary JSON missing field %q; got keys: %v", k, mapKeys(m))
		}
	}
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
