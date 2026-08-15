package tools

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type blockingWorkstationStream struct {
	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter
	stderrR *io.PipeReader
	stderrW *io.PipeWriter
	killN   atomic.Int64
	once    sync.Once
	done    chan struct{}
}

func newBlockingWorkstationStream() *blockingWorkstationStream {
	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()
	return &blockingWorkstationStream{
		stdoutR: stdoutR,
		stdoutW: stdoutW,
		stderrR: stderrR,
		stderrW: stderrW,
		done:    make(chan struct{}),
	}
}

func (s *blockingWorkstationStream) Stdout() io.Reader { return s.stdoutR }

func (s *blockingWorkstationStream) Stderr() io.Reader { return s.stderrR }

func (s *blockingWorkstationStream) Wait() (int, error) {
	<-s.done
	return 137, errors.New("killed")
}

func (s *blockingWorkstationStream) Kill() error {
	s.killN.Add(1)
	s.once.Do(func() {
		_ = s.stdoutW.CloseWithError(context.Canceled)
		_ = s.stderrW.CloseWithError(context.Canceled)
		close(s.done)
	})
	return nil
}

func TestStreamAndCollectTimeoutKillsBlockedReaders(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	stream := newBlockingWorkstationStream()
	tool := &WorkstationExecTool{}
	ws := &store.Workstation{
		ID:       uuid.New(),
		TenantID: uuid.New(),
	}

	done := make(chan *Result, 1)
	go func() {
		done <- tool.streamAndCollect(ctx, stream, ws, uuid.NewString(), "session-timeout", "sleep 60")
	}()

	select {
	case result := <-done:
		if !result.IsError {
			t.Fatalf("expected timeout result to be an error, got %#v", result)
		}
		if stream.killN.Load() == 0 {
			t.Fatal("expected timed-out stream to be killed")
		}
	case <-time.After(time.Second):
		t.Fatal("streamAndCollect did not return after context timeout")
	}
}
