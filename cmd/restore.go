package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/backup"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func restoreCmd() *cobra.Command {
	var (
		skipDB    bool
		skipFiles bool
		force     bool
		dryRun    bool
		fromS3    string
		listS3    bool
	)

	cmd := &cobra.Command{
		Use:   "restore [archive-path]",
		Short: "Restore system from a backup archive (database + filesystem)",
		Long: `Restores GoClaw from a .tar.gz backup archive produced by 'goclaw backup'.

WARNING: This is a destructive operation. The database will be overwritten.
Requires --force flag to proceed. Stop the gateway before restoring.

Use --list-s3 to list available S3 backups.
Use --from-s3 <key> to download and restore from S3.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(resolveConfigPath())
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// --list-s3: list available S3 backups and exit.
			if listS3 {
				return listS3Backups(cmd.Context(), cfg)
			}

			// --from-s3: download backup from S3 to a temp file, then restore.
			archivePath := ""
			if len(args) > 0 {
				archivePath = args[0]
			}

			if fromS3 != "" {
				tmpPath, err := downloadFromS3(cmd.Context(), cfg, fromS3)
				if err != nil {
					return fmt.Errorf("s3 download: %w", err)
				}
				defer os.Remove(tmpPath)
				archivePath = tmpPath
				fmt.Printf("Downloaded from S3: %s → %s\n", fromS3, tmpPath)
			}

			if archivePath == "" {
				return fmt.Errorf("archive-path required (or use --from-s3 <key>)")
			}

			if _, err := os.Stat(archivePath); err != nil {
				return fmt.Errorf("archive not found: %s", archivePath)
			}

			dsn := cfg.Database.PostgresDSN

			if !dryRun && !force {
				fmt.Fprintln(os.Stderr, "ERROR: --force flag is required for restore (destructive operation).")
				fmt.Fprintln(os.Stderr, "       Use --dry-run to preview what would be restored.")
				os.Exit(1)
			}

			if !dryRun {
				// Check for active DB connections before destructive restore.
				if dsn != "" && !skipDB {
					conns, connErr := backup.CheckActiveConnections(cmd.Context(), dsn)
					if connErr == nil && conns > 0 {
						fmt.Fprintf(os.Stderr,
							"WARNING: %d active connection(s) detected on the database.\n"+
								"         Stop the gateway and all clients before restoring.\n", conns)
						if !force {
							os.Exit(1)
						}
					}
				}

				fmt.Printf("Restoring from: %s\n", archivePath)
				if skipDB {
					fmt.Println("  database: skipped")
				}
				if skipFiles {
					fmt.Println("  filesystem: skipped")
				}
			} else {
				fmt.Printf("Dry-run: inspecting archive %s\n", archivePath)
			}

			opts := backup.RestoreOptions{
				ArchivePath:   archivePath,
				DSN:           dsn,
				DataDir:       cfg.ResolvedDataDir(),
				WorkspacePath: cfg.WorkspacePath(),
				DryRun:        dryRun,
				SkipDB:        skipDB,
				SkipFiles:     skipFiles,
				Force:         force,
				ProgressFn: func(phase, detail string) {
					fmt.Printf("  [%s] %s\n", phase, detail)
				},
			}

			result, err := backup.Restore(cmd.Context(), opts)
			if err != nil {
				return fmt.Errorf("restore failed: %w", err)
			}

			fmt.Println()
			if dryRun {
				fmt.Println("Dry-run complete (no changes made):")
			} else {
				fmt.Println("Restore complete:")
			}
			fmt.Printf("  manifest version : %d\n", result.ManifestVersion)
			fmt.Printf("  schema version   : %d\n", result.SchemaVersion)
			fmt.Printf("  database restored: %v\n", result.DatabaseRestored)
			fmt.Printf("  files extracted  : %d (%d MB)\n",
				result.FilesExtracted, result.BytesExtracted>>20)

			for _, w := range result.Warnings {
				fmt.Printf("  WARNING: %s\n", w)
			}

			if result.DatabaseRestored {
				fmt.Println("\nNext steps: run 'goclaw migrate up' if schema version was older than current.")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&skipDB, "skip-db", false, "skip database restore (filesystem only)")
	cmd.Flags().BoolVar(&skipFiles, "skip-files", false, "skip filesystem restore (database only)")
	cmd.Flags().BoolVar(&force, "force", false, "required: confirm destructive restore operation")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "inspect archive and show restore plan without executing")
	cmd.Flags().StringVar(&fromS3, "from-s3", "", "download and restore from this S3 key (e.g. backups/backup-20260409.tar.gz)")
	cmd.Flags().BoolVar(&listS3, "list-s3", false, "list available backups in S3 and exit")

	return cmd
}

// loadS3Client opens the DB, reads S3 config from config_secrets, and returns a client.
func loadS3Client(ctx context.Context, cfg *config.Config) (*backup.S3Client, error) {
	if cfg.Database.PostgresDSN == "" {
		return nil, fmt.Errorf("postgres DSN not configured; set GOCLAW_POSTGRES_DSN")
	}
	db, err := sql.Open("pgx", cfg.Database.PostgresDSN)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	encKey := os.Getenv("GOCLAW_ENCRYPTION_KEY")
	secrets := pg.NewPGConfigSecretsStore(db, encKey)

	s3cfg, err := backup.LoadS3Config(ctx, secrets)
	if err != nil {
		return nil, fmt.Errorf("load s3 config: %w", err)
	}
	if s3cfg == nil {
		return nil, fmt.Errorf("s3 not configured — save credentials via API or CLI first")
	}
	return backup.NewS3Client(s3cfg)
}

// listS3Backups prints available S3 backups to stdout.
func listS3Backups(ctx context.Context, cfg *config.Config) error {
	client, err := loadS3Client(ctx, cfg)
	if err != nil {
		return err
	}
	entries, err := client.ListBackups(ctx)
	if err != nil {
		return fmt.Errorf("list s3 backups: %w", err)
	}
	if len(entries) == 0 {
		fmt.Println("No backups found in S3.")
		return nil
	}
	fmt.Printf("%-60s  %10s  %s\n", "Key", "Size", "Last Modified")
	fmt.Printf("%-60s  %10s  %s\n", "---", "----", "-------------")
	for _, e := range entries {
		fmt.Printf("%-60s  %10s  %s\n",
			e.Key,
			formatBackupSize(e.Size),
			e.LastModified.Format("2006-01-02 15:04:05 UTC"),
		)
	}
	return nil
}

// downloadFromS3 downloads the given S3 key to a temp file and returns its path.
func downloadFromS3(ctx context.Context, cfg *config.Config, key string) (string, error) {
	client, err := loadS3Client(ctx, cfg)
	if err != nil {
		return "", err
	}

	tmp, err := os.CreateTemp("", "goclaw-s3-restore-*.tar.gz")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer tmp.Close()

	fmt.Printf("Downloading s3://%s ...\n", key)
	if err := client.Download(ctx, key, tmp); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

// formatBackupSize returns a human-readable size string.
func formatBackupSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	default:
		return fmt.Sprintf("%d KB", b>>10)
	}
}
