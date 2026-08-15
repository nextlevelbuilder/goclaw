package webhooks

import (
	"testing"
	"time"
)

func TestResolveTimeoutSec(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want time.Duration
	}{
		{"zero uses default", 0, 600 * time.Second},
		{"negative uses default", -5, 600 * time.Second},
		{"passthrough", 120, 120 * time.Second},
		{"caps at 3600", 99999, 3600 * time.Second},
		{"exactly cap", 3600, 3600 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveTimeoutSec(c.in); got != c.want {
				t.Fatalf("ResolveTimeoutSec(%d) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
