//go:build !prometheus

package cmd

import (
	"testing"
)

// TestPrometheusNoopWire is a compile-time contract for the default (no
// -tags prometheus) build: wirePrometheusMetrics must be the no-op that
// returns (nil, nil) without touching the database.
func TestPrometheusNoopWire(t *testing.T) {
	stop, err := wirePrometheusMetrics(nil, nil, nil)
	if err != nil {
		t.Fatalf("noop wirePrometheusMetrics returned error: %v", err)
	}
	if stop != nil {
		t.Fatal("noop wirePrometheusMetrics must return nil stop function")
	}
}
