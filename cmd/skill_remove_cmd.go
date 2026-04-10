package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func skillsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove [slug]",
		Short: "Remove an installed skill",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if err := runSkillRemove(args[0]); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
	}
}

func runSkillRemove(slug string) error {
	ctx := context.Background()

	db, err := connectDBForCLI()
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer db.Close()

	cfgPath := resolveConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	dataDir := cfg.ResolvedDataDir()
	skillsStoreDir := filepath.Join(dataDir, "skills-store")

	store := pg.NewPGSkillStore(db, skillsStoreDir)
	loader := loadSkillsLoader()
	installer := skills.NewInstaller(store, skillsStoreDir, loader)

	if err := installer.Uninstall(ctx, slug); err != nil {
		return err
	}

	fmt.Printf("Skill %q removed.\n", slug)
	return nil
}
