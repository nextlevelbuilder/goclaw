//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestSQLiteTenantStoreReadsTextAndBlobSettings(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	initSqlx(db)
	tenantStore := NewSQLiteTenantStore(db)
	ctx := context.Background()
	cases := []struct {
		name     string
		settings json.RawMessage
	}{
		{"text", json.RawMessage(`{"source":"text"}`)},
		{"blob", json.RawMessage(`{"source":"blob"}`)},
	}
	for _, tc := range cases {
		tenant := &store.TenantData{Name: tc.name, Slug: tc.name + "-slug", Status: "active", Settings: tc.settings}
		if err := tenantStore.CreateTenant(ctx, tenant); err != nil {
			t.Fatalf("CreateTenant(%s): %v", tc.name, err)
		}
		if tc.name == "text" {
			if _, err := db.Exec(`UPDATE tenants SET settings = ? WHERE id = ?`, string(tc.settings), tenant.ID); err != nil {
				t.Fatalf("store text settings: %v", err)
			}
		}
		got, err := tenantStore.GetTenant(ctx, tenant.ID)
		if err != nil {
			t.Fatalf("GetTenant(%s): %v", tc.name, err)
		}
		if string(got.Settings) != string(tc.settings) {
			t.Fatalf("GetTenant(%s).Settings = %s, want %s", tc.name, got.Settings, tc.settings)
		}
	}
}

// The seeded master tenant row relies on the schema DEFAULT '{}' which SQLite
// stores as TEXT; listing tenants on a fresh DB must not fail on it.
func TestSQLiteTenantStoreListTenantsFreshDB(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	initSqlx(db)
	tenantStore := NewSQLiteTenantStore(db)

	tenants, err := tenantStore.ListTenants(context.Background())
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	var master *store.TenantData
	for i := range tenants {
		if tenants[i].ID == store.MasterTenantID {
			master = &tenants[i]
		}
	}
	if master == nil {
		t.Fatalf("ListTenants did not return seeded master tenant: %+v", tenants)
	}
	if string(master.Settings) != `{}` {
		t.Fatalf("master settings = %q, want {}", master.Settings)
	}
}

func TestSQLiteTenantStoreSettingsStorageForms(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	initSqlx(db)
	tenantStore := NewSQLiteTenantStore(db)
	ctx := context.Background()

	cases := []struct {
		name   string
		stored any
		want   string
	}{
		{"empty-text", "", ""},
		{"empty-object-text", "{}", "{}"},
		{"object-text", `{"a":1}`, `{"a":1}`},
		{"object-blob", []byte(`{"b":2}`), `{"b":2}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tenant := &store.TenantData{Name: tc.name, Slug: tc.name, Status: "active"}
			if err := tenantStore.CreateTenant(ctx, tenant); err != nil {
				t.Fatalf("CreateTenant: %v", err)
			}
			if _, err := db.Exec(`UPDATE tenants SET settings = ? WHERE id = ?`, tc.stored, tenant.ID); err != nil {
				t.Fatalf("store settings: %v", err)
			}
			got, err := tenantStore.GetTenant(ctx, tenant.ID)
			if err != nil {
				t.Fatalf("GetTenant: %v", err)
			}
			if string(got.Settings) != tc.want {
				t.Fatalf("GetTenant.Settings = %q, want %q", got.Settings, tc.want)
			}
			bySlug, err := tenantStore.GetTenantBySlug(ctx, tc.name)
			if err != nil {
				t.Fatalf("GetTenantBySlug: %v", err)
			}
			if string(bySlug.Settings) != tc.want {
				t.Fatalf("GetTenantBySlug.Settings = %q, want %q", bySlug.Settings, tc.want)
			}
			byIDs, err := tenantStore.GetTenantsByIDs(ctx, []uuid.UUID{tenant.ID})
			if err != nil {
				t.Fatalf("GetTenantsByIDs: %v", err)
			}
			if len(byIDs) != 1 || string(byIDs[0].Settings) != tc.want {
				t.Fatalf("GetTenantsByIDs = %+v, want one row with settings %q", byIDs, tc.want)
			}
			if _, err := tenantStore.ListTenants(ctx); err != nil {
				t.Fatalf("ListTenants: %v", err)
			}
		})
	}
}
