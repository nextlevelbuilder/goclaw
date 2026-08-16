package cmd

import (
	"fmt"
	"io"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/reliability"
)

// healthCmd exposes the reliability-layer diagnostics surface: goclaw health
// dumps live circuit/health/rate-limit/metrics state, goclaw health --check
// runs deterministic in-process regression checks (plan §27 cases A/B/C).
func healthCmd() *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Dump reliability-layer state and run regression checks",
		Run: func(cmd *cobra.Command, args []string) {
			if check {
				os.Exit(runHealthChecks())
			}
			runHealthDump()
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "run in-process reliability regression checks and exit")
	return cmd
}

// ---------------------------------------------------------------------------
// goclaw health — state dump
// ---------------------------------------------------------------------------

func runHealthDump() {
	fmt.Println("Reliability state")
	fmt.Println()

	// Per provider:model health entries.
	entries := providers.RemoteHealthSnapshotAll()
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	if len(entries) == 0 {
		fmt.Println("  no provider activity observed yet")
	} else {
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "PROVIDER:MODEL\tCIRCUIT\tSCORE\tATTEMPTS\tSUCCESSES\tFAILURES\tCOOLDOWN\tRATE-LIMITED-UNTIL\tTIMEOUTS\tSTALLS")
		for _, e := range entries {
			fmt.Fprintf(tw, "%s\t%s\t%.3f\t%d\t%d\t%d\t%s\t%s\t%d\t%d\n",
				e.Key,
				e.CircuitState,
				e.Score,
				e.Attempts,
				e.Successes,
				e.ConsecutiveFailures,
				formatCooldown(e.CooldownRemaining),
				formatTime(e.RateLimitedUntil),
				e.TimeoutCount,
				e.StreamStallCount,
			)
		}
		tw.Flush()
	}

	// Metrics snapshot.
	fmt.Println()
	m := providers.RemoteHealthMetricsAll()
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "METRIC\tCOUNT")
	fmt.Fprintf(tw, "requests\t%d\n", m.Requests)
	fmt.Fprintf(tw, "successes\t%d\n", m.Successes)
	fmt.Fprintf(tw, "retries\t%d\n", m.Retries)
	fmt.Fprintf(tw, "rate_limited\t%d\n", m.RateLimited)
	fmt.Fprintf(tw, "server_errors\t%d\n", m.ServerErrors)
	fmt.Fprintf(tw, "timeouts\t%d\n", m.Timeouts)
	fmt.Fprintf(tw, "stream_stalls\t%d\n", m.StreamStalls)
	fmt.Fprintf(tw, "agent_recovered\t%d\n", m.AgentRecovered)
	fmt.Fprintf(tw, "agent_continued\t%d\n", m.AgentContinued)
	fmt.Fprintf(tw, "premature_completions\t%d\n", m.PrematureCompletes)
	fmt.Fprintf(tw, "loop_detected\t%d\n", m.LoopDetected)
	tw.Flush()

	// Fallback selection policy (process-wide default; per-agent wrappers may
	// carry an explicit WithFallbackPolicy that overrides this view).
	fmt.Println()
	policy := providers.DefaultFallbackPolicyView()
	strategy := policy.Strategy
	if strategy == "" {
		strategy = providers.FallbackStrategyPriority
	}
	minAttempts := policy.MinAttemptsForHealth
	if minAttempts <= 0 {
		minAttempts = 5
	}
	fmt.Printf("Fallback policy\n")
	tw = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "strategy\t%s\n", strategy)
	fmt.Fprintf(tw, "min_attempts_for_health\t%d\n", minAttempts)
	tw.Flush()
}

func formatCooldown(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	return d.Round(time.Second).String()
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format(time.DateTime)
}

// ---------------------------------------------------------------------------
// goclaw health --check — deterministic in-process regression checks
// ---------------------------------------------------------------------------

func runHealthChecks() int {
	fail := 0
	check := func(name string, ok bool, detail string) {
		status := "PASS"
		if !ok {
			status = "FAIL"
			fail++
		}
		fmt.Printf("%s %s\n", status, name)
		if detail != "" {
			fmt.Printf("     %s\n", detail)
		}
	}

	// Case A — fake 429 → cooldown registered on the shared coordinator.
	{
		reg := reliability.Default()
		reg.RateLimit.Record429("fake-provider", "fake-model", 5*time.Second)
		remaining, ok := reg.RateLimit.CooldownFor("fake-provider", "fake-model")
		check("429 cooldown set (case A)",
			ok && remaining > 0 && remaining <= 6*time.Second,
			fmt.Sprintf("cooldown %s", remaining.Round(time.Millisecond)))
	}

	// Case B — simulated stream disconnect → classified retryable.
	// A stream that drops mid-read surfaces a transport-level error from the
	// provider connection (EOF / connection reset), not the run's own context
	// cancellation. That error must classify as retryable so the caller can
	// reconnect instead of aborting the run.
	{
		streamErr := io.ErrUnexpectedEOF
		re, _ := reliability.ClassifyError(streamErr)
		ok := re != nil && re.IsRetryable()
		check("stream disconnect retryable (case B)", ok,
			fmt.Sprintf("classified %s retryable=%v", re.Code, re.IsRetryable()))
	}

	// Case C — false-error guard: nil error is never retryable.
	{
		ok := !reliability.IsRetryable(nil)
		check("nil error not retryable (case C)", ok, "")
	}

	fmt.Println()
	if fail == 0 {
		fmt.Println("All reliability checks passed.")
		return 0
	}
	fmt.Printf("%d reliability check(s) FAILED.\n", fail)
	return 1
}