package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
)

func skillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "List and manage skills",
	}
	cmd.AddCommand(skillsListCmd())
	cmd.AddCommand(skillsShowCmd())
	return cmd
}

func skillsListCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all available skills",
		Run: func(cmd *cobra.Command, args []string) {
			loader := loadSkillsLoader()
			allSkills := loader.ListSkills()

			if jsonOutput {
				data, _ := json.MarshalIndent(allSkills, "", "  ")
				fmt.Println(string(data))
				return
			}

			if len(allSkills) == 0 {
				fmt.Println("No skills found.")
				return
			}

			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "NAME\tSOURCE\tDESCRIPTION\n")
			for _, s := range allSkills {
				desc := s.Description
				if len(desc) > 60 {
					desc = desc[:57] + "..."
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Name, s.Source, desc)
			}
			tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func skillsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show [name]",
		Short: "Show details and content of a skill",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			loader := loadSkillsLoader()
			info, ok := loader.GetSkill(args[0])
			if !ok {
				fmt.Fprintf(os.Stderr, "Skill not found: %s\n", args[0])
				os.Exit(1)
			}
			fmt.Printf("Name:        %s\n", info.Name)
			fmt.Printf("Description: %s\n", info.Description)
			fmt.Printf("Source:      %s\n", info.Source)
			fmt.Printf("Location:    %s\n", info.Path)
			fmt.Println()

			content, ok := loader.LoadSkill(args[0])
			if ok {
				fmt.Println("--- Content ---")
				fmt.Println(content)
			}
		},
	}
}

func loadSkillsLoader() *skills.Loader {
	cfgPath := resolveConfigPath()
	cfg, _ := config.Load(cfgPath)
	workspace := config.ExpandHome(cfg.Agents.Defaults.Workspace)
	globalSkillsDir := os.Getenv("GOCLAW_SKILLS_DIR")
	if globalSkillsDir == "" {
		globalSkillsDir = filepath.Join(cfg.ResolvedDataDir(), "skills")
	}
	builtinSkillsDir := os.Getenv("GOCLAW_BUILTIN_SKILLS_DIR")
	if builtinSkillsDir == "" {
		builtinSkillsDir = "/app/bundled-skills"
	}
	return skills.NewLoader(workspace, globalSkillsDir, builtinSkillsDir)
}
