package cmd

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bgalert"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/reliability"
)

// flushLoopTickerInterval is the cadence at which the reliability flush loop
// drains counters and evaluates the SLO. Kept as a named constant so tests can
// reason about it without a magic number.
const flushLoopTickerInterval = 5 * time.Second

// sloTrackerFromConfig constructs the rolling SLO evaluator from config, or
// returns nil when reliability.slo.enabled is false. The target and window
// fall back to their config defaults when unset.
func sloTrackerFromConfig(cfg *config.Config) *reliability.SLOTracker {
	if cfg == nil || !cfg.Reliability.SLO.Enabled {
		return nil
	}
	return reliability.NewSLOTracker(
		cfg.Reliability.SLO.EffectiveSLOTarget(),
		cfg.Reliability.SLO.EffectiveSLOWindow(),
	)
}

// startReliabilityFlushLoop is the single shared 5s flush loop. Exactly one
// flush loop must run per process so the counters are drained once per tick
// and emitted to whatever sink is registered (e.g. the OTel sink wired by the
// otel build). Each tick takes the pre-drain delta, flushes, and folds the
// same delta into the SLO tracker (when configured). When the tracker leaves
// its error budget and alerts are enabled with a webhook URL, it fires an
// out-of-band webhook with the frozen reason "slo_burn_rate".
//
// forceFlush keeps the loop running even without a tracker (sink-only mode,
// required when an OTel sink is registered). Returns (nil, false) when neither
// a sink needs draining nor a tracker is configured.
func startReliabilityFlushLoop(ctx context.Context, cfg *config.Config, tracker *reliability.SLOTracker, forceFlush bool) (func(), bool) {
	if !forceFlush && tracker == nil {
		return nil, false
	}

	webhookURL := effectiveAlertWebhookURL(cfg)
	minInterval := 0
	if cfg != nil {
		minInterval = int(cfg.Reliability.Alerts.EffectiveAlertMinInterval() / time.Second)
	}
	if minInterval <= 0 {
		minInterval = 1 // never allow zero-interval alert storms
	}

	stopCh := make(chan struct{})
	go func() {
		t := time.NewTicker(flushLoopTickerInterval)
		defer t.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-t.C:
				// Take before Flush so the tracker folds the same delta the
				// sink drains, without a second Flush that would drain the
				// counters twice per tick.
				snap := reliability.Default().Metrics.Take()
				reliability.Default().Metrics.Flush()

				if tracker != nil {
					tracker.Observe(snap)
					st := tracker.Status()
					if !st.WithinBudget && webhookURL != "" {
						bgalert.SendWebhook(ctx, bgalert.AlertDeps{
							WebhookURL:         webhookURL,
							MinIntervalSeconds: minInterval,
						}, "slo_monitor", "slo_burn_rate", errors.New("reliability SLO error budget exceeded"))
						slog.Warn("reliability SLO budget exceeded",
							"success_rate", st.SuccessRate,
							"target", st.Target,
							"burn_rate", st.BurnRate,
							"window", st.Window.String(),
						)
					}
				}
			}
		}
	}()

	var once sync.Once
	stop := func() {
		once.Do(func() { close(stopCh) })
	}
	return stop, true
}