package backup

import "testing"

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
