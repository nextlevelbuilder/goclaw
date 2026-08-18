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
// provider and starts a 5s flush loop that drains reliability.Default().Metrics
// into the registered sink. It returns a stop function that closes the flush
// loop and shuts down the meter provider. When telemetry is disabled or has no
// endpoint it returns (nil, nil) — no exporter, no goroutine.
func wireReliabilityMetrics(ctx context.Context, cfg *config.Config) (func(), error) {
	if cfg == nil {
		return nil, nil
	}
	if !cfg.Telemetry.Enabled || cfg.Telemetry.Endpoint == "" {
		slog.Debug("reliability OTel metrics available but not enabled (set telemetry.enabled + telemetry.endpoint)")
		return nil, nil
	}

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

	stopCh := make(chan struct{})
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-t.C:
				reliability.Default().Metrics.Flush()
			}
		}
	}()

	var once sync.Once
	stop := func() {
		once.Do(func() {
			close(stopCh)
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
	return stop, nil
}
