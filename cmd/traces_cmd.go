package cmd

import "github.com/spf13/cobra"

const traceExportResponseLimit = 64 << 20

func tracesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "traces",
		Short: "Inspect gateway traces",
	}
	cmd.PersistentFlags().StringVarP(&gatewayOutputFormat, "output", "o", "table", "output format (table|json)")
	cmd.AddCommand(tracesListCmd())
	cmd.AddCommand(tracesGetCmd())
	cmd.AddCommand(tracesExportCmd())
	cmd.AddCommand(tracesFollowCmd())
	cmd.AddCommand(tracesTimelineCmd())
	return cmd
}

func tracesListCmd() *cobra.Command {
	var opts traceListOptions
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List traces",
		RunE: func(cmd *cobra.Command, args []string) error {
			requireRunningGatewayHTTP()
			return runTracesList(opts)
		},
	}
	addTraceListFlags(cmd, &opts)
	return cmd
}

func tracesGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <trace-id>",
		Short: "Get trace details with spans",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requireRunningGatewayHTTP()
			return runTracesGet(args[0])
		},
	}
}

func tracesExportCmd() *cobra.Command {
	var filePath string
	cmd := &cobra.Command{
		Use:   "export <trace-id>",
		Short: "Export a gzipped trace tree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requireRunningGatewayHTTP()
			return runTracesExport(args[0], filePath)
		},
	}
	cmd.Flags().StringVar(&filePath, "file", "", "write gzip export to file (use - for stdout)")
	return cmd
}

func tracesFollowCmd() *cobra.Command {
	var opts traceFollowOptions
	cmd := &cobra.Command{
		Use:   "follow",
		Short: "Poll trace changes for a session or agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			requireRunningGatewayHTTP()
			return runTracesFollow(opts)
		},
	}
	cmd.Flags().StringVar(&opts.SessionKey, "session", "", "filter by session key")
	cmd.Flags().StringVar(&opts.AgentID, "agent-id", "", "filter by agent UUID")
	cmd.Flags().StringVar(&opts.UserID, "user", "", "filter by user ID for admin callers")
	cmd.Flags().StringVar(&opts.Status, "status", "", "filter by trace status")
	cmd.Flags().StringVar(&opts.Channel, "channel", "", "filter by raw channel")
	cmd.Flags().StringVar(&opts.Since, "since", "", "RFC3339 lower bound for changed traces")
	cmd.Flags().IntVar(&opts.Limit, "limit", 0, "page size, max 200")
	cmd.Flags().BoolVar(&opts.IncludeSpans, "include-spans", false, "include spans grouped by trace ID")
	return cmd
}

func tracesTimelineCmd() *cobra.Command {
	var opts traceTimelineOptions
	cmd := &cobra.Command{
		Use:   "timeline <trace-id>",
		Short: "Show the run timeline linked to a trace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requireRunningGatewayHTTP()
			return runTracesTimeline(args[0], opts)
		},
	}
	cmd.Flags().IntVar(&opts.Limit, "limit", 0, "page size, max 500")
	cmd.Flags().IntVar(&opts.Offset, "offset", 0, "pagination offset")
	return cmd
}

func addTraceListFlags(cmd *cobra.Command, opts *traceListOptions) {
	cmd.Flags().StringVarP(&opts.Query, "query", "q", "", "search trace text, IDs, labels, and span previews")
	cmd.Flags().StringVar(&opts.AgentID, "agent-id", "", "filter by agent UUID")
	cmd.Flags().StringVar(&opts.UserID, "user", "", "filter by user ID for admin callers")
	cmd.Flags().StringVar(&opts.SessionKey, "session", "", "filter by session key")
	cmd.Flags().StringVar(&opts.Status, "status", "", "filter by trace status")
	cmd.Flags().StringVar(&opts.Channel, "channel", "", "filter by raw channel")
	cmd.Flags().StringVar(&opts.AgentQuery, "agent", "", "search agent display name or key")
	cmd.Flags().StringVar(&opts.ChannelQuery, "channel-query", "", "search channel instance labels")
	cmd.Flags().StringVar(&opts.ToolName, "tool", "", "search span tool names")
	cmd.Flags().StringVar(&opts.From, "from", "", "start time lower bound, RFC3339")
	cmd.Flags().StringVar(&opts.To, "to", "", "start time upper bound, RFC3339")
	cmd.Flags().StringVar(&opts.Since, "since", "", "alias for --from")
	cmd.Flags().StringVar(&opts.Until, "until", "", "alias for --to")
	cmd.Flags().StringVar(&opts.HasToolCalls, "has-tool-calls", "", "filter true or false")
	cmd.Flags().IntVar(&opts.MinInputTokens, "min-input-tokens", 0, "minimum input tokens")
	cmd.Flags().IntVar(&opts.MaxInputTokens, "max-input-tokens", 0, "maximum input tokens")
	cmd.Flags().IntVar(&opts.MinOutputTokens, "min-output-tokens", 0, "minimum output tokens")
	cmd.Flags().IntVar(&opts.MaxOutputTokens, "max-output-tokens", 0, "maximum output tokens")
	cmd.Flags().IntVar(&opts.MinToolCalls, "min-tool-calls", 0, "minimum tool calls")
	cmd.Flags().IntVar(&opts.MaxToolCalls, "max-tool-calls", 0, "maximum tool calls")
	cmd.Flags().IntVar(&opts.Limit, "limit", 0, "page size, max 200")
	cmd.Flags().IntVar(&opts.Offset, "offset", 0, "pagination offset")
}
