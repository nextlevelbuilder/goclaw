package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/backup"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/upgrade"
)

func tenantBackupCmd() *cobra.Command {
	var (
		outputPath string
		tenantSlug string
		tenantID   string
		uploadS3   bool
	)

	cmd := &cobra.Command{
		Use:   "tenant-backup",
		Short: "Create a tenant-scoped backup (database rows + filesystem)",
		Long:  "Exports all DB rows belonging to a tenant + workspace/data dirs as a .tar.gz archive.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(resolveConfigPath())
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// Tenant backup is PG-only — SQLite edition has only master tenant
			if cfg.Database.StorageBackend == "sqlite" {
				return fmt.Errorf("tenant backup is not available in Lite edition (single tenant). Use 'goclaw backup' for full system backup")
			}

			tid, slug, db, err := resolveTenantForCLI(cmd, cfg, tenantID, tenantSlug)
			if err != nil {
				return err
			}
			defer db.Close()

			if outputPath == "" {
				ts := time.Now().UTC().Format("20060102-150405")
				outputPath = fmt.Sprintf("./tenant-backup-%s-%s.tar.gz", slug, ts)
			}

			fmt.Printf("Starting tenant backup → %s\n", outputPath)
			fmt.Printf("  tenant : %s (%s)\n", slug, tid)

			dataDir := config.TenantDataDir(cfg.ResolvedDataDir(), tid, slug)
			wsDir := config.TenantWorkspace(cfg.WorkspacePath(), tid, slug)

			opts := backup.TenantBackupOptions{
				DB:            db,
				TenantID:      tid,
				TenantSlug:    slug,
				DataDir:       dataDir,
				WorkspacePath: wsDir,
				OutputPath:    outputPath,
				CreatedBy:     "cli",
				SchemaVersion: int(upgrade.RequiredSchemaVersion),
				ProgressFn: func(phase, detail string) {
					fmt.Printf("  [%s] %s\n", phase, detail)
				},
			}

			manifest, err := backup.TenantBackup(cmd.Context(), opts)
			if err != nil {
				return fmt.Errorf("tenant backup failed: %w", err)
			}

			fmt.Printf("\nTenant backup complete: %s\n", outputPath)
			fmt.Printf("  tenant         : %s\n", manifest.TenantSlug)
			fmt.Printf("  schema version : %d\n", manifest.SchemaVersion)
			fmt.Printf("  tables         : %d\n", len(manifest.TableCounts))
			fmt.Printf("  files          : %d\n", manifest.Stats.FilesystemFiles)

			if uploadS3 {
				return tenantS3Upload(cmd, cfg, outputPath)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&tenantSlug, "tenant", "", "tenant slug to back up")
	cmd.Flags().StringVar(&tenantID, "tenant-id", "", "tenant UUID (alternative to --tenant)")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "output path for .tar.gz")
	cmd.Flags().BoolVar(&uploadS3, "upload-s3", false, "upload backup to S3 after creation")
	return cmd
}
