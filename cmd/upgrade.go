package cmd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/upgrade"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func upgradeCmd() *cobra.Command {
	var dryRun bool
	var status bool

	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade database schema and run data migrations",
		Long:  "Applies pending SQL migrations and Go-based data hooks. Safe to run multiple times (idempotent).",
		RunE: func(cmd *cobra.Command, args []string) error {
			if status {
				return runUpgradeStatus()
			}
			return runUpgrade(dryRun)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be done without applying changes")
	cmd.Flags().BoolVar(&status, "status", false, "show current upgrade status")

	return cmd
}

func runUpgradeStatus() error {
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	fmt.Printf("  App version:     %s (protocol %d)\n", Version, protocol.ProtocolVersion)

	if cfg.Database.PostgresDSN == "" {
		fmt.Println("  Database:        NOT CONFIGURED (set GOCLAW_POSTGRES_DSN)")
		return nil
	}

	db, err := sql.Open("pgx", cfg.Database.PostgresDSN)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer db.Close()

	s, err := upgrade.CheckSchema(db)
	if err != nil {
		return fmt.Errorf("check schema: %w", err)
	}

	fmt.Printf("  Schema current:  %d\n", s.CurrentVersion)
	fmt.Printf("  Schema required: %d\n", s.RequiredVersion)

	if s.Dirty {
		fmt.Println("  Status:          DIRTY (failed migration)")
		fmt.Println()
		fmt.Print(upgrade.FormatError(s))
		return nil
	}

	if s.Compatible {
		fmt.Println("  Status:          UP TO DATE")
	} else if s.CurrentVersion > s.RequiredVersion {
		fmt.Println("  Status:          BINARY TOO OLD")
	} else {
		fmt.Printf("  Status:          UPGRADE NEEDED (%d -> %d)\n", s.CurrentVersion, s.RequiredVersion)
	}

	// Check pending data hooks.
	pending, err := upgrade.PendingHooks(context.Background(), db)
	if err != nil {
		slog.Debug("could not check pending data hooks", "error", err)
	} else if len(pending) > 0 {
		fmt.Printf("\n  Pending data hooks: %d\n", len(pending))
		for _, name := range pending {
			fmt.Printf("    - %s\n", name)
		}
	}

	if s.NeedsMigration {
		fmt.Println()
		fmt.Println("  Run 'goclaw upgrade' to apply all pending changes.")
	}

	return nil
}

func runUpgrade(dryRun bool) error {
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if cfg.Database.PostgresDSN == "" {
		fmt.Println("Database not configured. Set GOCLAW_POSTGRES_DSN to enable migrations.")
		return nil
	}

	dsn := cfg.Database.PostgresDSN

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer db.Close()

	s, err := upgrade.CheckSchema(db)
	if err != nil {
		return fmt.Errorf("check schema: %w", err)
	}

	fmt.Printf("  App version:     %s (protocol %d)\n", Version, protocol.ProtocolVersion)
	fmt.Printf("  Schema current:  %d\n", s.CurrentVersion)
	fmt.Printf("  Schema required: %d\n", s.RequiredVersion)
	fmt.Println()

	if s.Dirty {
		fmt.Print(upgrade.FormatError(s))
		return ErrUpgradeFailed
	}
	if s.CurrentVersion > s.RequiredVersion {
		fmt.Print(upgrade.FormatError(s))
		return ErrUpgradeFailed
	}

	if dryRun {
		if s.NeedsMigration {
			fmt.Printf("  Would apply SQL migrations: v%d -> v%d\n", s.CurrentVersion, s.RequiredVersion)
		} else {
			fmt.Println("  SQL schema is up to date.")
		}

		pending, err := upgrade.PendingHooks(context.Background(), db)
		if err != nil {
			slog.Debug("could not check pending data hooks", "error", err)
		} else if len(pending) > 0 {
			fmt.Printf("  Would run %d data hook(s):\n", len(pending))
			for _, name := range pending {
				fmt.Printf("    - %s\n", name)
			}
		} else {
			fmt.Println("  No pending data hooks.")
		}
		return nil
	}

	// Apply SQL migrations.
	if s.NeedsMigration {
		fmt.Print("  Applying SQL migrations... ")
		m, err := newMigrator(dsn)
		if err != nil {
			return fmt.Errorf("create migrator: %w", err)
		}
		defer m.Close()

		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			fmt.Println("FAILED")
			return fmt.Errorf("migrate up: %w", err)
		}
		v, _, _ := m.Version()
		fmt.Printf("OK (v%d -> v%d)\n", s.CurrentVersion, v)
	} else {
		fmt.Println("  SQL schema is up to date.")
	}

	// Run data hooks.
	fmt.Print("  Running data hooks... ")
	count, err := upgrade.RunPendingHooks(context.Background(), db)
	if err != nil {
		fmt.Println("FAILED")
		return fmt.Errorf("data hooks: %w", err)
	}
	if count > 0 {
		fmt.Printf("%d applied\n", count)
	} else {
		fmt.Println("none pending")
	}

	fmt.Println()
	fmt.Println("  Upgrade complete.")
	return nil
}

// ErrUpgradeFailed is returned when upgrade cannot proceed.
var ErrUpgradeFailed = fmt.Errorf("upgrade cannot proceed")

// checkSchemaOrAutoUpgrade is called from gateway startup to gate on schema compatibility.
// If GOCLAW_AUTO_UPGRADE=true and schema is outdated, it runs the upgrade inline.
func checkSchemaOrAutoUpgrade(dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("schema check: connect: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("schema check: ping: %w", err)
	}

	s, err := upgrade.CheckSchema(db)
	if err != nil {
		return fmt.Errorf("schema check: %w", err)
	}

	if s.Compatible {
		slog.Info("schema check passed", "current", s.CurrentVersion, "required", s.RequiredVersion)
		return nil
	}

	if s.Dirty {
		return errors.New(upgrade.FormatError(s))
	}

	if s.CurrentVersion > s.RequiredVersion {
		return errors.New(upgrade.FormatError(s))
	}

	// Schema is outdated — check if auto-upgrade is enabled.
	if os.Getenv("GOCLAW_AUTO_UPGRADE") == "true" {
		slog.Info("auto-upgrade: applying migrations", "from", s.CurrentVersion, "to", s.RequiredVersion)

		m, mErr := newMigrator(dsn)
		if mErr != nil {
			return fmt.Errorf("auto-upgrade: create migrator: %w", mErr)
		}
		defer m.Close()

		if mErr := m.Up(); mErr != nil && !errors.Is(mErr, migrate.ErrNoChange) {
			return fmt.Errorf("auto-upgrade: migrate up: %w", mErr)
		}

		v, _, _ := m.Version()
		slog.Info("auto-upgrade: SQL migrations applied", "version", v)

		// Run data hooks.
		count, hErr := upgrade.RunPendingHooks(context.Background(), db)
		if hErr != nil {
			return fmt.Errorf("auto-upgrade: data hooks: %w", hErr)
		}
		if count > 0 {
			slog.Info("auto-upgrade: data hooks applied", "count", count)
		}

		slog.Info("auto-upgrade complete")
		return nil
	}

	return errors.New(upgrade.FormatError(s))
}
