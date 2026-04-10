package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/backup"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func tenantRestoreCmd() *cobra.Command {
	var (
		tenantSlug string
		tenantID   string
		mode       string
		force      bool
		dryRun     bool
	)

	cmd := &cobra.Command{
		Use:   "tenant-restore <archive-path>",
		Short: "Restore a tenant from a backup archive",
		Long: `Restores a tenant from a .tar.gz archive produced by 'goclaw tenant-backup'.

Modes:
  upsert  (default) — INSERT … ON CONFLICT DO NOTHING. Non-destructive.
  replace           — Delete existing tenant data first, then INSERT. Requires --force.
  new               — Create a new tenant and import data under the new tenant ID.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			archivePath := args[0]

			// Tenant restore is PG-only
			cfg, cfgErr := config.Load(resolveConfigPath())
			if cfgErr == nil && cfg.Database.StorageBackend == "sqlite" {
				return fmt.Errorf("tenant restore is not available in Lite edition (single tenant). Use 'goclaw restore' for full system restore")
			}

			if _, err := os.Stat(archivePath); err != nil {
				return fmt.Errorf("archive not found: %s", archivePath)
			}
			if mode == "replace" && !dryRun && !force {
				fmt.Fprintln(os.Stderr, "ERROR: --force is required for replace mode (destructive operation).")
				os.Exit(1)
			}

			cfg, err := config.Load(resolveConfigPath())
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// For "new" mode the tenant may not exist yet — allow lookup failure.
			tid, slug, db, lookupErr := resolveTenantForCLI(cmd, cfg, tenantID, tenantSlug)
			if lookupErr != nil && mode != "new" {
				return lookupErr
			}
			if db != nil {
				defer db.Close()
			}

			dataDir := config.TenantDataDir(cfg.ResolvedDataDir(), tid, slug)
			wsDir := config.TenantWorkspace(cfg.WorkspacePath(), tid, slug)

			if dryRun {
				fmt.Printf("Dry-run: inspecting archive %s\n", archivePath)
			} else {
				fmt.Printf("Restoring tenant (%s) from: %s\n", slug, archivePath)
				fmt.Printf("  mode: %s\n", mode)
			}

			opts := backup.TenantRestoreOptions{
				DB:            db,
				ArchivePath:   archivePath,
				TenantID:      tid,
				TenantSlug:    slug,
				DataDir:       dataDir,
				WorkspacePath: wsDir,
				Mode:          mode,
				Force:         force,
				DryRun:        dryRun,
				ProgressFn: func(phase, detail string) {
					fmt.Printf("  [%s] %s\n", phase, detail)
				},
			}

			result, err := backup.TenantRestore(cmd.Context(), opts)
			if err != nil {
				return fmt.Errorf("tenant restore failed: %w", err)
			}

			fmt.Println()
			if dryRun {
				fmt.Println("Dry-run complete (no changes made).")
			} else {
				fmt.Println("Tenant restore complete:")
				fmt.Printf("  tenant_id      : %s\n", result.TenantID)
				fmt.Printf("  tables restored: %d\n", len(result.TablesRestored))
				fmt.Printf("  files extracted: %d\n", result.FilesExtracted)
			}
			for _, w := range result.Warnings {
				fmt.Printf("  WARNING: %s\n", w)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&tenantSlug, "tenant", "", "target tenant slug")
	cmd.Flags().StringVar(&tenantID, "tenant-id", "", "target tenant UUID")
	cmd.Flags().StringVar(&mode, "mode", "upsert", "restore mode: upsert, replace, new")
	cmd.Flags().BoolVar(&force, "force", false, "required for replace mode")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "inspect archive without making changes")
	return cmd
}
