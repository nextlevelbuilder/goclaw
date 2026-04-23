package cmd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/upgrade"
)

var migrationsDir string

func resolveMigrationsDir() string {
	if migrationsDir != "" {
		return migrationsDir
	}
	// Allow env override (used by Docker entrypoint).
	if v := os.Getenv("GOCLAW_MIGRATIONS_DIR"); v != "" {
		return v
	}
	// Default: ./migrations relative to the executable's working directory.
	exe, err := os.Executable()
	if err != nil {
		return "migrations"
	}
	return filepath.Join(filepath.Dir(exe), "migrations")
}

// absoluteToFileURI formats an already-absolute path into an RFC 8089-compliant
// file:// URL. golang-migrate's file source driver rejects Windows paths like
// "file://F:\\project\\migrations" because "F" is parsed as the host and
// ":\\..." as the port. The fix is shape-driven (presence of a drive-letter
// colon at index 1), so no runtime.GOOS branch is needed — the same code is
// correct for POSIX inputs ("/app/x" → "file:///app/x") and for Windows inputs
// on any OS ("F:\\x" → "file:///F:/x"). strings.ReplaceAll covers the case
// where a Windows path is seen on a non-Windows runner (filepath.ToSlash is a
// no-op outside Windows).
func absoluteToFileURI(abs string) string {
	abs = strings.ReplaceAll(filepath.ToSlash(abs), `\`, `/`)
	if len(abs) >= 2 && abs[1] == ':' {
		abs = "/" + abs
	}
	return "file://" + abs
}

// migrationsSourceURL resolves dir to an absolute path and formats it for
// golang-migrate's file source driver.
func migrationsSourceURL(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	return absoluteToFileURI(abs)
}

func newMigrator(dsn string) (*migrate.Migrate, error) {
	dir := resolveMigrationsDir()
	m, err := migrate.New(migrationsSourceURL(dir), dsn)
	if err != nil {
		return nil, fmt.Errorf("create migrator: %w", err)
	}
	return m, nil
}

func resolveDSN() (string, error) {
	// DSN comes from environment only (secret, never in config.json).
	// config.Load also reads GOCLAW_POSTGRES_DSN into cfg.Database.PostgresDSN.
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return "", fmt.Errorf("load config: %w", err)
	}
	dsn := cfg.Database.PostgresDSN
	if dsn == "" {
		return "", fmt.Errorf("GOCLAW_POSTGRES_DSN environment variable is not set")
	}
	return dsn, nil
}

func migrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Database migration management",
	}

	cmd.PersistentFlags().StringVar(&migrationsDir, "migrations-dir", "", "path to migrations directory (default: ./migrations)")

	cmd.AddCommand(migrateUpCmd())
	cmd.AddCommand(migrateDownCmd())
	cmd.AddCommand(migrateVersionCmd())
	cmd.AddCommand(migrateForceCmd())
	cmd.AddCommand(migrateGotoCmd())
	cmd.AddCommand(migrateDropCmd())

	return cmd
}

func migrateUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Apply all pending migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			dsn, err := resolveDSN()
			if err != nil {
				return err
			}
			m, err := newMigrator(dsn)
			if err != nil {
				return err
			}
			defer m.Close()

			if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
				return fmt.Errorf("migrate up: %w", err)
			}

			v, dirty, _ := m.Version()
			slog.Info("migration complete", "version", v, "dirty", dirty)

			// Run pending data hooks after SQL migrations.
			db, dbErr := sql.Open("pgx", dsn)
			if dbErr != nil {
				slog.Warn("could not connect for data hooks", "error", dbErr)
				return nil
			}
			defer db.Close()

			count, hookErr := upgrade.RunPendingHooks(context.Background(), db)
			if hookErr != nil {
				slog.Warn("data hooks failed", "error", hookErr)
			} else if count > 0 {
				slog.Info("data hooks applied", "count", count)
			}

			return nil
		},
	}
}

func migrateDownCmd() *cobra.Command {
	var steps int
	cmd := &cobra.Command{
		Use:   "down",
		Short: "Roll back migrations (default: 1 step)",
		RunE: func(cmd *cobra.Command, args []string) error {
			dsn, err := resolveDSN()
			if err != nil {
				return err
			}
			m, err := newMigrator(dsn)
			if err != nil {
				return err
			}
			defer m.Close()

			if steps <= 0 {
				steps = 1
			}
			if err := m.Steps(-steps); err != nil && !errors.Is(err, migrate.ErrNoChange) {
				return fmt.Errorf("migrate down: %w", err)
			}

			v, dirty, _ := m.Version()
			slog.Info("rollback complete", "version", v, "dirty", dirty)
			return nil
		},
	}
	cmd.Flags().IntVarP(&steps, "steps", "n", 1, "number of steps to roll back")
	return cmd
}

func migrateVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show current migration version",
		RunE: func(cmd *cobra.Command, args []string) error {
			dsn, err := resolveDSN()
			if err != nil {
				return err
			}
			m, err := newMigrator(dsn)
			if err != nil {
				return err
			}
			defer m.Close()

			v, dirty, err := m.Version()
			if err != nil {
				return fmt.Errorf("get version: %w", err)
			}
			fmt.Printf("version: %d, dirty: %v\n", v, dirty)
			return nil
		},
	}
}

func migrateForceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "force <version>",
		Short: "Force set migration version (no migration applied)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			version, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid version: %w", err)
			}
			dsn, err := resolveDSN()
			if err != nil {
				return err
			}
			m, err := newMigrator(dsn)
			if err != nil {
				return err
			}
			defer m.Close()

			if err := m.Force(version); err != nil {
				return fmt.Errorf("force version: %w", err)
			}
			slog.Info("forced version", "version", version)
			return nil
		},
	}
}

func migrateGotoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "goto <version>",
		Short: "Migrate to a specific version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			version, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid version: %w", err)
			}
			dsn, err := resolveDSN()
			if err != nil {
				return err
			}
			m, err := newMigrator(dsn)
			if err != nil {
				return err
			}
			defer m.Close()

			if err := m.Migrate(uint(version)); err != nil && !errors.Is(err, migrate.ErrNoChange) {
				return fmt.Errorf("migrate goto: %w", err)
			}
			slog.Info("migrated to version", "version", version)
			return nil
		},
	}
}

func migrateDropCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "drop",
		Short: "Drop all tables (DANGEROUS)",
		RunE: func(cmd *cobra.Command, args []string) error {
			dsn, err := resolveDSN()
			if err != nil {
				return err
			}
			m, err := newMigrator(dsn)
			if err != nil {
				return err
			}
			defer m.Close()

			if err := m.Drop(); err != nil {
				return fmt.Errorf("drop: %w", err)
			}
			slog.Info("all tables dropped")
			return nil
		},
	}
}
