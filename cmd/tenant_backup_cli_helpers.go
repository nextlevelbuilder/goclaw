package cmd

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// resolveTenantForCLI opens the DB, looks up the tenant by slug or UUID,
// and returns the resolved tenant ID, slug, and an open *sql.DB (caller must close).
func resolveTenantForCLI(cmd *cobra.Command, cfg *config.Config, rawID, slug string) (uuid.UUID, string, *sql.DB, error) {
	if rawID == "" && slug == "" {
		return uuid.Nil, "", nil, fmt.Errorf("--tenant <slug> or --tenant-id <uuid> is required")
	}

	dsn := cfg.Database.PostgresDSN
	if dsn == "" {
		return uuid.Nil, "", nil, fmt.Errorf("GOCLAW_POSTGRES_DSN not configured")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return uuid.Nil, "", nil, fmt.Errorf("open db: %w", err)
	}

	ts := pg.NewPGTenantStore(db)

	if rawID != "" {
		tid, err := uuid.Parse(rawID)
		if err != nil {
			db.Close()
			return uuid.Nil, "", nil, fmt.Errorf("invalid tenant-id: %w", err)
		}
		tenant, err := ts.GetTenant(cmd.Context(), tid)
		if err != nil {
			db.Close()
			return uuid.Nil, "", nil, fmt.Errorf("tenant not found: %w", err)
		}
		return tenant.ID, tenant.Slug, db, nil
	}

	tenant, err := ts.GetTenantBySlug(cmd.Context(), slug)
	if err != nil {
		db.Close()
		return uuid.Nil, "", nil, fmt.Errorf("tenant %q not found: %w", slug, err)
	}
	return tenant.ID, tenant.Slug, db, nil
}

// tenantS3Upload uploads a local archive to S3 using the system S3 config.
func tenantS3Upload(cmd *cobra.Command, cfg *config.Config, archivePath string) error {
	if err := uploadBackupToS3(cmd.Context(), cfg, archivePath, Version); err != nil {
		fmt.Fprintf(os.Stderr, "\nS3 upload failed: %v\n", err)
		return err
	}
	return nil
}
