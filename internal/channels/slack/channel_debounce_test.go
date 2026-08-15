package slack

import (
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestSlackDebounceDelayDefaultAndDisabled(t *testing.T) {
	defaultChannel := newTestSlackChannel(t, config.SlackConfig{})
	if defaultChannel.debounceDelay != 300*time.Millisecond {
		t.Fatalf("default debounce = %s, want 300ms", defaultChannel.debounceDelay)
	}

	disabled := 0
	disabledChannel := newTestSlackChannel(t, config.SlackConfig{DebounceDelay: &disabled})
	if disabledChannel.debounceDelay != 0 {
		t.Fatalf("disabled debounce = %s, want 0", disabledChannel.debounceDelay)
	}

	custom := 750
	customChannel := newTestSlackChannel(t, config.SlackConfig{DebounceDelay: &custom})
	if customChannel.debounceDelay != 750*time.Millisecond {
		t.Fatalf("custom debounce = %s, want 750ms", customChannel.debounceDelay)
	}
}

func newTestSlackChannel(t *testing.T, cfg config.SlackConfig) *Channel {
	t.Helper()
	cfg.Enabled = true
	cfg.BotToken = "xoxb-test"
	cfg.AppToken = "xapp-test"
	ch, err := New(cfg, bus.New(), nil, nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return ch
}
