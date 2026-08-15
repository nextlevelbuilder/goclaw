//go:build linux || darwin

package tools

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

func TestDelegationArtifactPublishRejectsFIFOWithoutBlocking(t *testing.T) {
	exchange := newTestDelegationExchange(
		t,
		t.TempDir(),
		uuid.New(),
		uuid.New(),
		DelegationArtifactLimits{},
	)
	fifoPath := filepath.Join(exchange.OutputsHostPath(), "pipe")
	if err := unix.Mkfifo(fifoPath, 0600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	destinationPath := t.TempDir()
	_, err := exchange.Publish(
		context.Background(),
		openTestArtifactRoot(t, destinationPath),
		time.Now(),
	)
	if !errors.Is(err, ErrArtifactNonRegular) {
		t.Fatalf("Publish() error = %v, want non-regular rejection", err)
	}
}
