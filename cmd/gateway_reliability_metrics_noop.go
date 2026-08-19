//go:build !otel

package cmd

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// wireReliabilityMetrics is a no-op for OTel export when built without the
// "otel" tag, but it still starts the shared 5s flush loop when the
// reliability SLO is enabled so burn-rate webhook alerts work on default
// builds. Build with `go build -tags otel` to additionally export reliability
// counters via OTLP.
func wireReliabilityMetrics(ctx context.Context, cfg *config.Config) (func(), error) {
	if cfg == nil {
		return nil, nil
	}
	tracker := sloTrackerFromConfig(cfg)
	if tracker == nil {
		return nil, nil
	}
	stop, ok := startReliabilityFlushLoop(ctx, cfg, tracker, false)
	if !ok {
		return nil, nil
	}
	return stop, nil
}