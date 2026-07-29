package cmd

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/orchestration"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestDrainChildRunsWithRetryReturnsTypedFailure(t *testing.T) {
	admission := orchestration.NewChildRunAdmission(1, 2)
	started := make(chan struct{})
	release := make(chan struct{})
	ticket, err := admission.Enqueue(context.Background(), orchestration.ChildRunConstraints{
		TenantID: store.MasterTenantID,
		TaskID:   uuid.NewString(),
	}, func(context.Context, *orchestration.ChildRunLease) {
		close(started)
		<-release
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ticket.Activate(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("child run did not start")
	}

	err = drainChildRunsWithRetry(admission, 10*time.Millisecond, 10*time.Millisecond)
	if !errors.Is(err, orchestration.ErrChildRunDrainTimeout) {
		t.Fatalf("drain error = %v, want typed timeout", err)
	}

	close(release)
	select {
	case <-ticket.Done():
	case <-time.After(time.Second):
		t.Fatal("child run did not finish after forced termination signal")
	}
	if err := admission.Close(context.Background()); err != nil {
		t.Fatalf("final drain: %v", err)
	}
}
