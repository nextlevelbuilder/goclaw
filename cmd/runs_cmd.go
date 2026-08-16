package cmd

import (
	"strconv"

	"github.com/spf13/cobra"
)

// runsCmd exposes durable agent-run record inspection (goclaw run list|get|events).
// Follows the goclaw traces pattern (cmd/traces_*.go).
func runsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Inspect durable agent runs",
	}
	cmd.PersistentFlags().StringVarP(&gatewayOutputFormat, "output", "o", "table", "output format (table|json)")
	cmd.AddCommand(runsListCmd())
	cmd.AddCommand(runsGetCmd())
	cmd.AddCommand(runsEventsCmd())
	return cmd
}

type runsListOptions struct {
	SessionKey string
	Status     string
	Limit      int
	Offset     int
}

func runsListCmd() *cobra.Command {
	var opts runsListOptions
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List durable agent runs",
		RunE: func(cmd *cobra.Command, args []string) error {
			requireRunningGatewayHTTP()
			return runRunsList(opts)
		},
	}
	cmd.Flags().StringVar(&opts.SessionKey, "session", "", "filter by session key")
	cmd.Flags().StringVar(&opts.Status, "status", "", "filter by status (pending|running|compacting|completed|failed|cancelled)")
	cmd.Flags().IntVar(&opts.Limit, "limit", 0, "page size, max 500")
	cmd.Flags().IntVar(&opts.Offset, "offset", 0, "pagination offset")
	return cmd
}

func runsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <run-id>",
		Short: "Get one durable agent run record",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requireRunningGatewayHTTP()
			return runRunsGet(args[0])
		},
	}
}

func runsEventsCmd() *cobra.Command {
	var after int
	var limit int
	var sessionKey string
	cmd := &cobra.Command{
		Use:   "events <run-id>",
		Short: "Replay run timeline events (cursor after seq)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requireRunningGatewayHTTP()
			return runRunsEvents(args[0], after, limit, sessionKey)
		},
	}
	cmd.Flags().IntVar(&after, "after", 0, "only events with seq > this cursor")
	cmd.Flags().IntVar(&limit, "limit", 0, "page size, max 500")
	cmd.Flags().StringVar(&sessionKey, "session", "", "filter by session key")
	return cmd
}

func runsStatusValid(s string) bool {
	switch s {
	case "", "pending", "running", "compacting", "completed", "failed", "cancelled":
		return true
	}
	return false
}

func runsPagination(limit, offset int) (l, o int) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func itoa(n int) string { return strconv.Itoa(n) }
