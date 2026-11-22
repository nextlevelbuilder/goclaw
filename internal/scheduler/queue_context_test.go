package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// startQueuedPair runs "first" and, while it is active, enqueues "second" so
// that second starts from first's completion rather than from its own Enqueue.
// beforeRelease runs just before first is let go; onSecond runs inside second.
func startQueuedPair(
	t *testing.T,
	firstCtx, secondCtx context.Context,
	beforeRelease func(),
	onSecond func(ctx context.Context),
) (first, second <-chan RunOutcome) {
	t.Helper()
	firstStarted := make(chan struct{})
	release := make(chan struct{})

	runFn := func(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		if req.RunID == "first" {
			close(firstStarted)
			<-release
			return &agent.RunResult{}, nil
		}
		onSecond(ctx)
		return &agent.RunResult{}, ctx.Err()
	}

	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 2}})
	t.Cleanup(laneMgr.StopAll)
	cfg := DefaultQueueConfig()
	cfg.DebounceMs = 0
	sq := NewSessionQueue("s", LaneMain, cfg, laneMgr, runFn)

	first = sq.Enqueue(firstCtx, agent.RunRequest{RunID: "first", SessionKey: "s"})
	<-firstStarted
	second = sq.Enqueue(secondCtx, agent.RunRequest{RunID: "second", SessionKey: "s"})
	if beforeRelease != nil {
		beforeRelease()
	}
	close(release)
	return first, second
}

func waitOutcome(t *testing.T, ch <-chan RunOutcome) RunOutcome {
	t.Helper()
	select {
	case out := <-ch:
		return out
	case <-time.After(5 * time.Second):
		t.Fatal("run did not finish")
		return RunOutcome{}
	}
}

type queueTestCtxKey struct{}

func TestSessionQueue_QueuedRunKeepsOwnContextValues(t *testing.T) {
	firstCtx := context.WithValue(context.Background(), queueTestCtxKey{}, "first")
	secondCtx := context.WithValue(context.Background(), queueTestCtxKey{}, "second")

	var got any
	_, second := startQueuedPair(t, firstCtx, secondCtx, nil, func(ctx context.Context) {
		got = ctx.Value(queueTestCtxKey{})
	})
	waitOutcome(t, second)

	if got != "second" {
		t.Fatalf("queued run saw context value %v, want its own (second)", got)
	}
}

func TestSessionQueue_QueuedRunNotCancelledByFirstRequestContext(t *testing.T) {
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()

	var runErr error
	_, second := startQueuedPair(t, firstCtx, context.Background(), cancelFirst, func(ctx context.Context) {
		runErr = ctx.Err()
	})
	out := waitOutcome(t, second)

	if runErr != nil || out.Err != nil {
		t.Fatalf("queued run cancelled by another request's context: run=%v outcome=%v", runErr, out.Err)
	}
}

// A run queued behind another must record the team create lock in its own
// tracker: the caller releases only that one. With a foreign tracker the lock
// stays held forever and every later team_tasks list/search for that team and
// chat blocks in Mutex.Lock.
func TestSessionQueue_QueuedRunReleasesTeamCreateLock(t *testing.T) {
	var teamLock sync.Mutex
	activeStarted := make(chan struct{})
	release := make(chan struct{})

	runFn := func(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		switch req.RunID {
		case "active":
			close(activeStarted)
			<-release
		case "queued":
			// Same steps as team_tasks list.
			if ptd := tools.PendingTeamDispatchFromCtx(ctx); ptd != nil && !ptd.HasListed() {
				teamLock.Lock()
				ptd.SetTeamLock(&teamLock)
				ptd.MarkListed()
			}
		}
		return &agent.RunResult{}, nil
	}

	laneMgr := NewLaneManager([]LaneConfig{{Name: LaneMain, Concurrency: 2}})
	t.Cleanup(laneMgr.StopAll)
	cfg := DefaultQueueConfig()
	cfg.DebounceMs = 0
	sq := NewSessionQueue("s", LaneMain, cfg, laneMgr, runFn)

	// Same as the gateway consumer: every caller gets its own tracker and
	// releases only that one, once its run has finished.
	schedule := func(runID string) (<-chan RunOutcome, *tools.PendingTeamDispatch) {
		ptd := tools.NewPendingTeamDispatch()
		ctx := tools.WithPendingTeamDispatch(context.Background(), ptd)
		return sq.Enqueue(ctx, agent.RunRequest{RunID: runID, SessionKey: "s"}), ptd
	}

	// The session's first request is long gone by the time the others run.
	seedOut, seedPTD := schedule("seed")
	waitOutcome(t, seedOut)
	seedPTD.ReleaseTeamLock()

	activeOut, activePTD := schedule("active")
	<-activeStarted
	queuedOut, queuedPTD := schedule("queued")
	close(release)

	waitOutcome(t, activeOut)
	activePTD.ReleaseTeamLock()
	waitOutcome(t, queuedOut)
	queuedPTD.ReleaseTeamLock()

	if !queuedPTD.HasListed() {
		t.Errorf("queued run did not use its own tracker")
	}
	if !teamLock.TryLock() {
		t.Fatalf("team create lock still held after every run finished and released")
	}
}
