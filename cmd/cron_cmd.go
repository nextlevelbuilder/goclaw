package cmd

import (
	"encoding/json"
	"fmt"
	"os"
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
	cmd.AddCommand(cronDeleteCmd())
	cmd.AddCommand(cronToggleCmd())
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

	params, _ := json.Marshal(map[string]interface{}{"includeDisabled": showDisabled})
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

	params, _ := json.Marshal(map[string]interface{}{"jobId": jobID, "enabled": enabled})
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
