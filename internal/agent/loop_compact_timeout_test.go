package agent

import (
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestLoopCompactionTimeoutDefaultsToTwoMinutes(t *testing.T) {
	loop := &Loop{}

	if got := loop.compactionTimeout(); got != 120*time.Second {
		t.Fatalf("compactionTimeout() = %v, want %v", got, 120*time.Second)
	}
}

func TestLoopCompactionTimeoutIgnoresNonPositiveConfig(t *testing.T) {
	for _, timeoutSeconds := range []int{0, -1} {
		loop := &Loop{compactionCfg: &config.CompactionConfig{TimeoutSeconds: timeoutSeconds}}

		if got := loop.compactionTimeout(); got != 120*time.Second {
			t.Fatalf("compactionTimeout() with timeoutSeconds=%d = %v, want %v", timeoutSeconds, got, 120*time.Second)
		}
	}
}

func TestLoopCompactionTimeoutUsesConfiguredSeconds(t *testing.T) {
	loop := &Loop{compactionCfg: &config.CompactionConfig{TimeoutSeconds: 45}}

	if got := loop.compactionTimeout(); got != 45*time.Second {
		t.Fatalf("compactionTimeout() = %v, want %v", got, 45*time.Second)
	}
}
