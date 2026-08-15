package backup

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestLoadTenantRestoreRow(t *testing.T) {
	row, err := loadTenantRestoreRow([]byte(`{"id":"0193a5b0-7000-7000-8000-000000000001","name":"Master","slug":"master","status":"active","settings":"{\"theme\":\"dark\"}"}`))
	if err != nil {
		t.Fatalf("loadTenantRestoreRow() error = %v", err)
	}
	if row.Name != "Master" {
		t.Fatalf("row.Name = %q, want %q", row.Name, "Master")
	}
	if row.Slug != "master" {
		t.Fatalf("row.Slug = %q, want %q", row.Slug, "master")
	}
	if row.Status != "active" {
		t.Fatalf("row.Status = %q, want %q", row.Status, "active")
	}
	if row.Settings != `{"theme":"dark"}` {
		t.Fatalf("row.Settings = %q, want %q", row.Settings, `{"theme":"dark"}`)
	}
}

func TestShouldRestoreTable(t *testing.T) {
	if shouldRestoreTable("new", TableDef{Name: "tenants"}) {
		t.Fatal("shouldRestoreTable(new, tenants) = true, want false")
	}
	if shouldRestoreTable("replace", TableDef{Name: "tenants"}) {
		t.Fatal("shouldRestoreTable(replace, tenants) = true, want false")
	}
	if !shouldRestoreTable("upsert", TableDef{Name: "tenants"}) {
		t.Fatal("shouldRestoreTable(upsert, tenants) = false, want true")
	}
	if !shouldRestoreTable("new", TableDef{Name: "agents"}) {
		t.Fatal("shouldRestoreTable(new, agents) = false, want true")
	}
	if !shouldRestoreTable("replace", TableDef{Name: "agents"}) {
		t.Fatal("shouldRestoreTable(replace, agents) = false, want true")
	}
}

func TestEnsureTenantSlugAvailable(t *testing.T) {
	existingID := uuid.MustParse("0193a5b0-7000-7000-8000-000000000002")

	tests := []struct {
		name            string
		lookup          func(context.Context, string) (uuid.UUID, error)
		wantErrContains string
	}{
		{
			name: "available",
			lookup: func(context.Context, string) (uuid.UUID, error) {
				return uuid.Nil, sql.ErrNoRows
			},
		},
		{
			name: "duplicate",
			lookup: func(context.Context, string) (uuid.UUID, error) {
				return existingID, nil
			},
			wantErrContains: "already exists",
		},
		{
			name: "lookup failure",
			lookup: func(context.Context, string) (uuid.UUID, error) {
				return uuid.Nil, errors.New("boom")
			},
			wantErrContains: "check tenant slug",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ensureTenantSlugAvailable(context.Background(), "new-slug", tt.lookup)
			if tt.wantErrContains == "" {
				if err != nil {
					t.Fatalf("ensureTenantSlugAvailable() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("ensureTenantSlugAvailable() error = nil, want non-nil")
			}
			if !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Fatalf("ensureTenantSlugAvailable() error = %q, want substring %q", err.Error(), tt.wantErrContains)
			}
		})
	}
}
