package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/skills"
)

func skillsGrantUserCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "user [skill-id] [user-id]",
		Short: "Grant a skill to a user",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			skillsRequireGateway()
			path := "/v1/skills/" + url.PathEscape(args[0]) + "/grants/users"
			return runSkillsGateway(cmd.OutOrStdout(), http.MethodPost, path, map[string]any{"user_id": args[1]}, jsonOutput)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func skillsRevokeCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "revoke", Short: "Revoke skill access"}
	cmd.AddCommand(skillsRevokeAgentCmd())
	cmd.AddCommand(skillsRevokeUserCmd())
	return cmd
}

func skillsRevokeAgentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "agent [skill-id] [agent-id]",
		Short: "Revoke a skill from an agent",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			skillsRequireGateway()
			path := "/v1/skills/" + url.PathEscape(args[0]) + "/grants/agents/" + url.PathEscape(args[1])
			return runSkillsGatewayDelete(cmd.OutOrStdout(), path)
		},
	}
}

func skillsRevokeUserCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "user [skill-id] [user-id]",
		Short: "Revoke a skill from a user",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			skillsRequireGateway()
			path := "/v1/skills/" + url.PathEscape(args[0]) + "/grants/users/" + url.PathEscape(args[1])
			return runSkillsGatewayDelete(cmd.OutOrStdout(), path)
		},
	}
}

func runSkillsGateway(w io.Writer, method, path string, body any, jsonOutput bool) error {
	resp, err := skillsGatewayDo(method, path, body)
	if err != nil {
		return err
	}
	if jsonOutput {
		return writePrettyJSON(w, resp)
	}
	if ok, _ := resp["ok"].(bool); ok {
		_, _ = fmt.Fprintln(w, "ok")
		return nil
	}
	return writePrettyJSON(w, resp)
}

func runSkillsGatewayDelete(w io.Writer, path string) error {
	if err := skillsGatewayDelete(path); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(w, "ok")
	return nil
}

func runLocalSkillDepsStatus(w io.Writer, target string, jsonOutput bool) error {
	dir := target
	if filepath.Base(target) == "SKILL.md" {
		dir = filepath.Dir(target)
	}
	manifest := skills.ScanSkillDeps(dir)
	ok, missing := skills.CheckSkillDeps(manifest)
	resp := map[string]any{
		"skill":         map[string]any{"path": dir},
		"ok":            ok,
		"status":        localDepsStatus(ok),
		"manifest":      manifest,
		"missing":       missing,
		"missing_count": len(missing),
	}
	if jsonOutput {
		return writePrettyJSON(w, resp)
	}
	if ok {
		_, _ = fmt.Fprintln(w, "all deps satisfied")
		return nil
	}
	_, _ = fmt.Fprintf(w, "missing dependencies: %s\n", skills.FormatMissing(missing))
	return nil
}

func writePrettyJSON(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func localDepsStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "missing"
}
