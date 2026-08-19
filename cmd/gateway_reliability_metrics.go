//go:build otel

package cmd

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/reliability"
)

// wireReliabilityMetrics wires the reliability counters to an OTLP meter
// provider and starts the shared 5s flush loop (see
// startReliabilityFlushLoop). The loop drains reliability.Default().Metrics
// into the OTel sink each tick and, when the SLO is enabled, folds the delta
// into the rolling tracker and fires slo_burn_rate webhooks on budget
// violations. It returns a stop function that closes the flush loop and shuts
// down the meter provider. When telemetry is disabled or has no endpoint only
// the SLO branch runs; with neither export nor SLO configured it returns
// (nil, nil) — no exporter, no goroutine.
func wireReliabilityMetrics(ctx context.Context, cfg *config.Config) (func(), error) {
	if cfg == nil {
		return nil, nil
	}

	// Construct the tracker first: it needs to be handed to the flush loop
	// before the loop starts so no delta is ever missed.
	tracker := sloTrackerFromConfig(cfg)

	var flushStop func()
	if cfg.Telemetry.Enabled && cfg.Telemetry.Endpoint != "" {
		mp, err := reliability.NewOTelMeterProvider(ctx, reliability.OTelConfig{
			Endpoint:    cfg.Telemetry.Endpoint,
			Protocol:    cfg.Telemetry.Protocol,
			Insecure:    cfg.Telemetry.Insecure,
			ServiceName: cfg.Telemetry.ServiceName,
			Headers:     cfg.Telemetry.Headers,
		})
		if err != nil {
			return nil, err
		}
		sink := reliability.NewOTelSink(mp)
		reliability.RegisterSink(sink)

		stop, ok := startReliabilityFlushLoop(ctx, cfg, tracker, true)
		if !ok {
			stop = func() {}
		}
		flushStop = stop

		var once sync.Once
		stopAll := func() {
			once.Do(func() {
				if flushStop != nil {
					flushStop()
				}
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := mp.Shutdown(shutdownCtx); err != nil {
					slog.Warn("reliability OTel meter provider shutdown", "error", err)
				}
			})
		}

		slog.Info("reliability OTel metrics enabled",
			"endpoint", cfg.Telemetry.Endpoint,
			"protocol", cfg.Telemetry.Protocol,
			"service_name", cfg.Telemetry.ServiceName,
		)
		return stopAll, nil
	}

	// No OTLP export — SLO alerting still runs on its own flush loop when
	// configured.
	if tracker != nil {
		stop, ok := startReliabilityFlushLoop(ctx, cfg, tracker, false)
		if ok {
			slog.Info("reliability SLO alerting enabled (no OTLP export)",
				"target", cfg.Reliability.SLO.EffectiveSLOTarget(),
				"window", cfg.Reliability.SLO.EffectiveSLOWindow().String(),
			)
		}
		return stop, nil
	}

	return nil, nil
}