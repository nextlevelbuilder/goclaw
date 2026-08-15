//go:build integration

package integration

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/nextlevelbuilder/goclaw/internal/backup"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// TestBackup_CheckActiveConnections_ExcludesGatewayPool is a regression test for
// issue #1338: restoring on a fresh server failed with "N active DB connection(s)
// detected" because the gateway's own pool connections were counted as active
// clients. Gateway pool connections are tagged application_name='goclaw' and must
// be excluded; genuinely external connections must still be counted.
func TestBackup_CheckActiveConnections_ExcludesGatewayPool(t *testing.T) {
	testDB(t) // skips if PG unavailable
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = defaultTestDSN
	}
	ctx := context.Background()

	base, err := backup.CheckActiveConnections(ctx, dsn)
	if err != nil {
		t.Fatalf("baseline CheckActiveConnections: %v", err)
	}

	// A gateway-tagged pool connection must NOT be counted.
	pool, err := pg.OpenDB(dsn)
	if err != nil {
		t.Fatalf("open gateway pool: %v", err)
	}
	defer pool.Close()
	var one int
	if err := pool.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		t.Fatalf("warm gateway pool: %v", err)
	}

	after, err := backup.CheckActiveConnections(ctx, dsn)
	if err != nil {
		t.Fatalf("CheckActiveConnections after gateway pool: %v", err)
	}
	if after != base {
		t.Errorf("gateway pool connection was counted as active: base=%d after=%d", base, after)
	}

	// A non-gateway connection MUST be counted.
	external, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open external conn: %v", err)
	}
	defer external.Close()
	if err := external.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		t.Fatalf("warm external conn: %v", err)
	}
	withExternal, err := backup.CheckActiveConnections(ctx, dsn)
	if err != nil {
		t.Fatalf("CheckActiveConnections with external: %v", err)
	}
	if withExternal <= after {
		t.Errorf("external connection not counted: after=%d withExternal=%d", after, withExternal)
	}
}
