package childrun

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

const childRunTestTimeout = 3 * time.Second

func TestChildRunAdmissionStateTransitionsAndDormantActivation(t *testing.T) {
	controller := NewChildRunAdmission(1, 2)
	closeChildRunController(t, controller)

	blockFirst := make(chan struct{})
	first := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{}, func(context.Context, *ChildRunLease) {
		<-blockFirst
	})
	waitChildRunSignal(t, first.Started(), "first start")

	var ran atomic.Bool
	second, err := controller.Enqueue(context.Background(), ChildRunConstraints{}, func(context.Context, *ChildRunLease) {
		ran.Store(true)
	})
	if err != nil {
		t.Fatalf("Enqueue second: %v", err)
	}
	if got := second.State(); got != ChildRunNew {
		t.Fatalf("state before activation = %q, want %q", got, ChildRunNew)
	}
	if ran.Load() {
		t.Fatal("callback started before activation")
	}
	if err := second.Activate(); err != nil {
		t.Fatalf("Activate second: %v", err)
	}
	if got := second.State(); got != ChildRunQueued {
		t.Fatalf("state while capacity is occupied = %q, want %q", got, ChildRunQueued)
	}

	close(blockFirst)
	waitChildRunSignal(t, second.Started(), "second start")
	waitChildRunSignal(t, second.Done(), "second completion")
	if got := second.State(); got != ChildRunCompleted {
		t.Fatalf("terminal state = %q, want %q", got, ChildRunCompleted)
	}
	if !ran.Load() {
		t.Fatal("callback did not run after activation")
	}
}

func TestChildRunAdmissionEnforcesProcessRootAndParentLimitsAtGrant(t *testing.T) {
	controller := NewChildRunAdmission(3, 8)
	closeChildRunController(t, controller)

	tenant := uuid.New()
	root := uuid.New()
	firstRelease := make(chan struct{})
	firstConstraints := ChildRunConstraints{
		TenantID: tenant, RootAgentID: root, RootLimit: 2,
		TaskID: "first", ParentTaskID: "parent", ParentFanout: 1,
	}
	first := enqueueAndActivateChildRun(t, controller, firstConstraints, func(context.Context, *ChildRunLease) {
		<-firstRelease
	})
	secondConstraints := firstConstraints
	secondConstraints.TaskID = "second"
	second := enqueueAndActivateChildRun(t, controller, secondConstraints, func(context.Context, *ChildRunLease) {})
	waitChildRunSignal(t, first.Started(), "first sibling start")
	assertChildRunNotSignalled(t, second.Started(), "second sibling bypassed immediate-parent fan-out")

	otherRoot := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{
		TenantID: tenant, RootAgentID: uuid.New(), RootLimit: 1, TaskID: "other",
	}, func(context.Context, *ChildRunLease) {})
	waitChildRunSignal(t, otherRoot.Done(), "independent root completion")

	close(firstRelease)
	waitChildRunSignal(t, second.Done(), "second sibling completion")
	if stats := controller.Stats(); stats.Active != 0 {
		t.Fatalf("active after completion = %d, want 0", stats.Active)
	}
}

func TestChildRunAdmissionUsesOldestEligibleWithoutHeadOfLineBlocking(t *testing.T) {
	controller := NewChildRunAdmission(2, 8)
	closeChildRunController(t, controller)

	tenant := uuid.New()
	rootA := uuid.New()
	rootB := uuid.New()
	blockA := make(chan struct{})
	activeA := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{
		TenantID: tenant, RootAgentID: rootA, RootLimit: 1, TaskID: "active-a",
	}, func(context.Context, *ChildRunLease) { <-blockA })
	blockedA := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{
		TenantID: tenant, RootAgentID: rootA, RootLimit: 1, TaskID: "blocked-a",
	}, func(context.Context, *ChildRunLease) {})
	eligibleB := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{
		TenantID: tenant, RootAgentID: rootB, RootLimit: 1, TaskID: "eligible-b",
	}, func(context.Context, *ChildRunLease) {})

	waitChildRunSignal(t, activeA.Started(), "active root A start")
	waitChildRunSignal(t, eligibleB.Done(), "eligible root B completion")
	assertChildRunNotSignalled(t, blockedA.Started(), "root-blocked entry started early")
	close(blockA)
	waitChildRunSignal(t, blockedA.Done(), "blocked root A completion")
}

func TestChildRunAdmissionPreservesFIFOAmongEligibleRequests(t *testing.T) {
	controller := NewChildRunAdmission(1, 4)
	closeChildRunController(t, controller)

	activeRelease := make(chan struct{})
	active := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{}, func(context.Context, *ChildRunLease) {
		<-activeRelease
	})
	waitChildRunSignal(t, active.Started(), "active blocker start")

	firstRelease := make(chan struct{})
	first := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{}, func(context.Context, *ChildRunLease) {
		<-firstRelease
	})
	second := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{}, func(context.Context, *ChildRunLease) {})
	close(activeRelease)
	waitChildRunSignal(t, first.Started(), "oldest eligible start")
	assertChildRunNotSignalled(t, second.Started(), "later eligible request overtook oldest request")
	close(firstRelease)
	waitChildRunSignal(t, second.Done(), "later eligible completion")
}

func TestChildRunAdmissionEnforcesStandardAndPerRootCapsAcrossRootlessWork(t *testing.T) {
	controller := NewChildRunAdmission(32, 40)
	closeChildRunController(t, controller)

	tenant := uuid.New()
	root := uuid.New()
	startedRoot := make(chan struct{}, 21)
	startedRootless := make(chan struct{}, 12)
	release := make(chan struct{})
	var tickets []*ChildRunTicket
	for index := range 21 {
		tickets = append(tickets, enqueueAndActivateChildRun(t, controller, ChildRunConstraints{
			TenantID: tenant, RootAgentID: root, RootLimit: 20,
			TaskID: "root-task-" + string(rune('a'+index)),
		}, func(context.Context, *ChildRunLease) {
			startedRoot <- struct{}{}
			<-release
		}))
	}
	for index := range 12 {
		tickets = append(tickets, enqueueAndActivateChildRun(t, controller, ChildRunConstraints{
			TaskID: "rootless-task-" + string(rune('a'+index)),
		}, func(context.Context, *ChildRunLease) {
			startedRootless <- struct{}{}
			<-release
		}))
	}

	for range 20 {
		waitChildRunSignal(t, startedRoot, "root-scoped admitted callback")
	}
	for range 12 {
		waitChildRunSignal(t, startedRootless, "rootless admitted callback")
	}
	assertChildRunNotSignalled(t, startedRoot, "twenty-first same-root callback bypassed root cap")
	if stats := controller.Stats(); stats.Active != 32 {
		t.Fatalf("active at saturation = %d, want Standard cap 32", stats.Active)
	}

	close(release)
	for _, ticket := range tickets {
		waitChildRunSignal(t, ticket.Done(), "saturated callback completion")
	}
}

func TestChildRunAdmissionQueueCapacityIsExactly128IndependentChains(t *testing.T) {
	controller := NewChildRunAdmission(1, 128)
	closeChildRunController(t, controller)

	activeRelease := make(chan struct{})
	active := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{}, func(context.Context, *ChildRunLease) {
		<-activeRelease
	})
	waitChildRunSignal(t, active.Started(), "queue blocker start")

	queued := make([]*ChildRunTicket, 0, 128)
	for index := range 128 {
		ticket, err := controller.Enqueue(context.Background(), ChildRunConstraints{}, func(context.Context, *ChildRunLease) {})
		if err != nil {
			t.Fatalf("Enqueue pending ticket %d: %v", index, err)
		}
		queued = append(queued, ticket)
	}
	if _, err := controller.Enqueue(context.Background(), ChildRunConstraints{}, func(context.Context, *ChildRunLease) {}); !errors.Is(err, ErrChildRunBusy) {
		t.Fatalf("129th pending Enqueue error = %v, want ErrChildRunBusy", err)
	}
	if stats := controller.Stats(); stats.Pending != 128 {
		t.Fatalf("pending at queue saturation = %d, want 128", stats.Pending)
	}
	for _, ticket := range queued {
		if !ticket.Cancel() {
			t.Fatal("Cancel dormant queued ticket returned false")
		}
	}
	close(activeRelease)
}

func TestChildRunAdmissionBoundsIndependentQueueButContinuationReusesChain(t *testing.T) {
	controller := NewChildRunAdmission(1, 1)
	closeChildRunController(t, controller)

	tenant := uuid.New()
	root := uuid.New()
	var orderMu sync.Mutex
	var order []string
	var completedChildLease *ChildRunLease
	parentStarted := make(chan struct{})
	continueParent := make(chan struct{})

	parent := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{
		TenantID: tenant, RootAgentID: root, RootLimit: 1,
		TaskID: "parent", Depth: 1, MaxDepth: 3,
	}, func(ctx context.Context, lease *ChildRunLease) {
		close(parentStarted)
		<-continueParent
		err := lease.Continue(ctx, ChildRunConstraints{
			TenantID: tenant, RootAgentID: root, RootLimit: 0,
			TaskID: "child", ParentTaskID: "parent", ParentFanout: 1,
			Depth: 2, MaxDepth: 3,
		}, func(_ context.Context, childLease *ChildRunLease) {
			completedChildLease = childLease
			if stats := controller.Stats(); stats.Active != 1 {
				t.Errorf("active during in-place child handoff = %d, want 1", stats.Active)
			}
			controller.mu.Lock()
			rootCount := controller.rootActive[childRunRoot{tenant: tenant, agent: root}]
			parentCount := controller.parentActive[childRunParent{
				root:   childRunRoot{tenant: tenant, agent: root},
				parent: "parent",
			}]
			controller.mu.Unlock()
			if rootCount != 1 || parentCount != 1 {
				t.Errorf("handoff counters root=%d parent=%d, want 1/1", rootCount, parentCount)
			}
			orderMu.Lock()
			order = append(order, "child")
			orderMu.Unlock()
		})
		if err != nil {
			t.Errorf("Continue: %v", err)
			return
		}
		orderMu.Lock()
		order = append(order, "parent")
		orderMu.Unlock()
	})
	waitChildRunSignal(t, parentStarted, "parent start")

	independent := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{
		TaskID: "independent",
	}, func(context.Context, *ChildRunLease) {
		orderMu.Lock()
		order = append(order, "independent")
		orderMu.Unlock()
	})
	if _, err := controller.Enqueue(context.Background(), ChildRunConstraints{}, func(context.Context, *ChildRunLease) {}); !errors.Is(err, ErrChildRunBusy) {
		t.Fatalf("second independent pending Enqueue error = %v, want ErrChildRunBusy", err)
	}

	close(continueParent)
	waitChildRunSignal(t, parent.Done(), "parent continuation completion")
	waitChildRunSignal(t, independent.Done(), "independent completion")
	if completedChildLease == nil || completedChildLease.frame.state != ChildRunCompleted {
		t.Fatalf("normal continuation state = %v, want %q", completedChildLease, ChildRunCompleted)
	}
	orderMu.Lock()
	defer orderMu.Unlock()
	want := []string{"child", "parent", "independent"}
	if len(order) != len(want) {
		t.Fatalf("execution order = %v, want %v", order, want)
	}
	for index := range want {
		if order[index] != want[index] {
			t.Fatalf("execution order = %v, want %v", order, want)
		}
	}
}

func TestChildRunAdmissionContinuationValidatesLineageAndDepth(t *testing.T) {
	controller := NewChildRunAdmission(1, 4)
	closeChildRunController(t, controller)

	result := make(chan error, 4)
	ticket := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{
		TaskID: "parent", Depth: 1, MaxDepth: 1,
	}, func(ctx context.Context, lease *ChildRunLease) {
		result <- lease.Continue(ctx, ChildRunConstraints{
			TaskID: "wrong-lineage", ParentTaskID: "someone-else", Depth: 2, MaxDepth: 2,
		}, func(context.Context, *ChildRunLease) {})
		result <- lease.Continue(ctx, ChildRunConstraints{
			TaskID: "too-deep", ParentTaskID: "parent", Depth: 2, MaxDepth: 1,
		}, func(context.Context, *ChildRunLease) {})
		result <- lease.Continue(ctx, ChildRunConstraints{
			TaskID: "zero-depth", ParentTaskID: "parent", Depth: 0,
		}, func(context.Context, *ChildRunLease) {})
		result <- lease.Continue(ctx, ChildRunConstraints{
			TenantID: uuid.New(), TaskID: "other-tenant", ParentTaskID: "parent", Depth: 2,
		}, func(context.Context, *ChildRunLease) {})
	})
	waitChildRunSignal(t, ticket.Done(), "lineage validation callback completion")

	if err := waitChildRunValue(t, result, "invalid lineage result"); !errors.Is(err, ErrChildRunInvalidContinuation) {
		t.Fatalf("invalid lineage error = %v, want ErrChildRunInvalidContinuation", err)
	}
	if err := waitChildRunValue(t, result, "depth result"); !errors.Is(err, ErrChildRunContinuationDepth) {
		t.Fatalf("depth error = %v, want ErrChildRunContinuationDepth", err)
	}
	if err := waitChildRunValue(t, result, "zero-depth result"); !errors.Is(err, ErrChildRunInvalidContinuation) {
		t.Fatalf("zero-depth error = %v, want ErrChildRunInvalidContinuation", err)
	}
	if err := waitChildRunValue(t, result, "tenant transition result"); !errors.Is(err, ErrChildRunInvalidContinuation) {
		t.Fatalf("tenant transition error = %v, want ErrChildRunInvalidContinuation", err)
	}
}

func TestChildRunAdmissionContinuationPanicResumesParent(t *testing.T) {
	controller := NewChildRunAdmission(1, 4)
	closeChildRunController(t, controller)

	var resumed atomic.Bool
	var continuationErr error
	var failedChildLease *ChildRunLease
	parent := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{
		TaskID: "parent", Depth: 1, MaxDepth: 3,
	}, func(ctx context.Context, lease *ChildRunLease) {
		continuationErr = lease.Continue(ctx, ChildRunConstraints{
			TaskID: "child", ParentTaskID: "parent", Depth: 2, MaxDepth: 3,
		}, func(_ context.Context, childLease *ChildRunLease) {
			failedChildLease = childLease
			panic("nested boom")
		})
		resumed.Store(true)
	})
	waitChildRunSignal(t, parent.Done(), "parent resume after nested panic")
	if !resumed.Load() {
		t.Fatal("parent did not resume after nested callback panic")
	}
	if continuationErr == nil {
		t.Fatal("nested callback panic did not return an error")
	}
	if failedChildLease == nil || failedChildLease.frame.state != ChildRunFailed {
		t.Fatalf("panicking continuation state = %v, want %q", failedChildLease, ChildRunFailed)
	}
	if got := parent.State(); got != ChildRunCompleted {
		t.Fatalf("parent state after handled nested panic = %q, want %q", got, ChildRunCompleted)
	}
	if stats := controller.Stats(); stats.Active != 0 || stats.Pending != 0 {
		t.Fatalf("stats after nested panic = %+v, want zero active/pending", stats)
	}
}

func TestChildRunAdmissionQueuedContinuationContextCancellationResumesParent(t *testing.T) {
	controller := NewChildRunAdmission(2, 4)
	closeChildRunController(t, controller)

	tenant := uuid.New()
	rootA := uuid.New()
	rootB := uuid.New()
	rootBRelease := make(chan struct{})
	rootBOwner := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{
		TenantID: tenant, RootAgentID: rootB, RootLimit: 1, TaskID: "root-b-owner",
	}, func(context.Context, *ChildRunLease) {
		<-rootBRelease
	})
	waitChildRunSignal(t, rootBOwner.Started(), "root B owner start")

	childCtx, cancelChild := context.WithCancel(context.Background())
	var childRan atomic.Bool
	var continuationErr error
	var parentResumed atomic.Bool
	parent := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{
		TenantID: tenant, RootAgentID: rootA, RootLimit: 1,
		TaskID: "parent-a", Depth: 1, MaxDepth: 3,
	}, func(_ context.Context, lease *ChildRunLease) {
		continuationErr = lease.Continue(childCtx, ChildRunConstraints{
			TenantID: tenant, RootAgentID: rootB, RootLimit: 1,
			TaskID: "child-b", ParentTaskID: "parent-a", ParentFanout: 1,
			Depth: 2, MaxDepth: 3,
		}, func(context.Context, *ChildRunLease) {
			childRan.Store(true)
		})
		parentResumed.Store(true)
	})
	waitChildRunSignal(t, parent.Started(), "parent A start")
	waitForChildRunCondition(t, "queued continuation state", func() bool {
		return parent.State() == ChildRunWaitingChild && controller.Stats().Pending == 1
	})
	cancelChild()
	waitChildRunSignal(t, parent.Done(), "parent resume after child context cancellation")
	if childRan.Load() {
		t.Fatal("cancelled queued continuation callback ran")
	}
	if !parentResumed.Load() {
		t.Fatal("parent did not resume after queued child context cancellation")
	}
	if !errors.Is(continuationErr, context.Canceled) {
		t.Fatalf("continuation error = %v, want context.Canceled", continuationErr)
	}
	close(rootBRelease)
}

func TestChildRunAdmissionSerializesConcurrentSynchronousContinuations(t *testing.T) {
	controller := NewChildRunAdmission(1, 4)
	closeChildRunController(t, controller)

	tenant := uuid.New()
	root := uuid.New()
	var active atomic.Int32
	var maximum atomic.Int32
	var calls atomic.Int32
	childStarted := make(chan struct{}, 2)
	releaseChildren := make(chan struct{})

	parent := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{
		TenantID: tenant, RootAgentID: root, RootLimit: 1,
		TaskID: "parent", Depth: 1, MaxDepth: 3,
	}, func(ctx context.Context, lease *ChildRunLease) {
		var group sync.WaitGroup
		for index := range 2 {
			index := index
			group.Go(func() {
				err := lease.Continue(ctx, ChildRunConstraints{
					TenantID: tenant, RootAgentID: root, RootLimit: 1,
					TaskID: "child-" + string(rune('a'+index)), ParentTaskID: "parent",
					ParentFanout: 1, Depth: 2, MaxDepth: 3,
				}, func(context.Context, *ChildRunLease) {
					current := active.Add(1)
					for {
						observed := maximum.Load()
						if current <= observed || maximum.CompareAndSwap(observed, current) {
							break
						}
					}
					calls.Add(1)
					childStarted <- struct{}{}
					<-releaseChildren
					active.Add(-1)
				})
				if err != nil {
					t.Errorf("Continue %d: %v", index, err)
				}
			})
		}
		waitChildRunSignal(t, childStarted, "first serialized child start")
		assertChildRunNotSignalled(t, childStarted, "second synchronous child overlapped first")
		close(releaseChildren)
		group.Wait()
	})
	waitChildRunSignal(t, parent.Done(), "serialized continuation parent completion")
	if got := calls.Load(); got != 2 {
		t.Fatalf("child callback count = %d, want 2", got)
	}
	if got := maximum.Load(); got != 1 {
		t.Fatalf("maximum concurrent synchronous children = %d, want 1", got)
	}
}

func TestChildRunAdmissionConstraintChangingContinuationReleasesParentCapacity(t *testing.T) {
	controller := NewChildRunAdmission(2, 8)
	closeChildRunController(t, controller)

	tenant := uuid.New()
	rootA := uuid.New()
	rootB := uuid.New()
	rootBRelease := make(chan struct{})
	rootBOwner := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{
		TenantID: tenant, RootAgentID: rootB, RootLimit: 1, TaskID: "root-b-owner",
	}, func(context.Context, *ChildRunLease) {
		<-rootBRelease
	})
	waitChildRunSignal(t, rootBOwner.Started(), "root B owner start")

	parentEntered := make(chan struct{})
	childBStarted := make(chan struct{})
	parentResumed := make(chan struct{})
	parent := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{
		TenantID: tenant, RootAgentID: rootA, RootLimit: 1,
		TaskID: "parent-a", Depth: 1, MaxDepth: 3,
	}, func(ctx context.Context, lease *ChildRunLease) {
		close(parentEntered)
		err := lease.Continue(ctx, ChildRunConstraints{
			TenantID: tenant, RootAgentID: rootB, RootLimit: 1,
			TaskID: "child-b", ParentTaskID: "parent-a", ParentFanout: 1,
			Depth: 2, MaxDepth: 3,
		}, func(context.Context, *ChildRunLease) {
			close(childBStarted)
		})
		if err != nil {
			t.Errorf("Continue to root B: %v", err)
			return
		}
		close(parentResumed)
	})
	waitChildRunSignal(t, parentEntered, "root A parent start")

	unrelated := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{
		TenantID: tenant, RootAgentID: uuid.New(), RootLimit: 1, TaskID: "unrelated",
	}, func(context.Context, *ChildRunLease) {})
	waitChildRunSignal(t, unrelated.Done(), "unrelated work while parent is suspended")
	assertChildRunNotSignalled(t, childBStarted, "root B child bypassed saturated root")

	close(rootBRelease)
	waitChildRunSignal(t, childBStarted, "root B child start")
	waitChildRunSignal(t, parentResumed, "root A parent resume")
	waitChildRunSignal(t, parent.Done(), "root A parent completion")
}

func TestChildRunAdmissionCancellationBeforeGrantRemovesWaiter(t *testing.T) {
	controller := NewChildRunAdmission(1, 2)
	closeChildRunController(t, controller)

	block := make(chan struct{})
	active := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{}, func(context.Context, *ChildRunLease) {
		<-block
	})
	waitChildRunSignal(t, active.Started(), "active run start")

	var ran atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())
	queued, err := controller.Enqueue(ctx, ChildRunConstraints{}, func(context.Context, *ChildRunLease) {
		ran.Store(true)
	})
	if err != nil {
		t.Fatalf("Enqueue queued: %v", err)
	}
	if err := queued.Activate(); err != nil {
		t.Fatalf("Activate queued: %v", err)
	}
	cancel()
	waitChildRunSignal(t, queued.Done(), "queued cancellation")
	if got := queued.State(); got != ChildRunCancelled {
		t.Fatalf("queued state = %q, want %q", got, ChildRunCancelled)
	}
	if ran.Load() {
		t.Fatal("cancelled queued callback ran")
	}
	if stats := controller.Stats(); stats.Pending != 0 {
		t.Fatalf("pending after cancellation = %d, want 0", stats.Pending)
	}
	close(block)
}

func TestChildRunAdmissionGrantWinningCancellationRetainsLeaseUntilReturn(t *testing.T) {
	controller := NewChildRunAdmission(1, 2)
	closeChildRunController(t, controller)

	callbackStarted := make(chan struct{})
	callbackRelease := make(chan struct{})
	var contextCancelled atomic.Bool
	first := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{}, func(ctx context.Context, lease *ChildRunLease) {
		close(callbackStarted)
		<-ctx.Done()
		contextCancelled.Store(true)
		<-callbackRelease
	})
	waitChildRunSignal(t, callbackStarted, "cancellation-resistant callback start")

	second := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{}, func(context.Context, *ChildRunLease) {})
	if !first.Cancel() {
		t.Fatal("Cancel running ticket returned false")
	}
	if got := first.State(); got != ChildRunCancelled {
		t.Fatalf("running cancellation state = %q, want %q", got, ChildRunCancelled)
	}
	waitForChildRunCondition(t, "callback context cancellation", contextCancelled.Load)
	if stats := controller.Stats(); stats.Active != 1 {
		t.Fatalf("active after early Release calls = %d, want 1", stats.Active)
	}
	assertChildRunNotSignalled(t, second.Started(), "second run started before cancelled callback returned")

	close(callbackRelease)
	waitChildRunSignal(t, first.Done(), "cancelled callback return")
	waitChildRunSignal(t, second.Done(), "second callback completion")
}

func TestChildRunAdmissionLeaseReleaseReturnsCapacityBeforeCallbackReturn(t *testing.T) {
	controller := NewChildRunAdmission(1, 2)
	closeChildRunController(t, controller)

	callbackStarted := make(chan struct{})
	leaseReleased := make(chan struct{})
	callbackRelease := make(chan struct{})
	first := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{}, func(_ context.Context, lease *ChildRunLease) {
		close(callbackStarted)
		lease.Release()
		lease.Release()
		close(leaseReleased)
		<-callbackRelease
	})
	waitChildRunSignal(t, callbackStarted, "first callback start")
	waitChildRunSignal(t, leaseReleased, "lease release")
	if stats := controller.Stats(); stats.Active != 0 {
		t.Fatalf("active after Release calls = %d, want 0", stats.Active)
	}

	second := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{}, func(context.Context, *ChildRunLease) {})
	waitChildRunSignal(t, second.Done(), "second run after explicit release")
	if stats := controller.Stats(); stats.Live != 1 {
		t.Fatalf("live tickets while first callback remains open = %d, want 1", stats.Live)
	}
	assertChildRunNotSignalled(t, first.Done(), "ticket completed before callback return")

	close(callbackRelease)
	waitChildRunSignal(t, first.Done(), "first callback return")
}

func TestChildRunAdmissionCloseCancelsReleasedLiveCallbackAndDrains(t *testing.T) {
	controller := NewChildRunAdmission(1, 2)

	callbackStarted := make(chan struct{})
	leaseReleased := make(chan struct{})
	callbackCancelled := make(chan struct{})
	ticket := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{}, func(ctx context.Context, lease *ChildRunLease) {
		close(callbackStarted)
		lease.Release()
		close(leaseReleased)
		<-ctx.Done()
		close(callbackCancelled)
	})
	waitChildRunSignal(t, callbackStarted, "callback start")
	waitChildRunSignal(t, leaseReleased, "lease release")

	if stats := controller.Stats(); stats.Active != 0 || stats.Live != 1 {
		t.Fatalf("stats after lease release = %+v, want active=0 live=1", stats)
	}

	closeCtx, cancelClose := context.WithTimeout(context.Background(), time.Second)
	defer cancelClose()
	if err := controller.Close(closeCtx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	waitChildRunSignal(t, callbackCancelled, "callback cancellation")
	waitChildRunSignal(t, ticket.Done(), "ticket completion")
	if got := ticket.State(); got != ChildRunCancelled {
		t.Fatalf("ticket state = %q, want %q", got, ChildRunCancelled)
	}
	if err := ticket.Err(); !errors.Is(err, ErrChildRunClosed) {
		t.Fatalf("ticket error = %v, want ErrChildRunClosed", err)
	}
	if stats := controller.Stats(); !stats.Closed || stats.Active != 0 || stats.Pending != 0 || stats.Live != 0 {
		t.Fatalf("final stats = %+v, want closed and drained", stats)
	}
}

func TestChildRunAdmissionCancellationDuringTransferredChildReleasesExactlyOnce(t *testing.T) {
	controller := NewChildRunAdmission(1, 4)
	closeChildRunController(t, controller)

	tenant := uuid.New()
	root := uuid.New()
	childStarted := make(chan struct{})
	childRelease := make(chan struct{})
	parent := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{
		TenantID: tenant, RootAgentID: root, RootLimit: 1,
		TaskID: "parent", Depth: 1, MaxDepth: 3,
	}, func(ctx context.Context, lease *ChildRunLease) {
		_ = lease.Continue(ctx, ChildRunConstraints{
			TenantID: tenant, RootAgentID: root, RootLimit: 1,
			TaskID: "child", ParentTaskID: "parent", ParentFanout: 1,
			Depth: 2, MaxDepth: 3,
		}, func(context.Context, *ChildRunLease) {
			close(childStarted)
			<-childRelease
		})
	})
	waitChildRunSignal(t, childStarted, "transferred child start")
	if !parent.Cancel() {
		t.Fatal("Cancel parent during child returned false")
	}
	if stats := controller.Stats(); stats.Active != 1 {
		t.Fatalf("active while cancellation-resistant child runs = %d, want 1", stats.Active)
	}
	close(childRelease)
	waitChildRunSignal(t, parent.Done(), "cancelled continuation chain completion")
	if stats := controller.Stats(); stats.Active != 0 || stats.Pending != 0 {
		t.Fatalf("stats after cancelled continuation = %+v, want zero active/pending", stats)
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if len(controller.rootActive) != 0 || len(controller.parentActive) != 0 {
		t.Fatalf("constraint counters leaked: root=%v parent=%v", controller.rootActive, controller.parentActive)
	}
}

func TestChildRunAdmissionPanicReleasesCapacityAndMarksFailed(t *testing.T) {
	controller := NewChildRunAdmission(1, 2)
	closeChildRunController(t, controller)

	failed := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{}, func(context.Context, *ChildRunLease) {
		panic("boom")
	})
	next := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{}, func(context.Context, *ChildRunLease) {})
	waitChildRunSignal(t, failed.Done(), "panicking callback completion")
	if got := failed.State(); got != ChildRunFailed {
		t.Fatalf("panic state = %q, want %q", got, ChildRunFailed)
	}
	if failed.Err() == nil {
		t.Fatal("panic did not record an error")
	}
	waitChildRunSignal(t, next.Done(), "post-panic callback completion")
}

func TestChildRunAdmissionCloseIsIdempotentDrainsAndStopsDispatcher(t *testing.T) {
	controller := NewChildRunAdmission(1, 2)
	callbackStarted := make(chan struct{})
	callbackRelease := make(chan struct{})
	active := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{}, func(context.Context, *ChildRunLease) {
		close(callbackStarted)
		<-callbackRelease
	})
	waitChildRunSignal(t, callbackStarted, "active callback start")

	var queuedRan atomic.Bool
	queued := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{}, func(context.Context, *ChildRunLease) {
		queuedRan.Store(true)
	})
	closeCtx, cancelClose := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelClose()
	if err := controller.Close(closeCtx); !errors.Is(err, ErrChildRunDrainTimeout) {
		t.Fatalf("first Close error = %v, want ErrChildRunDrainTimeout", err)
	}
	waitChildRunSignal(t, queued.Done(), "queued close cancellation")
	if queuedRan.Load() {
		t.Fatal("queued callback ran during close")
	}
	if stats := controller.Stats(); stats.Active != 1 || !stats.Closed {
		t.Fatalf("stats after timed-out close = %+v", stats)
	}
	if _, err := controller.Enqueue(context.Background(), ChildRunConstraints{}, func(context.Context, *ChildRunLease) {}); !errors.Is(err, ErrChildRunClosed) {
		t.Fatalf("Enqueue after Close error = %v, want ErrChildRunClosed", err)
	}

	close(callbackRelease)
	waitChildRunSignal(t, active.Done(), "active callback drain")
	if err := controller.Close(context.Background()); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	waitChildRunSignal(t, controller.dispatcherDone, "dispatcher stop")
	if err := controller.Close(context.Background()); err != nil {
		t.Fatalf("third Close: %v", err)
	}
}

func TestChildRunAdmissionConcurrentCloseCallsShareOneDrain(t *testing.T) {
	controller := NewChildRunAdmission(1, 1)
	callbackStarted := make(chan struct{})
	callbackRelease := make(chan struct{})
	ticket := enqueueAndActivateChildRun(t, controller, ChildRunConstraints{}, func(context.Context, *ChildRunLease) {
		close(callbackStarted)
		<-callbackRelease
	})
	waitChildRunSignal(t, callbackStarted, "callback start before concurrent close")

	closeResults := make(chan error, 2)
	startClose := make(chan struct{})
	for range 2 {
		go func() {
			<-startClose
			closeResults <- controller.Close(context.Background())
		}()
	}
	close(startClose)
	waitForChildRunCondition(t, "controller closed by concurrent caller", func() bool {
		return controller.Stats().Closed
	})
	close(callbackRelease)
	waitChildRunSignal(t, ticket.Done(), "callback drain under concurrent close")
	for index := range 2 {
		if err := waitChildRunValue(t, closeResults, "concurrent Close result"); err != nil {
			t.Fatalf("concurrent Close %d: %v", index, err)
		}
	}
	waitChildRunSignal(t, controller.dispatcherDone, "dispatcher stop after concurrent close")
}

func enqueueAndActivateChildRun(
	t *testing.T,
	controller *ChildRunAdmission,
	constraints ChildRunConstraints,
	run func(context.Context, *ChildRunLease),
) *ChildRunTicket {
	t.Helper()
	ticket, err := controller.Enqueue(context.Background(), constraints, run)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := ticket.Activate(); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	return ticket
}

func closeChildRunController(t *testing.T, controller *ChildRunAdmission) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), childRunTestTimeout)
		defer cancel()
		if err := controller.Close(ctx); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
}

func waitChildRunSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(childRunTestTimeout):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func assertChildRunNotSignalled(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
		t.Fatal(description)
	case <-time.After(20 * time.Millisecond):
	}
}

func waitChildRunValue[T any](t *testing.T, values <-chan T, description string) T {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(childRunTestTimeout):
		t.Fatalf("timed out waiting for %s", description)
		var zero T
		return zero
	}
}

func waitForChildRunCondition(t *testing.T, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(childRunTestTimeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", description)
		}
		time.Sleep(time.Millisecond)
	}
}
