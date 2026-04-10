package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/channels/teams/appmanifest"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func teamsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "teams",
		Short: "Microsoft Teams channel management",
	}
	cmd.AddCommand(teamsAppPackageCmd())
	return cmd
}

func teamsAppPackageCmd() *cobra.Command {
	var (
		name        string
		fullName    string
		description string
		developer   string
		botID       string
		iconColor   string
		iconOutline string
		output      string
		toStdout    bool
	)

	cmd := &cobra.Command{
		Use:   "app-package",
		Short: "Generate a Teams app package ZIP for sideloading",
		Long:  "Generates a Teams-compatible app package (manifest.json + icons) for sideloading into Microsoft Teams.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if output == "" && !toStdout {
				return fmt.Errorf("specify --output <file> or --stdout")
			}
			if output != "" && toStdout {
				return fmt.Errorf("--output and --stdout are mutually exclusive")
			}

			cfg, err := config.Load(resolveConfigPath())
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			if botID == "" {
				botID = cfg.Channels.Teams.BotID
			}

			opts := appmanifest.Options{
				BotID:       botID,
				Name:        name,
				FullName:    fullName,
				Description: description,
				Developer:   developer,
			}

			if iconColor != "" {
				data, err := os.ReadFile(iconColor)
				if err != nil {
					return fmt.Errorf("reading color icon: %w", err)
				}
				opts.ColorIcon = data
			}
			if iconOutline != "" {
				data, err := os.ReadFile(iconOutline)
				if err != nil {
					return fmt.Errorf("reading outline icon: %w", err)
				}
				opts.OutlineIcon = data
			}

			zipData, err := appmanifest.GenerateZIP(opts)
			if err != nil {
				return err
			}

			if toStdout {
				_, err = os.Stdout.Write(zipData)
				return err
			}

			if err := os.WriteFile(output, zipData, 0644); err != nil {
				return fmt.Errorf("writing %s: %w", output, err)
			}
			fmt.Fprintf(os.Stderr, "Teams app package written to %s (%d bytes)\n", output, len(zipData))
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "bot display name (required, max 30 chars)")
	cmd.Flags().StringVar(&fullName, "full-name", "", "full bot name (default: same as --name)")
	cmd.Flags().StringVar(&description, "description", "", "short description (max 80 chars)")
	cmd.Flags().StringVar(&developer, "developer", "", "developer name (default: GoClaw)")
	cmd.Flags().StringVar(&botID, "bot-id", "", "override bot ID from config")
	cmd.Flags().StringVar(&iconColor, "icon-color", "", "path to custom 192x192 color icon (PNG)")
	cmd.Flags().StringVar(&iconOutline, "icon-outline", "", "path to custom 32x32 outline icon (PNG)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "output file path")
	cmd.Flags().BoolVar(&toStdout, "stdout", false, "write ZIP to stdout (for piping/Docker)")
	_ = cmd.MarkFlagRequired("name")

	return cmd
}
