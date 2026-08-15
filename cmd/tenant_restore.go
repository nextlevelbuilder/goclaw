package cmd

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/backup"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func tenantRestoreCmd() *cobra.Command {
	var (
		tenantSlug    string
		tenantID      string
		newTenantSlug string
		mode          string
		force         bool
		dryRun        bool
	)

	cmd := &cobra.Command{
		Use:   "tenant-restore <archive-path>",
		Short: "Restore a tenant from a backup archive",
		Long: `Restores a tenant from a .tar.gz archive produced by 'goclaw tenant-backup'.

Modes:
  upsert   (default) — INSERT ... ON CONFLICT DO NOTHING. Non-destructive.
                       Requires --tenant or --tenant-id.
  replace            — Wipes tenant-scoped data (keeps tenant metadata), then INSERT.
                       Requires --tenant or --tenant-id AND --force.
  new                — Creates a new tenant from archive metadata.
                       Requires --new-tenant-slug. Archive's tenant_id is remapped
                       to the new tenant. Users, API keys, LLM providers, and all
                       other tenant-scoped data are cloned from the archive.`,
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

			// Validate flag wiring before touching the filesystem state (--force check).
			// Catches invalid combinations like mode=new with --tenant before the user
			// sees destructive-operation prompts.
			if err := validateTenantRestoreFlags(mode, tenantSlug, tenantID, newTenantSlug); err != nil {
				return err
			}

			if mode == "replace" && !dryRun && !force {
				fmt.Fprintln(os.Stderr, "ERROR: --force is required for replace mode (destructive operation).")
				os.Exit(1)
			}

			cfg, err := config.Load(resolveConfigPath())
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			var (
				tid  = uuid.Nil
				slug string
				db   *sql.DB
			)
			var restoreErr error

			switch mode {
			case "new":
				db, restoreErr = openTenantBackupDB(cfg)
				if restoreErr != nil {
					return restoreErr
				}
				slug = strings.TrimSpace(newTenantSlug)
			default:
				tid, slug, db, restoreErr = resolveTenantForCLI(cmd, cfg, tenantID, tenantSlug)
				if restoreErr != nil {
					return restoreErr
				}
			}
			defer db.Close()

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

	cmd.Flags().StringVar(&tenantSlug, "tenant", "", "target tenant slug (for mode=upsert|replace)")
	cmd.Flags().StringVar(&tenantID, "tenant-id", "", "target tenant UUID (for mode=upsert|replace, alternative to --tenant)")
	cmd.Flags().StringVar(&newTenantSlug, "new-tenant-slug", "", "slug for the new tenant (required for mode=new)")
	cmd.Flags().StringVar(&mode, "mode", "upsert", "restore mode: upsert, replace, new")
	cmd.Flags().BoolVar(&force, "force", false, "required for replace mode")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "inspect archive without making changes")
	return cmd
}

// validateTenantRestoreFlags enforces flag semantics per restore mode.
// Called before opening the database so invalid combinations fail fast.
func validateTenantRestoreFlags(mode, tenant, tenantID, newTenantSlug string) error {
	switch mode {
	case "new":
		if strings.TrimSpace(newTenantSlug) == "" {
			return fmt.Errorf("--new-tenant-slug is required for mode=new (choose a unique slug for the new tenant)")
		}
		if tenant != "" || tenantID != "" {
			return fmt.Errorf("mode=new does not accept --tenant or --tenant-id; use --new-tenant-slug to name the new tenant")
		}
	case "upsert", "replace":
		if tenant == "" && tenantID == "" {
			return fmt.Errorf("--tenant <slug> or --tenant-id <uuid> is required for mode=%s", mode)
		}
		if strings.TrimSpace(newTenantSlug) != "" {
			fmt.Fprintf(os.Stderr, "warning: --new-tenant-slug is ignored for mode=%s\n", mode)
		}
	default:
		return fmt.Errorf("invalid --mode=%q (allowed: upsert, replace, new)", mode)
	}
	return nil
}
