//go:build integration

package integration

import (
	"bytes"
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/backup"
)

// TestBackup_ExportAllRegistryTables_NoSQLError is an end-to-end regression for
// issue #1076/#1338: exporting config_secrets (and other composite-PK tables)
// failed with SQLSTATE 42703 "column id does not exist" because exportQuery
// hardcoded ORDER BY id. PostgreSQL validates the ORDER BY column at plan time,
// so the export fails even for an empty tenant. Running ExportTable over every
// registered table proves each query is valid against the real schema — including
// the hook_agents ParentJoin.
func TestBackup_ExportAllRegistryTables_NoSQLError(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)

	for _, tbl := range backup.TenantTables() {
		t.Run(tbl.Name, func(t *testing.T) {
			var buf bytes.Buffer
			if _, err := backup.ExportTable(context.Background(), db, tbl, tenantID, &buf); err != nil {
				t.Errorf("ExportTable(%s): %v", tbl.Name, err)
			}
		})
	}
}
