package cmd

import (
	"strings"
	"testing"
)

func TestValidateTenantRestoreFlags(t *testing.T) {
	tests := []struct {
		name, mode, tenant, tenantID, newSlug, wantErr string
	}{
		{"new ok", "new", "", "", "fresh-slug", ""},
		{"new missing slug", "new", "", "", "", "required for mode=new"},
		{"new with tenant slug", "new", "acme", "", "fresh-slug", "does not accept"},
		{"new with tenant-id", "new", "", "uuid-x", "fresh-slug", "does not accept"},
		{"upsert ok tenant", "upsert", "acme", "", "", ""},
		{"upsert ok tenant-id", "upsert", "", "uuid-x", "", ""},
		{"upsert ok both", "upsert", "acme", "uuid-x", "", ""},
		{"upsert missing", "upsert", "", "", "", "required for mode=upsert"},
		{"upsert ignores new slug (no error)", "upsert", "acme", "", "ignored", ""},
		{"replace ok", "replace", "acme", "", "", ""},
		{"replace missing", "replace", "", "", "", "required for mode=replace"},
		{"invalid mode", "bogus", "acme", "", "", "invalid --mode"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTenantRestoreFlags(tc.mode, tc.tenant, tc.tenantID, tc.newSlug)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want nil, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want err containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want err containing %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}
