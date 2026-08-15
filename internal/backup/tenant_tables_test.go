package backup

import (
	"strings"
	"testing"
)

// TestExportQueryNeverOrdersByMissingIdColumn guards issue #1076/#1338: several
// tenant-scoped tables have no `id` column (config_secrets PK is (key,tenant_id),
// agent_team_members PK is (team_id,agent_id), tenant_hook_budget PK is tenant_id).
// exportQuery() previously hardcoded `ORDER BY id`, so exporting them failed with
// SQLSTATE 42703 "column id does not exist". Their registry entries must set an
// OrderBy that references real columns.
func TestExportQueryNeverOrdersByMissingIdColumn(t *testing.T) {
	idless := map[string]bool{
		"config_secrets":              true,
		"agent_team_members":          true,
		"tenant_hook_budget":          true,
		"user_agent_profiles":         true,
		"system_configs":              true,
		"builtin_tool_tenant_configs": true,
		"skill_tenant_configs":        true,
	}
	lookup := make(map[string]TableDef)
	for _, tbl := range TenantTables() {
		lookup[tbl.Name] = tbl
	}
	for name := range idless {
		tbl, ok := lookup[name]
		if !ok {
			t.Errorf("%s missing from backup registry", name)
			continue
		}
		q, err := tbl.exportQuery()
		if err != nil {
			t.Errorf("%s exportQuery error: %v", name, err)
			continue
		}
		if strings.Contains(q, "ORDER BY id") {
			t.Errorf("%s export orders by missing id column: %q", name, q)
		}
	}
}

// TestHookWebhookFamilyRegistered guards issue #1076: hooks + budget + webhook
// config tables were missing from the backup registry, so their data was silently
// dropped on tenant backup/restore. The junction hook_agents (no tenant_id) must
// export via a ParentJoin through hooks and order by its composite PK.
func TestHookWebhookFamilyRegistered(t *testing.T) {
	lookup := make(map[string]TableDef)
	for _, tbl := range TenantTables() {
		lookup[tbl.Name] = tbl
	}
	for _, name := range []string{"hooks", "tenant_hook_budget", "webhooks", "hook_agents"} {
		if _, ok := lookup[name]; !ok {
			t.Errorf("%s missing from backup registry", name)
		}
	}
	ha, ok := lookup["hook_agents"]
	if !ok {
		return
	}
	q, err := ha.exportQuery()
	if err != nil {
		t.Fatalf("hook_agents exportQuery error: %v", err)
	}
	if !strings.Contains(q, "JOIN hooks") {
		t.Errorf("hook_agents export must join hooks for tenant scope: %q", q)
	}
	if strings.Contains(q, "ORDER BY vl.id") {
		t.Errorf("hook_agents has no id column; export must not order by vl.id: %q", q)
	}
}

func TestTenantTablesIncludesTenantUsersAndTenantScope(t *testing.T) {
	tables := TenantTables()
	lookup := make(map[string]TableDef, len(tables))
	for _, table := range tables {
		lookup[table.Name] = table
	}

	tenants, ok := lookup["tenants"]
	if !ok {
		t.Fatal("tenants table missing from backup registry")
	}
	if tenants.HasTenantID {
		t.Fatal("tenants should not use tenant_id filtering")
	}
	if tenants.ScopeColumn != "id" {
		t.Fatalf("tenants scope column = %q, want %q", tenants.ScopeColumn, "id")
	}

	tenantUsers, ok := lookup["tenant_users"]
	if !ok {
		t.Fatal("tenant_users should be included in the tenant backup registry")
	}
	if !tenantUsers.HasTenantID {
		t.Fatal("tenant_users should use tenant_id filtering")
	}
}

func TestTableDefQueriesUseExpectedTenantScope(t *testing.T) {
	tests := []struct {
		name       string
		table      TableDef
		wantExport string
		wantDelete string
		wantErr    bool
	}{
		{
			name:       "tenants",
			table:      TableDef{Name: "tenants", ScopeColumn: "id"},
			wantExport: "SELECT * FROM tenants WHERE id = $1 ORDER BY id",
			wantDelete: "DELETE FROM tenants WHERE id = $1",
		},
		{
			name:       "tenant_users",
			table:      TableDef{Name: "tenant_users", HasTenantID: true},
			wantExport: "SELECT * FROM tenant_users WHERE tenant_id = $1 ORDER BY id",
			wantDelete: "DELETE FROM tenant_users WHERE tenant_id = $1",
		},
		{
			name:       "vault_links",
			table:      TableDef{Name: "vault_links", ParentJoin: "vault_links vl JOIN vault_documents fd ON vl.from_doc_id = fd.id WHERE fd.tenant_id = $1"},
			wantExport: "SELECT vl.* FROM vault_links vl JOIN vault_documents fd ON vl.from_doc_id = fd.id WHERE fd.tenant_id = $1 ORDER BY vl.id",
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			exportQuery, err := tc.table.exportQuery()
			if err != nil {
				t.Fatalf("exportQuery() error = %v", err)
			}
			if exportQuery != tc.wantExport {
				t.Fatalf("exportQuery() = %q, want %q", exportQuery, tc.wantExport)
			}

			deleteQuery, err := tc.table.deleteQuery()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("deleteQuery() = %q, want error", deleteQuery)
				}
				return
			}
			if err != nil {
				t.Fatalf("deleteQuery() error = %v", err)
			}
			if deleteQuery != tc.wantDelete {
				t.Fatalf("deleteQuery() = %q, want %q", deleteQuery, tc.wantDelete)
			}
		})
	}
}
