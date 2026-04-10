package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"text/tabwriter"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func skillsInstallCmd() *cobra.Command {
	var ref string
	cmd := &cobra.Command{
		Use:   "install [name|url]",
		Short: "Install a skill from registry or GitHub",
		Long: `Install a skill from the GoClaw registry (by slug) or directly from a GitHub repo.

Examples:
  goclaw skills install shopee-product-finder
  goclaw skills install github.com/user/repo
  goclaw skills install owner/repo@v1.0 --ref main`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if err := runSkillInstall(args[0], ref); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
	}
	cmd.Flags().StringVar(&ref, "ref", "", "Git ref, tag, or branch to install")
	return cmd
}

func runSkillInstall(input, refOverride string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Parse skill reference.
	skillRef, err := skills.ParseSkillRef(input)
	if err != nil {
		return err
	}

	// Override ref if flag provided.
	if refOverride != "" {
		skillRef.Ref = refOverride
	}

	// Resolve registry slug → owner/repo.
	cfgPath := resolveConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	dataDir := cfg.ResolvedDataDir()

	owner, repo := skillRef.Owner, skillRef.Repo
	if skillRef.IsRegistry {
		fmt.Printf("Resolving %q from registry...\n", input)
		cacheDir := filepath.Join(dataDir, "cache")
		registry := skills.NewRegistryClient(cacheDir)
		var err error
		owner, repo, err = registry.Resolve(ctx, input)
		if err != nil {
			return err
		}
	}

	// Fetch from GitHub.
	fmt.Printf("Fetching %s/%s", owner, repo)
	if skillRef.Ref != "" {
		fmt.Printf("@%s", skillRef.Ref)
	}
	fmt.Println("...")

	tmpDir, err := skills.FetchFromGitHub(ctx, owner, repo, skillRef.Ref)
	if err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Connect DB.
	db, err := connectDBForCLI()
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer db.Close()

	// Build installer.
	skillsStoreDir := filepath.Join(dataDir, "skills-store")
	if err := os.MkdirAll(skillsStoreDir, 0755); err != nil {
		return err
	}

	store := pg.NewPGSkillStore(db, skillsStoreDir)
	loader := loadSkillsLoader()
	installer := skills.NewInstaller(store, skillsStoreDir, loader)

	// Install.
	fmt.Println("Installing...")
	ownerID := fmt.Sprintf("github:%s/%s", owner, repo)
	result, err := installer.Install(ctx, tmpDir, ownerID)
	if err != nil {
		return err
	}

	// Print result.
	fmt.Printf("\nDone! Skill installed:\n")
	fmt.Printf("  Name:    %s\n", result.Name)
	fmt.Printf("  Slug:    %s\n", result.Slug)
	fmt.Printf("  Version: %d\n", result.Version)
	fmt.Printf("  ID:      %s\n", result.ID)
	if result.DepsWarning != "" {
		fmt.Printf("\n  ⚠ Dependencies: %s\n", result.DepsWarning)
	}
	return nil
}

func skillsSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search [query]",
		Short: "Search the skill registry",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ctx := context.Background()
			cfgPath := resolveConfigPath()
			cfg, err := config.Load(cfgPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: load config: %v\n", err)
				os.Exit(1)
			}
			cacheDir := filepath.Join(cfg.ResolvedDataDir(), "cache")
			registry := skills.NewRegistryClient(cacheDir)

			results, err := registry.Search(ctx, args[0])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			if len(results) == 0 {
				fmt.Println("No skills found matching query.")
				return
			}

			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "SLUG\tREPO\tDESCRIPTION\n")
			for _, r := range results {
				desc := r.Description
				if len(desc) > 60 {
					desc = desc[:57] + "..."
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Slug, r.Repo, desc)
			}
			tw.Flush()
		},
	}
}

// connectDBForCLI opens a minimal database connection for CLI commands.
func connectDBForCLI() (*sql.DB, error) {
	cfgPath := resolveConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	dsn := cfg.Database.PostgresDSN
	if dsn == "" {
		return nil, fmt.Errorf("GOCLAW_POSTGRES_DSN is not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("cannot connect to database: %w", err)
	}
	return db, nil
}
