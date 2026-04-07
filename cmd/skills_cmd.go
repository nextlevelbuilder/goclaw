package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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
	var agentID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all available skills",
		Run: func(cmd *cobra.Command, args []string) {
			// If --agent specified and gateway is running, use HTTP API
			if agentID != "" && isGatewayReachable() {
				runSkillsListHTTP(agentID, jsonOutput)
				return
			}

			// Fallback: filesystem-based skill listing
			loader := loadSkillsLoader()
			allSkills := loader.ListSkills(context.Background())

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
				if runes := []rune(desc); len(runes) > 60 {
					desc = string(runes[:57]) + "..."
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Name, s.Source, desc)
			}
			tw.Flush()
		},
	}
	cmd.Flags().StringVar(&agentID, "agent", "", "agent ID to list skills for (uses gateway API)")
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
			info, ok := loader.GetSkill(context.Background(), args[0])
			if !ok {
				fmt.Fprintf(os.Stderr, "Skill not found: %s\n", args[0])
				os.Exit(1)
			}
			fmt.Printf("Name:        %s\n", info.Name)
			fmt.Printf("Description: %s\n", info.Description)
			fmt.Printf("Source:      %s\n", info.Source)
			fmt.Printf("Location:    %s\n", info.Path)
			fmt.Println()

			content, ok := loader.LoadSkill(context.Background(), args[0])
			if ok {
				fmt.Println("--- Content ---")
				fmt.Println(content)
			}
		},
	}
}

// runSkillsListHTTP fetches skills for a specific agent from the gateway API.
func runSkillsListHTTP(agentID string, jsonOutput bool) {
	resp, err := gatewayHTTPGet("/v1/agents/" + url.PathEscape(agentID) + "/skills")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(data))
		return
	}

	raw, _ := json.Marshal(resp["skills"])
	var skills []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &skills); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing skills: %v\n", err)
		os.Exit(1)
	}

	if len(skills) == 0 {
		fmt.Println("No skills found for this agent.")
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "NAME\tDESCRIPTION\n")
	for _, s := range skills {
		desc := s.Description
		if runes := []rune(desc); len(runes) > 60 {
			desc = string(runes[:57]) + "..."
		}
		fmt.Fprintf(tw, "%s\t%s\n", s.Name, desc)
	}
	tw.Flush()
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
