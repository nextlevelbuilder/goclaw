package tools

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAnnounceQueueCloseDropsPendingDebounce(t *testing.T) {
	drained := make(chan struct{}, 1)
	queue := NewAnnounceQueue(20, 20, func(string, []AnnounceQueueItem, AnnounceMetadata) {
		drained <- struct{}{}
	})
	queue.Enqueue("session", AnnounceQueueItem{SubagentID: "task"}, AnnounceMetadata{})
	if err := queue.CloseContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-drained:
		t.Fatal("pending announce drained after queue close")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestAnnounceQueueCloseContextBoundsActiveDrain(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	queue := NewAnnounceQueue(1, 1, func(string, []AnnounceQueueItem, AnnounceMetadata) {
		close(started)
		<-release
	})
	queue.Enqueue("session", AnnounceQueueItem{SubagentID: "task"}, AnnounceMetadata{})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("announce drain did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := queue.CloseContext(ctx)
	if !errors.Is(err, ErrAnnounceQueueDrainTimeout) {
		t.Fatalf("CloseContext error = %v, want typed drain timeout", err)
	}
	close(release)
	if err := queue.CloseContext(context.Background()); err != nil {
		t.Fatalf("CloseContext retry: %v", err)
	}
}
