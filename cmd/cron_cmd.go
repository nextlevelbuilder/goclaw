package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func cronCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cron",
		Short: "Manage scheduled cron jobs",
	}
	cmd.AddCommand(cronListCmd())
	cmd.AddCommand(cronCreateCmd())
	cmd.AddCommand(cronDeleteCmd())
	cmd.AddCommand(cronToggleCmd())
	return cmd
}

func cronCreateCmd() *cobra.Command {
	var (
		name     string
		cronExpr string
		every    string
		at       string
		tz       string
		command  string
		argvJSON string
		cwd      string
		timeout  string
		envPairs []string
		deliver  bool
		channel  string
		to       string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a deterministic command cron job (runs a shell command, no LLM)",
		Long: "Create a cron job whose payload is a shell command executed in the gateway\n" +
			"process WITHOUT an LLM turn (zero model tokens). Requires the gateway to have\n" +
			"cron.command_enabled=true.\n\n" +
			"Examples:\n" +
			"  goclaw cron create --name disk-probe --cron '*/15 * * * *' --command 'df -h /'\n" +
			"  goclaw cron create --name backup --at 2026-07-01T09:00:00Z --argv '[\"/opt/backup.sh\"]' --timeout 5m",
		Run: func(cmd *cobra.Command, args []string) {
			if name == "" {
				fmt.Fprintln(os.Stderr, "Error: --name is required")
				os.Exit(1)
			}

			schedule := map[string]any{}
			switch {
			case cronExpr != "":
				schedule["kind"] = "cron"
				schedule["expr"] = cronExpr
				if tz != "" {
					schedule["tz"] = tz
				}
			case every != "":
				d, err := time.ParseDuration(every)
				if err != nil || d <= 0 {
					fmt.Fprintf(os.Stderr, "Error: invalid --every duration %q\n", every)
					os.Exit(1)
				}
				schedule["kind"] = "every"
				schedule["everyMs"] = d.Milliseconds()
			case at != "":
				ts, err := time.Parse(time.RFC3339, at)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: invalid --at time %q (use RFC3339, e.g. 2026-07-01T09:00:00Z)\n", at)
					os.Exit(1)
				}
				schedule["kind"] = "at"
				schedule["atMs"] = ts.UnixMilli()
			default:
				fmt.Fprintln(os.Stderr, "Error: one of --cron, --every, or --at is required")
				os.Exit(1)
			}

			var argv []string
			switch {
			case argvJSON != "":
				if err := json.Unmarshal([]byte(argvJSON), &argv); err != nil {
					fmt.Fprintf(os.Stderr, "Error: --argv must be a JSON array of strings: %v\n", err)
					os.Exit(1)
				}
			case command != "":
				argv = []string{"sh", "-c", command}
			default:
				fmt.Fprintln(os.Stderr, "Error: one of --command or --argv is required")
				os.Exit(1)
			}

			commandSpec := map[string]any{"argv": argv}
			if cwd != "" {
				commandSpec["cwd"] = cwd
			}
			if timeout != "" {
				d, err := time.ParseDuration(timeout)
				if err != nil || d <= 0 {
					fmt.Fprintf(os.Stderr, "Error: invalid --timeout duration %q\n", timeout)
					os.Exit(1)
				}
				commandSpec["timeoutSeconds"] = int(d.Seconds())
			}
			if len(envPairs) > 0 {
				env := map[string]string{}
				for _, kv := range envPairs {
					k, v, ok := strings.Cut(kv, "=")
					if !ok {
						fmt.Fprintf(os.Stderr, "Error: --env must be KEY=VALUE, got %q\n", kv)
						os.Exit(1)
					}
					env[k] = v
				}
				commandSpec["env"] = env
			}

			cronCreateCommandRPC(name, schedule, commandSpec, deliver, channel, to)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "job name (lowercase slug, required)")
	cmd.Flags().StringVar(&cronExpr, "cron", "", "cron expression (5-field), e.g. '*/15 * * * *'")
	cmd.Flags().StringVar(&every, "every", "", "fixed interval as a Go duration, e.g. 15m")
	cmd.Flags().StringVar(&at, "at", "", "one-shot time (RFC3339), e.g. 2026-07-01T09:00:00Z")
	cmd.Flags().StringVar(&tz, "tz", "", "IANA timezone for --cron (e.g. Asia/Seoul)")
	cmd.Flags().StringVar(&command, "command", "", "shell command (run as sh -c)")
	cmd.Flags().StringVar(&argvJSON, "argv", "", `explicit argv as a JSON array, e.g. '["node","x.js"]'`)
	cmd.Flags().StringVar(&cwd, "cwd", "", "working directory")
	cmd.Flags().StringVar(&timeout, "timeout", "", "per-command timeout as a Go duration, e.g. 30s")
	cmd.Flags().StringArrayVar(&envPairs, "env", nil, "environment override KEY=VALUE (repeatable)")
	cmd.Flags().BoolVar(&deliver, "deliver", false, "deliver command output to a channel")
	cmd.Flags().StringVar(&channel, "channel", "", "delivery channel")
	cmd.Flags().StringVar(&to, "to", "", "delivery chat/target ID")
	return cmd
}

func cronListCmd() *cobra.Command {
	var jsonOutput bool
	var showDisabled bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all cron jobs",
		Run: func(cmd *cobra.Command, args []string) {
			cronListRPC(showDisabled, jsonOutput)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().BoolVar(&showDisabled, "all", false, "include disabled jobs")
	return cmd
}

func cronDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete [jobId]",
		Short: "Delete a cron job",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cronDeleteRPC(args[0])
		},
	}
}

func cronToggleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "toggle [jobId] [true|false]",
		Short: "Enable or disable a cron job",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			enabled := args[1] == "true" || args[1] == "1" || args[1] == "on"
			cronToggleRPC(args[0], enabled)
		},
	}
}

// --- RPC implementations ---

func cronListRPC(showDisabled, jsonOutput bool) {
	requireGateway()

	params, _ := json.Marshal(map[string]any{"includeDisabled": showDisabled})
	resp, err := gatewayRPC(protocol.MethodCronList, params)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "Failed: %s\n", resp.Error.Message)
		os.Exit(1)
	}

	raw, _ := json.Marshal(resp.Payload)
	var result struct {
		Jobs []store.CronJob `json:"jobs"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		os.Exit(1)
	}

	printCronJobs(result.Jobs, jsonOutput)
}

func cronCreateCommandRPC(name string, schedule, command map[string]any, deliver bool, channel, to string) {
	requireGateway()

	params, _ := json.Marshal(map[string]any{
		"name":           name,
		"schedule":       schedule,
		"command":        command,
		"deliver":        deliver,
		"deliverChannel": channel,
		"deliverTo":      to,
	})
	resp, err := gatewayRPC(protocol.MethodCronCreate, params)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "Failed: %s\n", resp.Error.Message)
		os.Exit(1)
	}

	raw, _ := json.Marshal(resp.Payload)
	var result struct {
		Job store.CronJob `json:"job"`
	}
	if err := json.Unmarshal(raw, &result); err == nil && result.Job.ID != "" {
		fmt.Printf("Created command cron job %s (%s)\n", result.Job.ID, result.Job.Name)
		return
	}
	fmt.Println("Created command cron job.")
}

func cronDeleteRPC(jobID string) {
	requireGateway()

	params, _ := json.Marshal(map[string]string{"jobId": jobID})
	resp, err := gatewayRPC(protocol.MethodCronDelete, params)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "Failed: %s\n", resp.Error.Message)
		os.Exit(1)
	}
	fmt.Printf("Deleted job %s\n", jobID)
}

func cronToggleRPC(jobID string, enabled bool) {
	requireGateway()

	params, _ := json.Marshal(map[string]any{"jobId": jobID, "enabled": enabled})
	resp, err := gatewayRPC(protocol.MethodCronToggle, params)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if !resp.OK {
		fmt.Fprintf(os.Stderr, "Failed: %s\n", resp.Error.Message)
		os.Exit(1)
	}
	fmt.Printf("Job %s enabled=%v\n", jobID, enabled)
}

// --- Shared display ---

func printCronJobs(jobs []store.CronJob, jsonOutput bool) {
	if jsonOutput {
		data, _ := json.MarshalIndent(jobs, "", "  ")
		fmt.Println(string(data))
		return
	}

	if len(jobs) == 0 {
		fmt.Println("No cron jobs configured.")
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "ID\tNAME\tENABLED\tSCHEDULE\tLAST RUN\n")
	for _, j := range jobs {
		schedule := j.Schedule.Kind
		if j.Schedule.Expr != "" {
			schedule = j.Schedule.Expr
		} else if j.Schedule.EveryMS != nil {
			d := time.Duration(*j.Schedule.EveryMS) * time.Millisecond
			schedule = "every " + d.String()
		}

		lastRun := "never"
		if j.State.LastRunAtMS != nil {
			lastRun = time.UnixMilli(*j.State.LastRunAtMS).Format(time.DateTime)
		}

		idShort := j.ID
		if len(idShort) > 8 {
			idShort = idShort[:8]
		}

		fmt.Fprintf(tw, "%s\t%s\t%v\t%s\t%s\n",
			idShort, j.Name, j.Enabled, schedule, lastRun)
	}
	tw.Flush()
}
