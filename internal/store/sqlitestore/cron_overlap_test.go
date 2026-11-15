//go:build sqlite || sqliteonly

package sqlitestore

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestSQLiteCronStore_RejectOverlappingManualRun(t *testing.T) {
	s, ctx, _ := newTestSQLiteCronStore(t)
	ms := int64(time.Hour / time.Millisecond)
	job, err := s.AddJob(ctx, "overlap", store.CronSchedule{Kind: "every", EveryMS: &ms}, "hello", false, "", "", "", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if job == nil {
		job = mustOnlyJob(t, s, ctx)
	}
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	var calls atomic.Int32
	s.SetOnJob(func(*store.CronJob) (*store.CronJobResult, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return &store.CronJobResult{Content: "ok"}, nil
	})
	go func() {
		ran, _, err := s.RunJob(ctx, job.ID, true)
		if err == nil && !ran {
			err = fmt.Errorf("first run did not execute")
		}
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not start")
	}
	ran, _, secondErr := s.RunJob(ctx, job.ID, true)
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("first run failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first run did not finish")
	}
	if ran || secondErr == nil || calls.Load() != 1 {
		t.Fatalf("overlap accepted: ran=%v err=%v calls=%d; want rejected with one handler call", ran, secondErr, calls.Load())
	}
	if ran, _, err := s.RunJob(ctx, job.ID, true); !ran || err != nil {
		t.Fatalf("run after completion: ran=%v err=%v", ran, err)
	}
	if calls.Load() != 2 {
		t.Fatalf("handler calls after completion = %d, want 2", calls.Load())
	}
}
