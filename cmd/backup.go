package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/backup"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func backupCmd() *cobra.Command {
	var (
		outputPath   string
		excludeDB    bool
		excludeFiles bool
		uploadS3     bool
	)

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Create a full system backup (database + filesystem)",
		Long:  "Produces a .tar.gz archive containing a pg_dump of the database and all workspace/data files.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(resolveConfigPath())
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			dsn := cfg.Database.PostgresDSN

			if outputPath == "" {
				ts := time.Now().UTC().Format("20060102-150405")
				outputPath = fmt.Sprintf("./backup-%s.tar.gz", ts)
			}

			fmt.Printf("Starting backup → %s\n", outputPath)
			if excludeDB {
				fmt.Println("  database: excluded")
			}
			if excludeFiles {
				fmt.Println("  filesystem: excluded")
			}

			opts := backup.Options{
				DSN:           dsn,
				DataDir:       cfg.ResolvedDataDir(),
				WorkspacePath: cfg.WorkspacePath(),
				OutputPath:    outputPath,
				CreatedBy:     "cli",
				GoclawVersion: Version,
				ExcludeDB:     excludeDB,
				ExcludeFiles:  excludeFiles,
				ProgressFn: func(phase, detail string) {
					fmt.Printf("  [%s] %s\n", phase, detail)
				},
			}

			manifest, err := backup.Run(cmd.Context(), opts)
			if err != nil {
				return fmt.Errorf("backup failed: %w", err)
			}

			fmt.Printf("\nBackup complete: %s\n", outputPath)
			fmt.Printf("  schema version : %d\n", manifest.SchemaVersion)
			fmt.Printf("  database size  : %d MB\n", manifest.Stats.DatabaseSizeBytes>>20)
			fmt.Printf("  filesystem     : %d files, %d MB\n",
				manifest.Stats.FilesystemFiles,
				manifest.Stats.FilesystemBytes>>20,
			)
			fmt.Printf("  total          : %d MB\n", manifest.Stats.TotalBytes>>20)

			if uploadS3 {
				if err := uploadBackupToS3(cmd.Context(), cfg, outputPath, Version); err != nil {
					fmt.Fprintf(os.Stderr, "\nS3 upload failed: %v\n", err)
					return err
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "output path for .tar.gz (default: ./backup-<timestamp>.tar.gz)")
	cmd.Flags().BoolVar(&excludeDB, "exclude-db", false, "skip database dump (filesystem only)")
	cmd.Flags().BoolVar(&excludeFiles, "exclude-files", false, "skip filesystem archive (database only)")
	cmd.Flags().BoolVar(&uploadS3, "upload-s3", false, "upload backup to S3 after creation (requires s3 config in config_secrets)")

	return cmd
}

// uploadBackupToS3 loads S3 config from the database and uploads the archive.
func uploadBackupToS3(ctx context.Context, cfg *config.Config, archivePath, version string) error {
	if cfg.Database.PostgresDSN == "" {
		return fmt.Errorf("postgres DSN not configured; set GOCLAW_POSTGRES_DSN")
	}
	db, err := sql.Open("pgx", cfg.Database.PostgresDSN)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	encKey := os.Getenv("GOCLAW_ENCRYPTION_KEY")
	secrets := pg.NewPGConfigSecretsStore(db, encKey)
	s3cfg, err := backup.LoadS3Config(ctx, secrets)
	if err != nil {
		return fmt.Errorf("load s3 config: %w", err)
	}
	if s3cfg == nil {
		return fmt.Errorf("s3 not configured — run: goclaw s3-config set")
	}

	client, err := backup.NewS3Client(s3cfg)
	if err != nil {
		return fmt.Errorf("create s3 client: %w", err)
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat archive: %w", err)
	}

	ts := time.Now().UTC().Format("20060102-150405")
	key := fmt.Sprintf("backup-%s-v%s.tar.gz", ts, version)
	if version == "" {
		key = fmt.Sprintf("backup-%s.tar.gz", ts)
	}

	fmt.Printf("\nUploading to S3: %s/%s ...\n", s3cfg.Bucket, key)
	if err := client.Upload(ctx, key, f, info.Size()); err != nil {
		return err
	}
	fmt.Printf("S3 upload complete: s3://%s/%s%s\n", s3cfg.Bucket, s3cfg.Prefix, key)
	return nil
}
