package personal

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
)

func TestRetryTransientRetriesTransientOnly(t *testing.T) {
	old := sendRetryDelays
	sendRetryDelays = []time.Duration{0, 0}
	defer func() { sendRetryDelays = old }()

	attempts := 0
	err := retryTransient(context.Background(), func() error {
		attempts++
		if attempts < 3 {
			return protocol.NewAPIError("send", 503)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retryTransient: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}

	attempts = 0
	err = retryTransient(context.Background(), func() error {
		attempts++
		return protocol.NewAPIError("send", 403)
	})
	if !errors.Is(err, protocol.ErrPolicy) {
		t.Fatalf("err = %v, want ErrPolicy", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 for policy error", attempts)
	}
}
