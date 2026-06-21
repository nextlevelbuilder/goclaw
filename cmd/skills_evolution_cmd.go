package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func skillsEvolveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evolve",
		Short: "Manage per-skill self-evolution settings",
	}
	cmd.AddCommand(skillsEvolveStatusCmd())
	cmd.AddCommand(skillsEvolveSetCmd("enable", true))
	cmd.AddCommand(skillsEvolveSetCmd("disable", false))
	cmd.AddCommand(skillsEvolveModeCmd())
	return cmd
}

func skillsEvolveStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [skill]",
		Short: "Show self-evolution settings for a skill",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			requireRunningGatewayHTTP()
			skillID := resolveGatewaySkillID(args[0])
			resp, err := gatewayHTTPGet("/v1/skills/" + url.PathEscape(skillID) + "/evolution")
			exitOnErr(err)
			printSkillEvolutionJSON(resp)
		},
	}
}

func skillsEvolveSetCmd(name string, enabled bool) *cobra.Command {
	return &cobra.Command{
		Use:   name + " [skill]",
		Short: name + " self-evolution for a skill",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			requireRunningGatewayHTTP()
			skillID := resolveGatewaySkillID(args[0])
			resp, err := gatewayHTTPDo("PATCH", "/v1/skills/"+url.PathEscape(skillID)+"/evolution", map[string]any{"enabled": enabled})
			exitOnErr(err)
			printSkillEvolutionJSON(resp)
		},
	}
}

func skillsEvolveModeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mode [skill] [suggest_only|auto_analyze]",
		Short: "Set self-evolution mode for a skill",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			requireRunningGatewayHTTP()
			skillID := resolveGatewaySkillID(args[0])
			resp, err := gatewayHTTPDo("PATCH", "/v1/skills/"+url.PathEscape(skillID)+"/evolution", map[string]any{"mode": args[1]})
			exitOnErr(err)
			printSkillEvolutionJSON(resp)
		},
	}
}

func skillsMetricsCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "metrics [skill]",
		Short: "Show recorded usage metrics for a skill",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			requireRunningGatewayHTTP()
			skillID := resolveGatewaySkillID(args[0])
			resp, err := gatewayHTTPGet("/v1/skills/" + url.PathEscape(skillID) + "/metrics")
			exitOnErr(err)
			if jsonOutput {
				printSkillEvolutionJSON(resp)
				return
			}
			fmt.Printf("Total: %.0f\nStarted: %.0f\nSucceeded: %.0f\nFailed: %.0f\nAbandoned: %.0f\nSuccess rate: %.2f\n",
				num(resp["total_calls"]), num(resp["started"]), num(resp["succeeded"]), num(resp["failed"]), num(resp["abandoned"]), num(resp["success_rate"]))
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func skillsActivityCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "activity [skill]",
		Short: "Show recent self-evolution activity for a skill",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			requireRunningGatewayHTTP()
			skillID := resolveGatewaySkillID(args[0])
			resp, err := gatewayHTTPGet("/v1/skills/" + url.PathEscape(skillID) + "/activity")
			exitOnErr(err)
			printSkillEvolutionJSON(resp)
		},
	}
}

func skillsSuggestionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "suggestions",
		Short: "Manage skill improvement suggestions",
	}
	cmd.AddCommand(skillsSuggestionsListCmd())
	cmd.AddCommand(skillsSuggestionStatusCmd("approve"))
	cmd.AddCommand(skillsSuggestionStatusCmd("reject"))
	cmd.AddCommand(skillsSuggestionApplyCmd())
	return cmd
}

func skillsSuggestionsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [skill]",
		Short: "List suggestions for a skill",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			requireRunningGatewayHTTP()
			skillID := resolveGatewaySkillID(args[0])
			resp, err := gatewayHTTPGet("/v1/skills/" + url.PathEscape(skillID) + "/evolution/suggestions")
			exitOnErr(err)
			printSkillEvolutionJSON(resp)
		},
	}
}

func skillsSuggestionStatusCmd(action string) *cobra.Command {
	return &cobra.Command{
		Use:   action + " [skill] [suggestion-id]",
		Short: action + " a skill improvement suggestion",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			requireRunningGatewayHTTP()
			skillID := resolveGatewaySkillID(args[0])
			path := fmt.Sprintf("/v1/skills/%s/evolution/suggestions/%s/%s", url.PathEscape(skillID), url.PathEscape(args[1]), action)
			resp, err := gatewayHTTPPost(path, nil)
			exitOnErr(err)
			printSkillEvolutionJSON(resp)
		},
	}
}

func skillsSuggestionApplyCmd() *cobra.Command {
	var approve bool
	cmd := &cobra.Command{
		Use:   "apply [skill] [suggestion-id]",
		Short: "Apply an approved skill improvement suggestion",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			requireRunningGatewayHTTP()
			skillID := resolveGatewaySkillID(args[0])
			path := fmt.Sprintf("/v1/skills/%s/evolution/suggestions/%s/apply", url.PathEscape(skillID), url.PathEscape(args[1]))
			resp, err := gatewayHTTPPost(path, map[string]any{"approve": approve})
			exitOnErr(err)
			printSkillEvolutionJSON(resp)
		},
	}
	cmd.Flags().BoolVar(&approve, "approve", false, "approve pending suggestion before applying")
	return cmd
}

func resolveGatewaySkillID(input string) string {
	resp, err := gatewayHTTPGet("/v1/skills")
	exitOnErr(err)
	raw, _ := json.Marshal(resp["skills"])
	var skills []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	_ = json.Unmarshal(raw, &skills)
	for _, sk := range skills {
		if sk.ID == input || sk.Slug == input || sk.Name == input {
			return sk.ID
		}
	}
	return input
}

func printSkillEvolutionJSON(v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
	fmt.Print(buf.String())
}

func exitOnErr(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func num(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	default:
		return 0
	}
}

func printSuggestionTable(items []map[string]any) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTATUS\tTYPE\tREASON")
	for _, item := range items {
		fmt.Fprintf(tw, "%v\t%v\t%v\t%v\n", item["id"], item["status"], item["suggestion_type"], item["reason"])
	}
	tw.Flush()
}
