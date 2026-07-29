package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	orchestration "github.com/nextlevelbuilder/goclaw/internal/childrun"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// fakeDispatcher records Fire calls and returns a preset decision.
type fakeDispatcher struct {
	decision hooks.Decision
	calls    int
}

type recordingDelegateEventBus struct {
	mu     sync.Mutex
	events []eventbus.DomainEvent
}

func (b *recordingDelegateEventBus) Publish(event eventbus.DomainEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, event)
}

func (*recordingDelegateEventBus) Subscribe(
	eventbus.EventType,
	eventbus.DomainEventHandler,
) func() {
	return func() {}
}

func (*recordingDelegateEventBus) Start(context.Context) {}

func (*recordingDelegateEventBus) Drain(time.Duration) error { return nil }

func (b *recordingDelegateEventBus) snapshot() []eventbus.DomainEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]eventbus.DomainEvent(nil), b.events...)
}

func (f *fakeDispatcher) Fire(_ context.Context, _ hooks.Event) (hooks.FireResult, error) {
	f.calls++
	return hooks.FireResult{Decision: f.decision}, nil
}

// --- minimal noop stores to satisfy AgentLinkStore / AgentCRUDStore ---

type noopAgentLink struct{}

var noopAgentLinkID = uuid.MustParse("8d9c2f78-7914-4e4e-b75f-e6e22ea75921")

func (noopAgentLink) CreateLink(_ context.Context, _ *store.AgentLinkData) error { return nil }
func (noopAgentLink) DeleteLink(_ context.Context, _ uuid.UUID) error            { return nil }
func (noopAgentLink) UpdateLink(_ context.Context, _ uuid.UUID, _ map[string]any) error {
	return nil
}
func (noopAgentLink) GetLink(_ context.Context, _ uuid.UUID) (*store.AgentLinkData, error) {
	return nil, nil
}
func (noopAgentLink) ListLinksFrom(_ context.Context, _ uuid.UUID) ([]store.AgentLinkData, error) {
	return nil, nil
}
func (noopAgentLink) ListLinksTo(_ context.Context, _ uuid.UUID) ([]store.AgentLinkData, error) {
	return nil, nil
}
func (noopAgentLink) CanDelegate(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return true, nil
}
func (noopAgentLink) GetLinkBetween(_ context.Context, _, _ uuid.UUID) (*store.AgentLinkData, error) {
	return &store.AgentLinkData{
		BaseModel:     store.BaseModel{ID: noopAgentLinkID},
		MaxConcurrent: 3,
		Status:        store.LinkStatusActive,
	}, nil
}
func (noopAgentLink) DelegateTargets(_ context.Context, _ uuid.UUID) ([]store.AgentLinkData, error) {
	return nil, nil
}
func (noopAgentLink) SearchDelegateTargets(_ context.Context, _ uuid.UUID, _ string, _ int) ([]store.AgentLinkData, error) {
	return nil, nil
}
func (noopAgentLink) SearchDelegateTargetsByEmbedding(_ context.Context, _ uuid.UUID, _ []float32, _ int) ([]store.AgentLinkData, error) {
	return nil, nil
}
func (noopAgentLink) DeleteTeamLinksForAgent(_ context.Context, _, _ uuid.UUID) error { return nil }

type fixedAgentLink struct {
	noopAgentLink
	link store.AgentLinkData
}

func (f fixedAgentLink) GetLinkBetween(_ context.Context, _, _ uuid.UUID) (*store.AgentLinkData, error) {
	link := f.link
	return &link, nil
}

type noopAgentCRUD struct {
	keyToID map[string]uuid.UUID
}

func (n noopAgentCRUD) Create(_ context.Context, _ *store.AgentData) error { return nil }
func (n noopAgentCRUD) GetByKey(_ context.Context, key string) (*store.AgentData, error) {
	id, ok := n.keyToID[key]
	if !ok {
		id = uuid.New()
	}
	return &store.AgentData{BaseModel: store.BaseModel{ID: id}, AgentKey: key}, nil
}
func (n noopAgentCRUD) GetByID(_ context.Context, _ uuid.UUID) (*store.AgentData, error) {
	return nil, nil
}
func (n noopAgentCRUD) GetByIDUnscoped(_ context.Context, _ uuid.UUID) (*store.AgentData, error) {
	return nil, nil
}
func (n noopAgentCRUD) GetByKeys(_ context.Context, _ []string) ([]store.AgentData, error) {
	return nil, nil
}
func (n noopAgentCRUD) GetByIDs(_ context.Context, _ []uuid.UUID) ([]store.AgentData, error) {
	return nil, nil
}
func (n noopAgentCRUD) Update(_ context.Context, _ uuid.UUID, _ map[string]any) error { return nil }
func (n noopAgentCRUD) Delete(_ context.Context, _ uuid.UUID) error                   { return nil }
func (n noopAgentCRUD) List(_ context.Context, _ string) ([]store.AgentData, error)   { return nil, nil }
func (n noopAgentCRUD) GetDefault(_ context.Context) (*store.AgentData, error)        { return nil, nil }
func (n noopAgentCRUD) ResetStuckSummoning(_ context.Context) (int64, error)          { return 0, nil }

// --- helpers ---

func makeDelegateCtx(t *testing.T) context.Context {
	t.Helper()
	ctx := store.WithAgentID(context.Background(), uuid.New())
	ctx = store.WithTenantID(ctx, store.MasterTenantID)
	ctx = store.WithAgentKey(ctx, "parent-agent")
	ctx = WithToolWorkspace(ctx, t.TempDir())
	return ctx
}

func newDelegateTestTool(
	t *testing.T,
	links store.AgentLinkStore,
	runFn DelegateRunFunc,
) *DelegateTool {
	t.Helper()
	tool := NewDelegateTool(links, noopAgentCRUD{}, nil, runFn)
	tool.SetWorkspace(t.TempDir())
	t.Cleanup(tool.Close)
	return tool
}

// --- tests ---

func TestDelegateTool_EmitsSentOnlyAfterAdmissionWithLogicalTask(t *testing.T) {
	t.Run("rejected admission emits nothing", func(t *testing.T) {
		admission := orchestration.NewChildRunAdmission(1, 1)
		if err := admission.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
		events := &recordingDelegateEventBus{}
		tool := NewDelegateToolWithAdmission(
			noopAgentLink{},
			noopAgentCRUD{},
			events,
			func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
				t.Fatal("runFn called after admission closed")
				return DelegateResult{}, nil
			},
			admission,
		)
		tool.SetWorkspace(t.TempDir())
		defer tool.Close()

		result := tool.Execute(makeDelegateCtx(t), map[string]any{
			"agent_key": "child-agent",
			"task":      "do something",
			"mode":      "sync",
		})
		if result == nil || !result.IsError {
			t.Fatalf("result = %#v, want admission error", result)
		}
		if got := events.snapshot(); len(got) != 0 {
			t.Fatalf("events = %#v, want none", got)
		}
	})

	t.Run("accepted dispatch redacts caller host path", func(t *testing.T) {
		events := &recordingDelegateEventBus{}
		managedWorkspace := t.TempDir()
		callerWorkspace := t.TempDir()
		tool := NewDelegateTool(
			noopAgentLink{},
			noopAgentCRUD{},
			events,
			func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
				return DelegateResult{Content: "done"}, nil
			},
		)
		tool.SetWorkspace(managedWorkspace)
		defer tool.Close()

		ctx := WithToolWorkspace(makeDelegateCtx(t), callerWorkspace)
		result := tool.Execute(ctx, map[string]any{
			"agent_key": "child-agent",
			"task":      "inspect " + filepath.Join(callerWorkspace, "report.txt"),
			"mode":      "sync",
		})
		if result == nil || result.IsError {
			t.Fatalf("result = %#v, want success", result)
		}
		eventsSnapshot := events.snapshot()
		if len(eventsSnapshot) == 0 {
			t.Fatal("delegate.sent not emitted")
		}
		var sent *eventbus.DelegateSentPayload
		for _, event := range eventsSnapshot {
			if event.Type != eventbus.EventDelegateSent {
				continue
			}
			payload, ok := event.Payload.(eventbus.DelegateSentPayload)
			if !ok {
				t.Fatalf("delegate.sent payload = %T", event.Payload)
			}
			sent = &payload
			break
		}
		if sent == nil {
			t.Fatalf("events = %#v, want delegate.sent", eventsSnapshot)
		}
		if strings.Contains(sent.Task, callerWorkspace) ||
			!strings.Contains(sent.Task, "caller workspace") {
			t.Fatalf("delegate.sent task = %q", sent.Task)
		}
	})
}

func TestDelegateTool_SyncQueuedTimeoutDoesNotReportCompletion(t *testing.T) {
	admission := orchestration.NewChildRunAdmission(1, 4)
	blockerStarted := make(chan struct{})
	blockerRelease := make(chan struct{})
	blocker, err := admission.Enqueue(
		context.Background(),
		orchestration.ChildRunConstraints{TaskID: "blocker"},
		func(context.Context, *orchestration.ChildRunLease) {
			close(blockerStarted)
			<-blockerRelease
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := blocker.Activate(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blockerStarted:
	case <-time.After(time.Second):
		t.Fatal("blocker did not start")
	}

	events := &recordingDelegateEventBus{}
	runCalled := false
	tool := NewDelegateToolWithAdmission(
		noopAgentLink{},
		noopAgentCRUD{},
		events,
		func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
			runCalled = true
			return DelegateResult{Content: "unexpected"}, nil
		},
		admission,
	)
	tool.SetWorkspace(t.TempDir())
	defer tool.Close()
	ctx, cancel := context.WithTimeout(makeDelegateCtx(t), 20*time.Millisecond)
	defer cancel()
	result := tool.Execute(ctx, map[string]any{
		"agent_key": "child-agent",
		"task":      "must time out while queued",
		"mode":      "sync",
	})
	if result == nil || !result.IsError {
		t.Fatalf("result = %#v, want queued timeout", result)
	}
	if runCalled {
		t.Fatal("queued timed-out delegation ran")
	}
	for _, event := range events.snapshot() {
		if event.Type == eventbus.EventDelegateSent ||
			event.Type == eventbus.EventDelegateCompleted {
			t.Fatalf("timed-out queued delegation emitted %q", event.Type)
		}
	}

	close(blockerRelease)
	<-blocker.Done()
	if err := admission.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDelegateTool_SyncCallbackPanicReportsFailure(t *testing.T) {
	events := &recordingDelegateEventBus{}
	tool := NewDelegateTool(
		noopAgentLink{},
		noopAgentCRUD{},
		events,
		func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
			panic("injected delegate panic")
		},
	)
	tool.SetWorkspace(t.TempDir())
	defer tool.Close()
	result := tool.Execute(makeDelegateCtx(t), map[string]any{
		"agent_key": "child-agent",
		"task":      "panic safely",
		"mode":      "sync",
	})
	if result == nil || !result.IsError ||
		!strings.Contains(result.ForLLM, "child run panic") {
		t.Fatalf("result = %#v, want recovered callback panic", result)
	}
	var failed, completed bool
	for _, event := range events.snapshot() {
		failed = failed || event.Type == eventbus.EventDelegateFailed
		completed = completed || event.Type == eventbus.EventDelegateCompleted
	}
	if !failed || completed {
		t.Fatalf("events = %#v, want failed without completed", events.snapshot())
	}
}

func TestDelegateTool_SubagentStartBlock_AbortsDispatch(t *testing.T) {
	runCalled := 0
	runFn := func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
		runCalled++
		return DelegateResult{Content: "ok"}, nil
	}

	tool := newDelegateTestTool(t, noopAgentLink{}, runFn)
	disp := &fakeDispatcher{decision: hooks.DecisionBlock}
	tool.SetHookDispatcher(disp)

	result := tool.Execute(makeDelegateCtx(t), map[string]any{
		"agent_key": "child-agent",
		"task":      "do something",
		"mode":      "sync",
	})

	if result == nil || !result.IsError {
		t.Fatal("expected error result when hook blocks")
	}
	if runCalled != 0 {
		t.Errorf("runFn must not be called; got %d calls", runCalled)
	}
	if disp.calls != 1 {
		t.Errorf("expected 1 dispatcher Fire call; got %d", disp.calls)
	}
}

func TestDelegateTool_SubagentStartAllow_ProceedsToRun(t *testing.T) {
	runCalled := 0
	runFn := func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
		runCalled++
		return DelegateResult{Content: "done"}, nil
	}

	tool := newDelegateTestTool(t, noopAgentLink{}, runFn)
	disp := &fakeDispatcher{decision: hooks.DecisionAllow}
	tool.SetHookDispatcher(disp)

	result := tool.Execute(makeDelegateCtx(t), map[string]any{
		"agent_key": "child-agent",
		"task":      "do something",
		"mode":      "sync",
	})

	if result != nil && result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if runCalled != 1 {
		t.Errorf("expected runFn called once; got %d", runCalled)
	}
}

func TestDelegateTool_NilDispatcher_SkipsHook(t *testing.T) {
	runCalled := 0
	runFn := func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
		runCalled++
		return DelegateResult{Content: "done"}, nil
	}

	// No SetHookDispatcher — hookDispatcher stays nil.
	tool := newDelegateTestTool(t, noopAgentLink{}, runFn)

	result := tool.Execute(makeDelegateCtx(t), map[string]any{
		"agent_key": "child-agent",
		"task":      "do something",
		"mode":      "sync",
	})

	if result != nil && result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if runCalled != 1 {
		t.Errorf("expected runFn called once; got %d", runCalled)
	}
}

func TestDelegateTool_SyncNestedRunCompletesBeforeOuterPublication(t *testing.T) {
	admission := orchestration.NewChildRunAdmission(1, 8)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := admission.Close(ctx); err != nil {
			t.Errorf("close admission: %v", err)
		}
	})

	callerWorkspace := t.TempDir()
	managedWorkspace := t.TempDir()
	childB := uuid.New()
	childCStarted := make(chan struct{})
	releaseChildC := make(chan struct{})
	var childCCompleted atomic.Bool

	var tool *DelegateTool
	runFn := func(ctx context.Context, req DelegateRequest) (DelegateResult, error) {
		switch req.ToAgentKey {
		case "child-b":
			nestedCtx := store.WithAgentID(ctx, childB)
			nestedCtx = store.WithAgentKey(nestedCtx, "child-b")
			nestedCtx = WithToolAgentKey(nestedCtx, "child-b")
			nestedCtx = WithToolWorkspace(nestedCtx, req.DelegateOutputsPath)
			nestedCtx = WithDelegationID(nestedCtx, req.DelegationID)
			nestedCtx = WithDelegationArtifactInputs(nestedCtx, req.DelegateInputsPath)
			nestedCtx = WithSubagentConfig(nestedCtx, &config.SubagentsConfig{MaxSpawnDepth: 1})
			if depth := subagentDepthFromContext(nestedCtx, -1); depth != 0 {
				return DelegateResult{}, fmt.Errorf("delegated agent depth = %d, want 0", depth)
			}
			if parentTaskID := subagentTaskIDFromContext(nestedCtx); parentTaskID != "" {
				return DelegateResult{}, fmt.Errorf("delegated agent parent task = %q, want empty", parentTaskID)
			}
			if scope := subagentScopeFromContext(nestedCtx); scope.RootAgentID != childB ||
				scope.RootAgentKey != "child-b" {
				return DelegateResult{}, fmt.Errorf("delegated agent scope = %#v, want child-b", scope)
			}

			nested := tool.Execute(nestedCtx, map[string]any{
				"agent_key": "child-c",
				"task":      "finish before child B returns",
				"mode":      "sync",
			})
			if nested == nil || nested.IsError {
				return DelegateResult{}, fmt.Errorf("nested delegation failed: %#v", nested)
			}
			if !childCCompleted.Load() {
				return DelegateResult{}, errors.New("nested delegation returned before child C completed")
			}
			if err := os.WriteFile(
				filepath.Join(req.DelegateOutputsPath, "b-after-c.txt"),
				[]byte("child C completed"),
				0600,
			); err != nil {
				return DelegateResult{}, err
			}
			return DelegateResult{Content: "child B completed after child C"}, nil

		case "child-c":
			close(childCStarted)
			select {
			case <-releaseChildC:
			case <-ctx.Done():
				return DelegateResult{}, ctx.Err()
			}
			childCCompleted.Store(true)
			return DelegateResult{Content: "child C completed"}, nil

		default:
			return DelegateResult{}, fmt.Errorf("unexpected target %q", req.ToAgentKey)
		}
	}

	tool = NewDelegateToolWithAdmission(
		noopAgentLink{},
		noopAgentCRUD{keyToID: map[string]uuid.UUID{
			"child-b": childB,
			"child-c": uuid.New(),
		}},
		nil,
		runFn,
		admission,
	)
	tool.SetWorkspace(managedWorkspace)
	t.Cleanup(tool.Close)

	ctx := WithToolWorkspace(makeDelegateCtx(t), callerWorkspace)
	ctx = WithSubagentConfig(ctx, &config.SubagentsConfig{MaxSpawnDepth: 1})
	resultCh := make(chan *Result, 1)
	go func() {
		resultCh <- tool.Execute(ctx, map[string]any{
			"agent_key": "child-b",
			"task":      "delegate synchronously to child C",
			"mode":      "sync",
		})
	}()

	select {
	case <-childCStarted:
	case <-time.After(time.Second):
		t.Fatal("nested child C did not start")
	}
	select {
	case result := <-resultCh:
		t.Fatalf("outer delegation published before nested child completed: %#v", result)
	case <-time.After(50 * time.Millisecond):
	}
	if entries, err := os.ReadDir(filepath.Join(callerWorkspace, ".delegations")); err == nil && len(entries) > 0 {
		t.Fatalf("outer artifacts visible while nested child was active: %#v", entries)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inspect caller publication root: %v", err)
	}

	close(releaseChildC)
	var result *Result
	select {
	case result = <-resultCh:
	case <-time.After(2 * time.Second):
		t.Fatal("outer delegation did not finish after nested child completed")
	}
	if result == nil || result.IsError {
		t.Fatalf("outer delegation = %#v, want success", result)
	}
	foundMarker := false
	for _, media := range result.Media {
		if media.Filename == "b-after-c.txt" {
			foundMarker = true
			content, err := os.ReadFile(media.Path)
			if err != nil {
				t.Fatalf("read published marker: %v", err)
			}
			if string(content) != "child C completed" {
				t.Fatalf("published marker = %q", content)
			}
		}
	}
	if !foundMarker {
		t.Fatalf("published media = %#v, want b-after-c.txt", result.Media)
	}
}

func TestDelegateTool_ConcurrentMultiLinkTopologiesPublishInCallerScope(t *testing.T) {
	managedWorkspace := t.TempDir()
	callerA := t.TempDir()
	callerC := t.TempDir()
	agentA := uuid.New()
	agentB := uuid.New()
	agentC := uuid.New()

	started := make(chan struct{}, 3)
	release := make(chan struct{})
	runFn := func(ctx context.Context, req DelegateRequest) (DelegateResult, error) {
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return DelegateResult{}, ctx.Err()
		}
		if err := os.WriteFile(
			filepath.Join(req.DelegateOutputsPath, "result.txt"),
			[]byte(req.Task),
			0600,
		); err != nil {
			return DelegateResult{}, err
		}
		return DelegateResult{Content: req.Task}, nil
	}
	tool := NewDelegateTool(
		noopAgentLink{},
		noopAgentCRUD{keyToID: map[string]uuid.UUID{
			"agent-b": agentB,
			"agent-c": agentC,
		}},
		nil,
		runFn,
	)
	tool.SetWorkspace(managedWorkspace)
	t.Cleanup(tool.Close)

	type topologyCase struct {
		name            string
		fromID          uuid.UUID
		fromKey         string
		toKey           string
		callerWorkspace string
		payload         string
	}
	cases := []topologyCase{
		{
			name:            "a-to-b",
			fromID:          agentA,
			fromKey:         "agent-a",
			toKey:           "agent-b",
			callerWorkspace: callerA,
			payload:         "A to B",
		},
		{
			name:            "a-to-c",
			fromID:          agentA,
			fromKey:         "agent-a",
			toKey:           "agent-c",
			callerWorkspace: callerA,
			payload:         "A to C",
		},
		{
			name:            "c-to-b",
			fromID:          agentC,
			fromKey:         "agent-c",
			toKey:           "agent-b",
			callerWorkspace: callerC,
			payload:         "C to B",
		},
	}

	results := make([]*Result, len(cases))
	var wg sync.WaitGroup
	for i := range cases {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tc := cases[i]
			ctx := store.WithAgentID(context.Background(), tc.fromID)
			ctx = store.WithAgentKey(ctx, tc.fromKey)
			ctx = store.WithTenantID(ctx, store.MasterTenantID)
			ctx = WithToolWorkspace(ctx, tc.callerWorkspace)
			results[i] = tool.Execute(ctx, map[string]any{
				"agent_key": tc.toKey,
				"task":      tc.payload,
				"mode":      "sync",
			})
		}(i)
	}
	for range cases {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("concurrent delegation did not start")
		}
	}
	close(release)
	wg.Wait()

	aPublications := make(map[string]struct{})
	for i, tc := range cases {
		result := results[i]
		if result == nil || result.IsError {
			t.Fatalf("%s result = %#v", tc.name, result)
		}
		if len(result.Media) != 1 {
			t.Fatalf("%s media = %#v, want one output", tc.name, result.Media)
		}
		media := result.Media[0]
		relative, err := filepath.Rel(tc.callerWorkspace, media.Path)
		if err != nil || strings.HasPrefix(relative, "..") {
			t.Fatalf("%s output %q escaped caller %q", tc.name, media.Path, tc.callerWorkspace)
		}
		parts := strings.Split(filepath.ToSlash(relative), "/")
		if len(parts) != 4 || parts[0] != ".delegations" || parts[2] != "outputs" || parts[3] != "result.txt" {
			t.Fatalf("%s relative output = %q", tc.name, relative)
		}
		content, err := os.ReadFile(media.Path)
		if err != nil {
			t.Fatalf("%s read output: %v", tc.name, err)
		}
		if string(content) != tc.payload {
			t.Fatalf("%s output = %q, want %q", tc.name, content, tc.payload)
		}
		if tc.fromKey == "agent-a" {
			if _, exists := aPublications[parts[1]]; exists {
				t.Fatalf("A→B and A→C reused delegation %q", parts[1])
			}
			aPublications[parts[1]] = struct{}{}
		}
	}
	if len(aPublications) != 2 {
		t.Fatalf("A publications = %d, want 2 isolated delegation roots", len(aPublications))
	}
}

func TestDelegateTool_LinkMaxConcurrentIsReservedAndUnenforced(t *testing.T) {
	linkID := uuid.New()
	started := make(chan struct{})
	releaseRun := make(chan struct{})
	var calls atomic.Int32
	runFn := func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
		call := calls.Add(1)
		if call == 1 {
			close(started)
		}
		if call <= 2 {
			<-releaseRun
		}
		return DelegateResult{Content: "done"}, nil
	}
	tool := newDelegateTestTool(t, fixedAgentLink{link: store.AgentLinkData{
		BaseModel:     store.BaseModel{ID: linkID},
		MaxConcurrent: 1,
		Status:        store.LinkStatusActive,
	}}, runFn)

	first := tool.Execute(makeDelegateCtx(t), map[string]any{
		"agent_key": "child-agent",
		"task":      "first",
		"mode":      "async",
	})
	if first == nil || first.IsError {
		t.Fatalf("first delegation = %#v, want accepted", first)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first async delegation did not start")
	}

	second := tool.Execute(makeDelegateCtx(t), map[string]any{
		"agent_key": "child-agent",
		"task":      "second",
		"mode":      "async",
	})
	if second == nil || second.IsError {
		t.Fatalf("second delegation = %#v, want shared admission to accept it", second)
	}
	deadline := time.Now().Add(time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("runFn calls = %d, want max_concurrent ignored", got)
	}

	close(releaseRun)

	third := tool.Execute(makeDelegateCtx(t), map[string]any{
		"agent_key": "child-agent",
		"task":      "third",
		"mode":      "sync",
	})
	if third == nil || third.IsError {
		t.Fatalf("third delegation = %#v, want success after release", third)
	}
}

func TestDelegateTool_LinkSlotReleasedOnHookBlockAndSyncError(t *testing.T) {
	link := fixedAgentLink{link: store.AgentLinkData{
		BaseModel:     store.BaseModel{ID: uuid.New()},
		MaxConcurrent: 1,
		Status:        store.LinkStatusActive,
	}}
	var calls atomic.Int32
	runFn := func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
		if calls.Add(1) == 1 {
			return DelegateResult{}, errors.New("first run failed")
		}
		return DelegateResult{Content: "done"}, nil
	}
	tool := newDelegateTestTool(t, link, runFn)
	disp := &fakeDispatcher{decision: hooks.DecisionBlock}
	tool.SetHookDispatcher(disp)

	blocked := tool.Execute(makeDelegateCtx(t), map[string]any{
		"agent_key": "child-agent",
		"task":      "blocked",
		"mode":      "sync",
	})
	if blocked == nil || !blocked.IsError {
		t.Fatalf("blocked delegation = %#v, want error", blocked)
	}

	disp.decision = hooks.DecisionAllow
	failed := tool.Execute(makeDelegateCtx(t), map[string]any{
		"agent_key": "child-agent",
		"task":      "fails",
		"mode":      "sync",
	})
	if failed == nil || !failed.IsError ||
		strings.Contains(failed.ForLLM, "active delegation limit reached") {
		t.Fatalf("failed delegation = %#v, want run error after hook slot release", failed)
	}

	succeeded := tool.Execute(makeDelegateCtx(t), map[string]any{
		"agent_key": "child-agent",
		"task":      "succeeds",
		"mode":      "sync",
	})
	if succeeded == nil || succeeded.IsError {
		t.Fatalf("delegation after sync error = %#v, want success", succeeded)
	}
}

func TestDelegateTool_PreservesGroupAuthorizationScope(t *testing.T) {
	var captured DelegateRequest
	runFn := func(_ context.Context, req DelegateRequest) (DelegateResult, error) {
		captured = req
		return DelegateResult{Content: "done"}, nil
	}
	tool := newDelegateTestTool(t, noopAgentLink{}, runFn)

	ctx := makeDelegateCtx(t)
	ctx = store.WithUserID(ctx, "group:telegram:-100123")
	ctx = store.WithSenderID(ctx, "386246614")
	ctx = store.WithRole(ctx, "viewer")
	ctx = WithToolChannel(ctx, "telegram-main")
	ctx = WithToolChannelType(ctx, "telegram")
	ctx = WithToolChatID(ctx, "-100123")
	ctx = WithToolPeerKind(ctx, "group")

	result := tool.Execute(ctx, map[string]any{
		"agent_key": "child-agent",
		"task":      "create a file",
		"mode":      "sync",
	})

	if result == nil || result.IsError {
		t.Fatalf("delegate result = %#v, want success", result)
	}
	if captured.UserID != "group:telegram:-100123" {
		t.Fatalf("UserID = %q, want group authorization scope", captured.UserID)
	}
	if captured.SenderID != "386246614" || captured.Role != "viewer" {
		t.Fatalf("actor scope = (%q, %q), want real sender and role", captured.SenderID, captured.Role)
	}
	if captured.Channel != "telegram-main" || captured.ChannelType != "telegram" ||
		captured.ChatID != "-100123" || captured.PeerKind != "group" {
		t.Fatalf("origin scope = %#v", captured)
	}
}

func TestDelegateTool_CurrentRunMediaBecomesReadOnlyRelativeInput(t *testing.T) {
	workspace := t.TempDir()
	source := filepath.Join(workspace, "reference.jpg")
	if err := os.WriteFile(source, []byte("reference"), 0644); err != nil {
		t.Fatal(err)
	}

	var captured DelegateRequest
	runFn := func(_ context.Context, req DelegateRequest) (DelegateResult, error) {
		captured = req
		staged, err := os.ReadFile(filepath.Join(req.DelegateInputsPath, "reference.jpg"))
		if err != nil {
			return DelegateResult{}, err
		}
		if string(staged) != "reference" {
			return DelegateResult{}, errors.New("staged input content mismatch")
		}
		return DelegateResult{Content: "done"}, nil
	}
	tool := newDelegateTestTool(t, noopAgentLink{}, runFn)

	ctx := WithToolWorkspace(makeDelegateCtx(t), workspace)
	ctx = WithRunMediaPaths(ctx, []string{source})
	result := tool.Execute(ctx, map[string]any{
		"agent_key": "child-agent",
		"task":      "use " + source,
		"mode":      "sync",
	})

	if result == nil || result.IsError {
		t.Fatalf("delegate result = %#v, want success", result)
	}
	if strings.Contains(captured.Task, source) || !strings.Contains(captured.Task, "inputs/reference.jpg") {
		t.Fatalf("Task = %q, want only logical input alias", captured.Task)
	}
	if captured.DelegateInputsPath == "" || captured.DelegateOutputsPath == "" {
		t.Fatalf("runtime artifact roots missing: %#v", captured)
	}
}

func TestDelegateTool_DoesNotScrapeAbsolutePathsFromTask(t *testing.T) {
	workspace := t.TempDir()
	source := filepath.Join(workspace, "family-reference.jpg")
	if err := os.WriteFile(source, []byte("reference"), 0644); err != nil {
		t.Fatal(err)
	}

	var captured DelegateRequest
	var stagedInputCount int
	runFn := func(_ context.Context, req DelegateRequest) (DelegateResult, error) {
		captured = req
		entries, err := os.ReadDir(req.DelegateInputsPath)
		if err != nil {
			return DelegateResult{}, err
		}
		stagedInputCount = len(entries)
		return DelegateResult{Content: "done"}, nil
	}
	tool := newDelegateTestTool(t, noopAgentLink{}, runFn)

	ctx := WithToolWorkspace(makeDelegateCtx(t), workspace)
	result := tool.Execute(ctx, map[string]any{
		"agent_key": "child-agent",
		"task":      "Retry using the exact sample file path: " + source + ".",
		"mode":      "sync",
	})

	if result == nil || result.IsError {
		t.Fatalf("delegate result = %#v, want success", result)
	}
	if stagedInputCount != 0 {
		t.Fatalf("staged input count = %d, want no free-form path scraping", stagedInputCount)
	}
	if !strings.Contains(captured.Task, source) {
		t.Fatalf("Task = %q, want untrusted prose left as prose", captured.Task)
	}
}

func TestDelegateTool_RejectsInvalidExplicitInputBeforeDispatch(t *testing.T) {
	workspace := t.TempDir()

	var called atomic.Bool
	runFn := func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
		called.Store(true)
		return DelegateResult{Content: "done"}, nil
	}
	tool := newDelegateTestTool(t, noopAgentLink{}, runFn)

	ctx := WithToolWorkspace(makeDelegateCtx(t), workspace)
	result := tool.Execute(ctx, map[string]any{
		"agent_key": "child-agent",
		"task":      "Use the requested input",
		"mode":      "sync",
		"inputs":    []any{"../outside-reference.jpg"},
	})

	if result == nil || !result.IsError {
		t.Fatalf("delegate result = %#v, want validation error", result)
	}
	if called.Load() {
		t.Fatal("runFn called for invalid explicit input")
	}
}

func TestDelegateTool_MissingManagedWorkspaceFailsClosed(t *testing.T) {
	tool := NewDelegateTool(noopAgentLink{}, noopAgentCRUD{}, nil, func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
		return DelegateResult{Content: "unexpected"}, nil
	})
	result := tool.Execute(makeDelegateCtx(t), map[string]any{
		"agent_key": "child-agent",
		"task":      "do something",
		"mode":      "sync",
	})
	if result == nil || !result.IsError {
		t.Fatalf("delegate result = %#v, want fail-closed setup error", result)
	}
}

func TestDelegateTool_PublishesOnlyManifestOutputsAndCleansExchange(t *testing.T) {
	workspace := t.TempDir()
	managedWorkspace := t.TempDir()
	rawMedia := filepath.Join(t.TempDir(), "raw-child-media.png")
	if err := os.WriteFile(rawMedia, []byte("raw"), 0644); err != nil {
		t.Fatal(err)
	}

	var captured DelegateRequest
	runFn := func(_ context.Context, req DelegateRequest) (DelegateResult, error) {
		captured = req
		if err := os.WriteFile(filepath.Join(req.DelegateOutputsPath, "report.txt"), []byte("published"), 0600); err != nil {
			return DelegateResult{}, err
		}
		return DelegateResult{
			Content: "done",
			Media: []bus.MediaFile{{
				Path:     rawMedia,
				MimeType: "image/png",
				Filename: "raw-child-media.png",
			}},
		}, nil
	}
	tool := NewDelegateTool(noopAgentLink{}, noopAgentCRUD{}, nil, runFn)
	tool.SetWorkspace(managedWorkspace)

	ctx := WithToolWorkspace(makeDelegateCtx(t), workspace)
	result := tool.Execute(ctx, map[string]any{
		"agent_key": "child-agent",
		"task":      "create report.txt",
		"mode":      "sync",
	})
	if result == nil || result.IsError {
		t.Fatalf("delegate result = %#v, want success", result)
	}
	if len(result.Media) != 1 {
		t.Fatalf("result media = %#v, want one published manifest output", result.Media)
	}
	got := result.Media[0]
	if got.Path == rawMedia || got.Filename != "report.txt" || got.MimeType != "text/plain; charset=utf-8" {
		t.Fatalf("published media = %#v", got)
	}
	if content, err := os.ReadFile(got.Path); err != nil || string(content) != "published" {
		t.Fatalf("published content = %q, %v", content, err)
	}
	if _, err := os.Stat(filepath.Dir(captured.DelegateOutputsPath)); !os.IsNotExist(err) {
		t.Fatalf("successful exchange retained at runtime path: %v", err)
	}
}

func TestDelegateTool_AdmissionConstraintsAreTenantScopedAndRootless(t *testing.T) {
	tenantID := uuid.New()
	ctx := store.WithTenantID(context.Background(), tenantID)
	ctx = withSubagentExecution(ctx, TaskScope{
		TenantID: tenantID, RootAgentID: uuid.New(), RootAgentKey: "parent-agent",
	}, "parent-task", 3, nil)
	req := DelegateRequest{
		TenantID:     tenantID.String(),
		DelegationID: uuid.NewString(),
	}

	got := delegateAdmissionConstraints(ctx, req)
	if got.TenantID != tenantID || got.RootAgentID != uuid.Nil {
		t.Fatalf("identity constraints = %#v", got)
	}
	if got.TaskID != req.DelegationID || got.ParentTaskID != "parent-task" {
		t.Fatalf("task constraints = %#v", got)
	}
	if got.Depth != 4 || got.MaxDepth != 0 {
		t.Fatalf("depth constraints = %#v", got)
	}
}

func TestDelegateTool_AdmissionDoesNotUseAgentSpawnDepth(t *testing.T) {
	req := DelegateRequest{
		TenantID:     store.MasterTenantID.String(),
		DelegationID: uuid.NewString(),
	}
	got := delegateAdmissionConstraints(context.Background(), req)
	if got.Depth != 1 || got.MaxDepth != 0 {
		t.Fatalf("top-level admission depth = %#v, want structural depth 1 without agent cap", got)
	}

	ctx := WithSubagentConfig(context.Background(), &config.SubagentsConfig{MaxSpawnDepth: 4})
	got = delegateAdmissionConstraints(ctx, req)
	if got.MaxDepth != 0 {
		t.Fatalf("configured agent spawn depth leaked into admission = %#v", got)
	}
}

func TestDelegateTool_StagesArtifactsOnlyAfterAdmission(t *testing.T) {
	workspace := t.TempDir()
	managedWorkspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "input.txt"), []byte("input"), 0600); err != nil {
		t.Fatal(err)
	}

	admission := orchestration.NewChildRunAdmission(1, 4)
	defer func() {
		if err := admission.Close(context.Background()); err != nil {
			t.Errorf("close admission: %v", err)
		}
	}()
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondStarted := make(chan struct{})
	runFn := func(_ context.Context, req DelegateRequest) (DelegateResult, error) {
		if strings.HasPrefix(req.Task, "first") {
			close(firstStarted)
			<-releaseFirst
		} else {
			close(secondStarted)
		}
		return DelegateResult{Content: "done"}, nil
	}
	tool := NewDelegateToolWithAdmission(noopAgentLink{}, noopAgentCRUD{}, nil, runFn, admission)
	tool.SetWorkspace(managedWorkspace)
	ctx := WithToolWorkspace(makeDelegateCtx(t), workspace)

	first := tool.Execute(ctx, map[string]any{
		"agent_key": "child-agent",
		"task":      "first",
		"mode":      "async",
	})
	if first == nil || first.IsError {
		t.Fatalf("first delegation = %#v", first)
	}
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first delegation did not start")
	}

	second := tool.Execute(ctx, map[string]any{
		"agent_key": "child-agent",
		"task":      "second",
		"mode":      "async",
		"inputs":    []any{"input.txt"},
	})
	if second == nil || second.IsError {
		t.Fatalf("second delegation = %#v", second)
	}
	var accepted struct {
		DelegationID string `json:"delegation_id"`
	}
	if err := json.Unmarshal([]byte(second.ForLLM), &accepted); err != nil {
		t.Fatal(err)
	}
	secondExchange := filepath.Join(
		managedWorkspace,
		"collaboration",
		"delegations",
		accepted.DelegationID,
	)
	if _, err := os.Stat(secondExchange); !os.IsNotExist(err) {
		t.Fatalf("pending delegation staged before admission: %v", err)
	}

	close(releaseFirst)
	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("second delegation did not start after admission")
	}
}

func TestDelegateTool_AsyncReleasesAdmissionBeforeAnnouncement(t *testing.T) {
	admission := orchestration.NewChildRunAdmission(1, 4)
	messageBus := bus.New()
	for range 1000 {
		messageBus.PublishInbound(bus.InboundMessage{Content: "fill"})
	}

	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	var calls atomic.Int32
	runFn := func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
		switch calls.Add(1) {
		case 1:
			close(firstStarted)
		case 2:
			close(secondStarted)
		}
		return DelegateResult{Content: "done"}, nil
	}
	tool := NewDelegateToolWithAdmission(noopAgentLink{}, noopAgentCRUD{}, nil, runFn, admission)
	tool.SetWorkspace(t.TempDir())
	tool.SetMsgBus(messageBus)
	taskStore := newRecordingSubagentTaskStore()
	tool.SetTaskStore(taskStore)
	if tool.announceToParent(DelegateRequest{
		DelegationID: "backpressure-probe",
		ChatID:       "chat-1",
	}, "must not block", nil) {
		t.Fatal("full message bus accepted an announcement")
	}

	ctx := WithToolChatID(makeDelegateCtx(t), "chat-1")
	ctx = WithToolSessionKey(ctx, "delegate-parent-session")
	first := tool.Execute(ctx, map[string]any{
		"agent_key": "child-agent",
		"task":      "first",
		"mode":      "async",
	})
	if first == nil || first.IsError {
		t.Fatalf("first delegation = %#v, want accepted", first)
	}
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first delegation did not start")
	}

	second := tool.Execute(ctx, map[string]any{
		"agent_key": "child-agent",
		"task":      "second",
		"mode":      "async",
	})
	if second == nil || second.IsError {
		t.Fatalf("second delegation = %#v, want accepted", second)
	}
	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("blocked announce retained the only child-run permit")
	}
	for range 2 {
		select {
		case metadata := <-taskStore.metadata:
			if metadata[asyncCompletionDeliveryKey] != asyncCompletionDeliveryMissed {
				t.Fatalf("announcement metadata = %#v, want undelivered", metadata)
			}
		case <-time.After(time.Second):
			t.Fatal("delegation missed announcement was not recorded after bus saturation")
		}
	}

	if err := admission.Close(context.Background()); err != nil {
		t.Fatalf("close admission: %v", err)
	}
	tool.Close()
}

func TestDelegateToolCloseContextDrainsAsyncCompletion(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	tool := newDelegateTestTool(t, noopAgentLink{}, func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
		close(started)
		<-release
		return DelegateResult{Content: "done"}, nil
	})
	tool.SetTaskStore(newRecordingSubagentTaskStore())

	result := tool.Execute(makeDelegateCtx(t), map[string]any{
		"agent_key": "child-agent",
		"task":      "wait for shutdown drain",
		"mode":      "async",
	})
	if result == nil || result.IsError {
		t.Fatalf("delegate result = %#v, want accepted", result)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("delegation did not start")
	}

	timeoutCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := tool.CloseContext(timeoutCtx); err == nil {
		t.Fatal("CloseContext returned before async completion finished")
	}

	close(release)
	if err := tool.CloseContext(context.Background()); err != nil {
		t.Fatalf("CloseContext after completion: %v", err)
	}
}

func TestDelegationTaskWithInputAliasesDoesNotRewriteProseSubstrings(t *testing.T) {
	task := "Use the data file to update the database metadata."
	inputs := []delegateInput{{
		relativePath: "data",
	}}
	staged := []DelegationArtifact{{Path: "inputs/data"}}

	got := delegationTaskWithInputAliases(task, inputs, staged)
	if !strings.HasPrefix(got, task) {
		t.Fatalf("task prose was rewritten: %q", got)
	}
	if !strings.Contains(got, "data => inputs/data") {
		t.Fatalf("task is missing the explicit input mapping: %q", got)
	}
}

func TestDelegationTaskWithInputAliasesRewritesOnlyExactPathTokens(t *testing.T) {
	const reference = "/workspace/agent/.uploads/report.pdf"
	task := "Read " + reference + ", but preserve " + reference + ".backup and prefix" + reference
	inputs := []delegateInput{{
		relativePath:   ".uploads/report.pdf",
		taskReferences: []string{reference},
	}}
	staged := []DelegationArtifact{{Path: "inputs/report.pdf"}}

	got := delegationTaskWithInputAliases(task, inputs, staged)
	if !strings.Contains(got, "Read inputs/report.pdf,") {
		t.Fatalf("exact path token was not rewritten: %q", got)
	}
	if !strings.Contains(got, reference+".backup") {
		t.Fatalf("path substring suffix was rewritten: %q", got)
	}
	if !strings.Contains(got, "prefix"+reference) {
		t.Fatalf("path substring prefix was rewritten: %q", got)
	}
}

func TestRedactDelegationArtifactTextHandlesSlashVariants(t *testing.T) {
	exchange := &DelegationArtifactExchange{
		hostRoot: `C:\workspace\collaboration\delegations\123`,
	}
	text := `failed at C:/workspace/collaboration/delegations/123/inputs/file.txt`
	got := redactDelegationArtifactText(text, exchange)
	if strings.Contains(got, "C:/workspace") {
		t.Fatalf("slash-normalized host path was not redacted: %q", got)
	}
	if !strings.Contains(got, "inputs/file.txt") {
		t.Fatalf("logical input path missing after redaction: %q", got)
	}
}

func TestDelegateTool_RetainsFailedExchange(t *testing.T) {
	workspace := t.TempDir()
	managedWorkspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "input.txt"), []byte("input"), 0600); err != nil {
		t.Fatal(err)
	}

	var captured DelegateRequest
	tool := NewDelegateTool(noopAgentLink{}, noopAgentCRUD{}, nil, func(_ context.Context, req DelegateRequest) (DelegateResult, error) {
		captured = req
		return DelegateResult{}, errors.New("child run failed")
	})
	tool.SetWorkspace(managedWorkspace)
	t.Cleanup(tool.Close)
	ctx := WithToolWorkspace(makeDelegateCtx(t), workspace)
	result := tool.Execute(ctx, map[string]any{
		"agent_key": "child-agent",
		"task":      "fail after staging",
		"mode":      "sync",
		"inputs":    []any{"input.txt"},
	})
	if result == nil || !result.IsError {
		t.Fatalf("delegate result = %#v, want error", result)
	}
	exchangeRoot := filepath.Join(
		managedWorkspace,
		"collaboration",
		"delegations",
		captured.DelegationID,
	)
	if _, err := os.Stat(filepath.Join(exchangeRoot, "inputs", "input.txt")); err != nil {
		t.Fatalf("failed exchange input not retained: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".delegations", captured.DelegationID)); !os.IsNotExist(err) {
		t.Fatalf("failed exchange was published: %v", err)
	}
}

type delegateTraceCaptureStore struct {
	store.TracingStore
	mu    sync.Mutex
	spans []store.SpanData
}

func (s *delegateTraceCaptureStore) BatchCreateSpans(_ context.Context, spans []store.SpanData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spans = append(s.spans, spans...)
	return nil
}

func (s *delegateTraceCaptureStore) BatchUpdateTraceAggregates(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (s *delegateTraceCaptureStore) RecoverStaleRunningTraces(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func (s *delegateTraceCaptureStore) DeleteTracesOlderThan(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func TestDelegateTool_EmitsSanitizedArtifactLifecycleOnDelegateTrace(t *testing.T) {
	workspace := t.TempDir()
	traceStore := &delegateTraceCaptureStore{}
	collector := tracing.NewCollector(traceStore)
	collector.Start()
	traceID := uuid.New()

	runFn := func(_ context.Context, req DelegateRequest) (DelegateResult, error) {
		if err := os.WriteFile(filepath.Join(req.DelegateOutputsPath, "result.json"), []byte("{}"), 0600); err != nil {
			return DelegateResult{}, err
		}
		return DelegateResult{Content: "done", TraceID: traceID}, nil
	}
	tool := newDelegateTestTool(t, noopAgentLink{}, runFn)
	ctx := tracing.WithCollector(WithToolWorkspace(makeDelegateCtx(t), workspace), collector)
	result := tool.Execute(ctx, map[string]any{
		"agent_key": "child-agent",
		"task":      "create result",
		"mode":      "sync",
	})
	if result == nil || result.IsError {
		t.Fatalf("delegate result = %#v, want success", result)
	}
	collector.Stop()

	traceStore.mu.Lock()
	defer traceStore.mu.Unlock()
	if len(traceStore.spans) != 3 {
		t.Fatalf("spans = %#v, want staged, published, cleaned", traceStore.spans)
	}
	wantNames := []string{
		"delegate.artifacts.staged",
		"delegate.artifacts.published",
		"delegate.artifacts.cleaned",
	}
	for i, span := range traceStore.spans {
		if span.TraceID != traceID || span.SpanType != store.SpanTypeEvent ||
			span.Name != wantNames[i] {
			t.Fatalf("lifecycle span %d = %#v, want %s", i, span, wantNames[i])
		}
		metadata := string(span.Metadata)
		if strings.Contains(metadata, workspace) {
			t.Fatalf("lifecycle metadata leaked workspace: %s", metadata)
		}
	}
	for _, index := range []int{1, 2} {
		metadata := string(traceStore.spans[index].Metadata)
		if !strings.Contains(metadata, `"path":"outputs/result.json"`) {
			t.Fatalf("output lifecycle metadata = %s", metadata)
		}
	}
}

func TestDelegateTool_EmitsSanitizedFailedArtifactLifecycle(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "input.txt"), []byte("input"), 0600); err != nil {
		t.Fatal(err)
	}
	traceStore := &delegateTraceCaptureStore{}
	collector := tracing.NewCollector(traceStore)
	collector.Start()
	traceID := uuid.New()

	runFn := func(_ context.Context, req DelegateRequest) (DelegateResult, error) {
		if req.OnTraceCreated != nil {
			req.OnTraceCreated(traceID)
		}
		return DelegateResult{TraceID: traceID}, fmt.Errorf(
			"failed while using %s",
			req.DelegateOutputsPath,
		)
	}
	tool := newDelegateTestTool(t, noopAgentLink{}, runFn)
	ctx := tracing.WithCollector(WithToolWorkspace(makeDelegateCtx(t), workspace), collector)
	result := tool.Execute(ctx, map[string]any{
		"agent_key": "child-agent",
		"task":      "fail safely",
		"mode":      "sync",
		"inputs":    []any{"input.txt"},
	})
	if result == nil || !result.IsError {
		t.Fatalf("delegate result = %#v, want failure", result)
	}
	if strings.Contains(result.ForLLM, workspace) {
		t.Fatalf("delegate error leaked workspace: %s", result.ForLLM)
	}
	collector.Stop()

	traceStore.mu.Lock()
	defer traceStore.mu.Unlock()
	if len(traceStore.spans) != 2 {
		t.Fatalf("spans = %#v, want staged and failed", traceStore.spans)
	}
	wantNames := []string{
		"delegate.artifacts.staged",
		"delegate.artifacts.failed",
	}
	for i, span := range traceStore.spans {
		if span.TraceID != traceID || span.Name != wantNames[i] {
			t.Fatalf("failure lifecycle span %d = %#v", i, span)
		}
		metadata := string(span.Metadata)
		if strings.Contains(metadata, workspace) ||
			!strings.Contains(metadata, `"path":"inputs/input.txt"`) {
			t.Fatalf("failure lifecycle metadata = %s", metadata)
		}
	}
}

func TestDelegateTool_EmitsCancelledArtifactLifecycle(t *testing.T) {
	traceStore := &delegateTraceCaptureStore{}
	collector := tracing.NewCollector(traceStore)
	collector.Start()
	traceID := uuid.New()

	runFn := func(_ context.Context, req DelegateRequest) (DelegateResult, error) {
		if req.OnTraceCreated != nil {
			req.OnTraceCreated(traceID)
		}
		return DelegateResult{TraceID: traceID}, context.Canceled
	}
	tool := newDelegateTestTool(t, noopAgentLink{}, runFn)
	ctx := tracing.WithCollector(makeDelegateCtx(t), collector)
	result := tool.Execute(ctx, map[string]any{
		"agent_key": "child-agent",
		"task":      "cancel safely",
		"mode":      "sync",
	})
	if result == nil || !result.IsError {
		t.Fatalf("delegate result = %#v, want cancellation", result)
	}
	collector.Stop()

	traceStore.mu.Lock()
	defer traceStore.mu.Unlock()
	if len(traceStore.spans) != 2 ||
		traceStore.spans[0].Name != "delegate.artifacts.staged" ||
		traceStore.spans[1].Name != "delegate.artifacts.cancelled" {
		t.Fatalf("cancellation lifecycle spans = %#v", traceStore.spans)
	}
	for _, span := range traceStore.spans {
		if span.TraceID != traceID {
			t.Fatalf("cancellation trace ID = %s, want %s", span.TraceID, traceID)
		}
	}
}

func TestDelegateTool_SweeperDeletesOnlyRegisteredExpiredExchange(t *testing.T) {
	tenantWorkspace := t.TempDir()
	delegationID := uuid.New()
	exchangeRoot := filepath.Join(tenantWorkspace, "collaboration", "delegations", delegationID.String())
	if err := os.MkdirAll(exchangeRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(exchangeRoot, "retained.txt"), []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(exchangeRoot, "hostile-link")); err != nil {
		t.Logf("symlink cleanup coverage unavailable: %v", err)
	}
	tool := NewDelegateTool(noopAgentLink{}, noopAgentCRUD{}, nil, func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
		return DelegateResult{}, nil
	})
	item := retainedDelegationArtifact{
		tenantWorkspace: tenantWorkspace,
		tenantID:        store.MasterTenantID,
		delegationID:    delegationID,
		retainUntil:     time.Now().Add(-time.Minute),
	}
	tool.retained[retainedDelegationArtifactKey(tenantWorkspace, delegationID)] = item
	tool.sweepRetainedDelegationExchanges(time.Now())

	if _, err := os.Stat(exchangeRoot); !os.IsNotExist(err) {
		t.Fatalf("expired exchange still exists: %v", err)
	}
	if len(tool.retained) != 0 {
		t.Fatalf("retained registry = %#v, want empty", tool.retained)
	}
	if got, err := os.ReadFile(sentinel); err != nil || string(got) != "keep" {
		t.Fatalf("secure cleanup followed hostile link: %q, %v", got, err)
	}
}

func TestDelegateTool_RecoversRetainedExchangeAfterRestart(t *testing.T) {
	tenantWorkspace := t.TempDir()
	callerWorkspace := filepath.Join(tenantWorkspace, "agents", "caller")
	if err := os.MkdirAll(callerWorkspace, 0750); err != nil {
		t.Fatal(err)
	}
	callerRoot, err := OpenDelegationArtifactRoot(callerWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	delegationID := uuid.New()
	exchange, err := NewDelegationArtifactExchange(
		tenantWorkspace,
		store.MasterTenantID,
		delegationID,
		DelegationArtifactLimits{},
		time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}

	first := NewDelegateTool(noopAgentLink{}, noopAgentCRUD{}, nil, func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
		return DelegateResult{}, nil
	})
	first.SetWorkspace(tenantWorkspace)
	job := &delegateArtifactJob{
		req:             DelegateRequest{DelegationID: delegationID.String()},
		callerRoot:      callerRoot,
		callerWorkspace: callerWorkspace,
		tenantWorkspace: tenantWorkspace,
		tenantID:        store.MasterTenantID,
		delegationID:    delegationID,
	}
	job.callerLocation = first.resolveDelegationCallerLocation(job)
	exchange.RetainFailure(time.Now().Add(-2*time.Minute), "test_failure")
	if err := first.registerRetainedDelegationExchange(exchange, job, artifactLifecycleFailed); err != nil {
		t.Fatalf("register retained exchange: %v", err)
	}
	stateBytes, err := os.ReadFile(filepath.Join(
		tenantWorkspace,
		"collaboration",
		"delegations",
		delegationID.String(),
		delegationArtifactLifecycleFile,
	))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stateBytes), tenantWorkspace) ||
		strings.Contains(string(stateBytes), callerWorkspace) {
		t.Fatalf("lifecycle state persisted a host path: %s", stateBytes)
	}
	if err := exchange.Close(); err != nil {
		t.Fatal(err)
	}
	if err := callerRoot.Close(); err != nil {
		t.Fatal(err)
	}
	first.Close()

	restarted := NewDelegateTool(noopAgentLink{}, noopAgentCRUD{}, nil, func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
		return DelegateResult{}, nil
	})
	restarted.SetWorkspace(tenantWorkspace)
	if len(restarted.retained) != 1 {
		t.Fatalf("recovered retained registry = %#v, want one", restarted.retained)
	}
	restarted.sweepRetainedDelegationExchanges(time.Now())
	restarted.Close()

	exchangeRoot := filepath.Join(
		tenantWorkspace,
		"collaboration",
		"delegations",
		delegationID.String(),
	)
	if _, err := os.Stat(exchangeRoot); !os.IsNotExist(err) {
		t.Fatalf("restart janitor left expired exchange: %v", err)
	}
}

func TestDelegateTool_RecoversStaleArtifactLifecycleStates(t *testing.T) {
	testCases := []struct {
		name   string
		status delegationArtifactLifecycleStatus
	}{
		{name: "staging", status: artifactLifecycleStaging},
		{name: "running", status: artifactLifecycleRunning},
		{name: "publishing", status: artifactLifecyclePublishing},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tenantWorkspace := t.TempDir()
			callerWorkspace := filepath.Join(tenantWorkspace, "agents", "caller")
			if err := os.MkdirAll(callerWorkspace, 0750); err != nil {
				t.Fatal(err)
			}
			callerRoot, err := OpenDelegationArtifactRoot(callerWorkspace)
			if err != nil {
				t.Fatal(err)
			}
			delegationID := uuid.New()
			exchange, err := NewDelegationArtifactExchange(
				tenantWorkspace,
				store.MasterTenantID,
				delegationID,
				DelegationArtifactLimits{},
				time.Minute,
			)
			if err != nil {
				t.Fatal(err)
			}
			first := NewDelegateTool(
				noopAgentLink{},
				noopAgentCRUD{},
				nil,
				func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
					return DelegateResult{}, nil
				},
			)
			first.workspace = tenantWorkspace
			job := &delegateArtifactJob{
				req:             DelegateRequest{DelegationID: delegationID.String()},
				callerRoot:      callerRoot,
				callerWorkspace: callerWorkspace,
				tenantWorkspace: tenantWorkspace,
				tenantID:        store.MasterTenantID,
				delegationID:    delegationID,
			}
			job.callerLocation = first.resolveDelegationCallerLocation(job)
			if err := first.updateActiveDelegationLifecycle(exchange, job); err != nil {
				t.Fatalf("update staging lifecycle: %v", err)
			}

			publicationTempPath := ""
			if testCase.status == artifactLifecycleRunning ||
				testCase.status == artifactLifecyclePublishing {
				if err := first.markDelegationRunning(exchange, job, time.Now()); err != nil {
					t.Fatalf("mark running: %v", err)
				}
			}
			if testCase.status == artifactLifecyclePublishing {
				publicationTempPath = filepath.ToSlash(filepath.Join(
					".delegations",
					".tmp-"+delegationID.String()+"-"+uuid.NewString(),
				))
				if err := first.markDelegationPublishing(
					exchange,
					job,
					publicationTempPath,
					time.Now(),
				); err != nil {
					t.Fatalf("mark publishing: %v", err)
				}
				if err := os.MkdirAll(
					filepath.Join(callerWorkspace, filepath.FromSlash(publicationTempPath)),
					0700,
				); err != nil {
					t.Fatal(err)
				}
			}

			finalSentinel := filepath.Join(
				callerWorkspace,
				".delegations",
				delegationID.String(),
				"keep.txt",
			)
			if err := os.MkdirAll(filepath.Dir(finalSentinel), 0750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(finalSentinel, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := exchange.Close(); err != nil {
				t.Fatal(err)
			}
			if err := callerRoot.Close(); err != nil {
				t.Fatal(err)
			}
			first.Close()

			restarted := NewDelegateTool(
				noopAgentLink{},
				noopAgentCRUD{},
				nil,
				func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
					return DelegateResult{}, nil
				},
			)
			recoveredAfter := time.Now().UTC()
			restarted.SetWorkspace(tenantWorkspace)
			defer restarted.Close()
			if len(restarted.retained) != 1 {
				t.Fatalf("recovered registry = %#v, want one", restarted.retained)
			}

			lifecyclePath := filepath.Join(
				tenantWorkspace,
				"collaboration",
				"delegations",
				delegationID.String(),
				delegationArtifactLifecycleFile,
			)
			stateBytes, err := os.ReadFile(lifecyclePath)
			if err != nil {
				t.Fatal(err)
			}
			var state delegationArtifactLifecycleState
			if err := json.Unmarshal(stateBytes, &state); err != nil {
				t.Fatal(err)
			}
			if state.Status != artifactLifecycleFailed ||
				state.FailedAt == nil ||
				state.ReasonCode != "artifact_recovered_stale" {
				t.Fatalf("recovered state = %#v", state)
			}
			earliestExpiry := recoveredAfter.Add(delegationArtifactFailureTTL)
			if state.RetainUntil.Before(earliestExpiry) ||
				state.RetainUntil.After(time.Now().UTC().Add(delegationArtifactFailureTTL)) {
				t.Fatalf("retain until = %v, want recovery time + %v", state.RetainUntil, delegationArtifactFailureTTL)
			}

			restarted.sweepRetainedDelegationExchanges(state.RetainUntil.Add(-time.Nanosecond))
			if _, err := os.Stat(lifecyclePath); err != nil {
				t.Fatalf("retained exchange removed before TTL: %v", err)
			}
			restarted.sweepRetainedDelegationExchanges(state.RetainUntil)
			if _, err := os.Stat(filepath.Dir(lifecyclePath)); !os.IsNotExist(err) {
				t.Fatalf("expired exchange remains: %v", err)
			}
			if publicationTempPath != "" {
				if _, err := os.Stat(filepath.Join(
					callerWorkspace,
					filepath.FromSlash(publicationTempPath),
				)); !os.IsNotExist(err) {
					t.Fatalf("recorded publication temp remains: %v", err)
				}
			}
			if got, err := os.ReadFile(finalSentinel); err != nil || string(got) != "keep" {
				t.Fatalf("final UUID publication directory was modified: %q, %v", got, err)
			}
		})
	}
}

func TestDelegateTool_RecoveryPromotesDurablePublishingState(t *testing.T) {
	tenantWorkspace := t.TempDir()
	callerWorkspace := filepath.Join(tenantWorkspace, "agents", "caller")
	if err := os.MkdirAll(callerWorkspace, 0o750); err != nil {
		t.Fatal(err)
	}
	callerRoot, err := OpenDelegationArtifactRoot(callerWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	delegationID := uuid.New()
	exchange, err := NewDelegationArtifactExchange(
		tenantWorkspace,
		store.MasterTenantID,
		delegationID,
		DelegationArtifactLimits{},
		time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	first := NewDelegateTool(
		noopAgentLink{},
		noopAgentCRUD{},
		nil,
		func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
			return DelegateResult{}, nil
		},
	)
	first.workspace = tenantWorkspace
	job := &delegateArtifactJob{
		req:             DelegateRequest{DelegationID: delegationID.String()},
		callerRoot:      callerRoot,
		callerWorkspace: callerWorkspace,
		tenantWorkspace: tenantWorkspace,
		tenantID:        store.MasterTenantID,
		delegationID:    delegationID,
	}
	job.callerLocation = first.resolveDelegationCallerLocation(job)
	if err := first.updateActiveDelegationLifecycle(exchange, job); err != nil {
		t.Fatalf("update lifecycle: %v", err)
	}
	if err := first.markDelegationRunning(exchange, job, time.Now()); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(exchange.OutputsHostPath(), "result.txt"),
		[]byte("durable result"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	publication, err := exchange.publishWithPreparation(
		context.Background(),
		callerRoot,
		time.Now(),
		func(tempPath string) error {
			return first.markDelegationPublishing(exchange, job, tempPath, time.Now())
		},
	)
	if err != nil {
		t.Fatalf("publish before simulated crash: %v", err)
	}
	if err := exchange.Close(); err != nil {
		t.Fatal(err)
	}
	if err := callerRoot.Close(); err != nil {
		t.Fatal(err)
	}
	first.Close()

	restarted := NewDelegateTool(
		noopAgentLink{},
		noopAgentCRUD{},
		nil,
		func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
			return DelegateResult{}, nil
		},
	)
	restarted.SetWorkspace(tenantWorkspace)
	defer restarted.Close()

	item, ok := restarted.retained[retainedDelegationArtifactKey(tenantWorkspace, delegationID)]
	if !ok || !item.publicationDurable || !item.retainUntil.IsZero() {
		t.Fatalf("recovered publication = %#v, want durable immediate cleanup", item)
	}
	lifecyclePath := filepath.Join(
		tenantWorkspace,
		"collaboration",
		"delegations",
		delegationID.String(),
		delegationArtifactLifecycleFile,
	)
	stateBytes, err := os.ReadFile(lifecyclePath)
	if err != nil {
		t.Fatal(err)
	}
	var state delegationArtifactLifecycleState
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	if state.Status != artifactLifecyclePublished || state.ReasonCode != "artifact_published" {
		t.Fatalf("recovered lifecycle = %#v, want published", state)
	}

	restarted.sweepRetainedDelegationExchanges(time.Now())
	if _, err := os.Stat(filepath.Dir(lifecyclePath)); !os.IsNotExist(err) {
		t.Fatalf("published exchange remains after cleanup: %v", err)
	}
	publishedOutput := filepath.Join(
		callerWorkspace,
		filepath.FromSlash(publication.RootPath),
		"outputs",
		"result.txt",
	)
	if got, err := os.ReadFile(publishedOutput); err != nil || string(got) != "durable result" {
		t.Fatalf("durable publication changed during recovery: %q, %v", got, err)
	}
}

func TestDelegateTool_RecoveryCleansInvalidAndExpiresCorruptExchanges(t *testing.T) {
	tenantWorkspace := t.TempDir()
	delegationsRoot := filepath.Join(tenantWorkspace, "collaboration", "delegations")
	invalidRoot := filepath.Join(delegationsRoot, "not-a-delegation")
	if err := os.MkdirAll(invalidRoot, 0750); err != nil {
		t.Fatal(err)
	}
	corruptID := uuid.New()
	corruptRoot := filepath.Join(delegationsRoot, corruptID.String())
	if err := os.MkdirAll(corruptRoot, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(corruptRoot, delegationArtifactLifecycleFile),
		[]byte("{not-json"),
		0600,
	); err != nil {
		t.Fatal(err)
	}

	tool := NewDelegateTool(
		noopAgentLink{},
		noopAgentCRUD{},
		nil,
		func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
			return DelegateResult{}, nil
		},
	)
	tool.SetWorkspace(tenantWorkspace)
	defer tool.Close()

	if _, err := os.Stat(invalidRoot); !os.IsNotExist(err) {
		t.Fatalf("invalid exchange entry remains after recovery: %v", err)
	}
	item, ok := tool.retained[retainedDelegationArtifactKey(tenantWorkspace, corruptID)]
	if !ok {
		t.Fatalf("corrupt exchange was not registered for bounded retention: %#v", tool.retained)
	}
	tool.sweepRetainedDelegationExchanges(item.retainUntil.Add(-time.Nanosecond))
	if _, err := os.Stat(corruptRoot); err != nil {
		t.Fatalf("corrupt exchange removed before retention elapsed: %v", err)
	}
	tool.sweepRetainedDelegationExchanges(item.retainUntil)
	if _, err := os.Stat(corruptRoot); !os.IsNotExist(err) {
		t.Fatalf("corrupt exchange remains after retention elapsed: %v", err)
	}
}

func TestDelegateTool_RecoversPublicationCleanupAfterRestart(t *testing.T) {
	tenantWorkspace := t.TempDir()
	callerWorkspace := filepath.Join(tenantWorkspace, "agents", "caller")
	if err := os.MkdirAll(callerWorkspace, 0750); err != nil {
		t.Fatal(err)
	}
	callerRoot, err := OpenDelegationArtifactRoot(callerWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	delegationID := uuid.New()
	exchange, err := NewDelegationArtifactExchange(
		tenantWorkspace,
		store.MasterTenantID,
		delegationID,
		DelegationArtifactLimits{},
		time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}

	first := NewDelegateTool(
		noopAgentLink{},
		noopAgentCRUD{},
		nil,
		func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
			return DelegateResult{}, nil
		},
	)
	first.workspace = tenantWorkspace
	job := &delegateArtifactJob{
		req:             DelegateRequest{DelegationID: delegationID.String()},
		callerRoot:      callerRoot,
		callerWorkspace: callerWorkspace,
		tenantWorkspace: tenantWorkspace,
		tenantID:        store.MasterTenantID,
		delegationID:    delegationID,
	}
	job.callerLocation = first.resolveDelegationCallerLocation(job)
	if err := first.updateActiveDelegationLifecycle(exchange, job); err != nil {
		t.Fatalf("update staging lifecycle: %v", err)
	}
	if err := first.markDelegationRunning(exchange, job, time.Now()); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(exchange.OutputsHostPath(), "result.txt"),
		[]byte("durable"),
		0600,
	); err != nil {
		t.Fatal(err)
	}
	var publicationTempPath string
	publication, err := exchange.publishWithPreparation(
		context.Background(),
		callerRoot,
		time.Now(),
		func(tempPath string) error {
			publicationTempPath = tempPath
			return first.markDelegationPublishing(exchange, job, tempPath, time.Now())
		},
	)
	if err != nil {
		t.Fatalf("publish before simulated restart: %v", err)
	}
	if err := first.markDelegationPublished(exchange, job, publication.Manifest.PublishedAt); err != nil {
		t.Fatalf("mark published: %v", err)
	}
	if publicationTempPath == "" {
		t.Fatal("publication temp path was not recorded")
	}
	if err := exchange.Close(); err != nil {
		t.Fatal(err)
	}
	if err := callerRoot.Close(); err != nil {
		t.Fatal(err)
	}
	first.Close()

	restarted := NewDelegateTool(
		noopAgentLink{},
		noopAgentCRUD{},
		nil,
		func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
			return DelegateResult{}, nil
		},
	)
	restarted.SetWorkspace(tenantWorkspace)
	if len(restarted.retained) != 1 {
		t.Fatalf("recovered cleanup registry = %#v, want one", restarted.retained)
	}
	restarted.sweepRetainedDelegationExchanges(time.Now())
	restarted.Close()

	exchangeRoot := filepath.Join(
		tenantWorkspace,
		"collaboration",
		"delegations",
		delegationID.String(),
	)
	if _, err := os.Stat(exchangeRoot); !os.IsNotExist(err) {
		t.Fatalf("restart janitor left publication exchange: %v", err)
	}
	publicationRoot := filepath.Join(
		callerWorkspace,
		".delegations",
		delegationID.String(),
	)
	published, err := os.ReadFile(filepath.Join(publicationRoot, "outputs", "result.txt"))
	if err != nil || string(published) != "durable" {
		t.Fatalf("restart janitor damaged durable publication: %q, %v", published, err)
	}
}

func TestDelegateTool_RetriesPublishedExchangeCleanup(t *testing.T) {
	managedWorkspace := t.TempDir()
	callerWorkspace := t.TempDir()
	traceStore := &delegateTraceCaptureStore{}
	collector := tracing.NewCollector(traceStore)
	collector.Start()
	traceID := uuid.New()
	var captured DelegateRequest
	tool := NewDelegateTool(
		noopAgentLink{},
		noopAgentCRUD{},
		nil,
		func(_ context.Context, req DelegateRequest) (DelegateResult, error) {
			captured = req
			if err := os.WriteFile(
				filepath.Join(req.DelegateOutputsPath, "result.txt"),
				[]byte("durable"),
				0600,
			); err != nil {
				return DelegateResult{}, err
			}
			return DelegateResult{Content: "done", TraceID: traceID}, nil
		},
	)
	tool.SetWorkspace(managedWorkspace)
	defer tool.Close()

	removeCalls := 0
	tool.removeExchange = func(tenantWorkspace string, delegationID uuid.UUID) error {
		removeCalls++
		if removeCalls == 1 {
			return errors.New("injected cleanup failure")
		}
		return tool.removeDelegationExchange(tenantWorkspace, delegationID)
	}
	ctx := tracing.WithCollector(
		WithToolWorkspace(makeDelegateCtx(t), callerWorkspace),
		collector,
	)
	result := tool.Execute(ctx, map[string]any{
		"agent_key": "child-agent",
		"task":      "publish output",
		"mode":      "sync",
	})
	if result == nil || result.IsError {
		t.Fatalf("delegate result = %#v, want success", result)
	}
	delegationID := uuid.MustParse(captured.DelegationID)
	exchangeRoot := filepath.Join(
		managedWorkspace,
		"collaboration",
		"delegations",
		delegationID.String(),
	)
	if _, err := os.Stat(exchangeRoot); err != nil {
		t.Fatalf("failed cleanup did not retain exchange for retry: %v", err)
	}
	tool.retainedMu.Lock()
	retainedCount := len(tool.retained)
	sweeperStarted := tool.sweeperStarted
	tool.retainedMu.Unlock()
	if retainedCount != 1 || !sweeperStarted {
		t.Fatalf("automatic cleanup registry = %d, sweeper started = %v", retainedCount, sweeperStarted)
	}

	tool.sweepRetainedDelegationExchanges(time.Now())
	if removeCalls != 2 {
		t.Fatalf("cleanup calls = %d, want immediate attempt plus retry", removeCalls)
	}
	if _, err := os.Stat(exchangeRoot); !os.IsNotExist(err) {
		t.Fatalf("cleanup retry left exchange: %v", err)
	}
	published, err := os.ReadFile(filepath.Join(
		callerWorkspace,
		".delegations",
		delegationID.String(),
		"outputs",
		"result.txt",
	))
	if err != nil || string(published) != "durable" {
		t.Fatalf("cleanup retry damaged durable output: %q, %v", published, err)
	}
	collector.Stop()
	traceStore.mu.Lock()
	defer traceStore.mu.Unlock()
	if len(traceStore.spans) != 3 ||
		traceStore.spans[2].TraceID != traceID ||
		traceStore.spans[2].Name != "delegate.artifacts.cleaned" {
		t.Fatalf("cleanup retry spans = %#v, want cleaned on original trace", traceStore.spans)
	}
}
