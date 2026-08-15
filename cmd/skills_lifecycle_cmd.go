package cmd

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"
)

var (
	skillsGatewayDo      = gatewayHTTPDo
	skillsGatewayDelete  = gatewayHTTPDelete
	skillsRequireGateway = requireRunningGatewayHTTP
)

func skillsDepsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "deps", Short: "Scan, check, and install skill dependencies"}
	cmd.AddCommand(skillsDepsReadCmd("status", http.MethodGet, "Show dependency status"))
	cmd.AddCommand(skillsDepsReadCmd("scan", http.MethodPost, "Scan dependency declarations"))
	cmd.AddCommand(skillsDepsReadCmd("check", http.MethodPost, "Check dependency availability"))
	cmd.AddCommand(skillsDepsInstallCmd())
	return cmd
}

func skillsDepsReadCmd(name, method, short string) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   name + " [skill-id-or-path]",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if pathExists(args[0]) {
				return runLocalSkillDepsStatus(cmd.OutOrStdout(), args[0], jsonOutput)
			}
			skillsRequireGateway()
			path := "/v1/skills/" + url.PathEscape(args[0]) + "/dependencies"
			if method == http.MethodPost {
				path += "/" + name
			}
			return runSkillsGateway(cmd.OutOrStdout(), method, path, nil, jsonOutput)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func skillsDepsInstallCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "install [skill-id]",
		Short: "Install missing dependencies for a managed skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skillsRequireGateway()
			path := "/v1/skills/" + url.PathEscape(args[0]) + "/dependencies/install"
			return runSkillsGateway(cmd.OutOrStdout(), http.MethodPost, path, nil, jsonOutput)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func skillsAccessCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "access", Short: "Manage skill access mode and effective access"}
	cmd.AddCommand(skillsAccessGetCmd())
	cmd.AddCommand(skillsAccessSetCmd())
	cmd.AddCommand(skillsAccessEffectiveCmd())
	return cmd
}

func skillsAccessGetCmd() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "get [skill-id]",
		Short: "Show skill access mode and grants",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skillsRequireGateway()
			path := "/v1/skills/" + url.PathEscape(args[0]) + "/access"
			return runSkillsGateway(cmd.OutOrStdout(), http.MethodGet, path, nil, jsonOutput)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func skillsAccessSetCmd() *cobra.Command {
	var mode string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "set [skill-id]",
		Short: "Set skill access mode",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if mode == "" {
				return fmt.Errorf("--mode is required")
			}
			skillsRequireGateway()
			path := "/v1/skills/" + url.PathEscape(args[0]) + "/access"
			return runSkillsGateway(cmd.OutOrStdout(), http.MethodPatch, path, map[string]any{"mode": mode}, jsonOutput)
		},
	}
	cmd.Flags().StringVar(&mode, "mode", "", "access mode: private, internal, public")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func skillsAccessEffectiveCmd() *cobra.Command {
	var agentID, userID string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "effective [skill-id]",
		Short: "Inspect effective access for an agent and user",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if agentID == "" || userID == "" {
				return fmt.Errorf("--agent and --user are required")
			}
			skillsRequireGateway()
			values := url.Values{"agent_id": {agentID}, "user_id": {userID}}
			path := "/v1/skills/access/effective"
			if len(args) == 1 {
				path = "/v1/skills/" + url.PathEscape(args[0]) + "/access/effective"
			}
			return runSkillsGateway(cmd.OutOrStdout(), http.MethodGet, path+"?"+values.Encode(), nil, jsonOutput)
		},
	}
	cmd.Flags().StringVar(&agentID, "agent", "", "agent ID")
	cmd.Flags().StringVar(&userID, "user", "", "user ID")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func skillsGrantCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "grant", Short: "Grant skill access"}
	cmd.AddCommand(skillsGrantAgentCmd())
	cmd.AddCommand(skillsGrantUserCmd())
	return cmd
}

func skillsGrantAgentCmd() *cobra.Command {
	var canManage, jsonOutput bool
	var pinnedVersion int
	cmd := &cobra.Command{
		Use:   "agent [skill-id] [agent-id]",
		Short: "Grant a skill to an agent",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			skillsRequireGateway()
			body := map[string]any{"agent_id": args[1]}
			if canManage {
				body["can_manage"] = true
			}
			if pinnedVersion > 0 {
				body["pinned_version"] = pinnedVersion
			}
			path := "/v1/skills/" + url.PathEscape(args[0]) + "/grants/agents"
			return runSkillsGateway(cmd.OutOrStdout(), http.MethodPost, path, body, jsonOutput)
		},
	}
	cmd.Flags().BoolVar(&canManage, "can-manage", false, "grant manage permission")
	cmd.Flags().IntVar(&pinnedVersion, "pinned-version", 0, "pin a specific skill version")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}
