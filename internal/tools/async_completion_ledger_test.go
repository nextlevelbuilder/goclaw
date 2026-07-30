package tools

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	orchestration "github.com/nextlevelbuilder/goclaw/internal/childrun"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestSpawnAsyncReturnsDurableCompletionAndGetSurvivesManagerStateLoss(t *testing.T) {
	manager := NewSubagentManager(
		&recordingSubagentProvider{response: "durable spawn result"},
		nil,
		"model",
		nil,
		NewRegistry,
		SubagentConfig{MaxConcurrent: 2, MaxSpawnDepth: 2, MaxChildrenPerAgent: 2},
	)
	taskStore := newRecordingSubagentTaskStore()
	manager.SetTaskStore(taskStore)
	tool := NewSpawnTool(manager, "parent", 0)
	ctx := subagentTestContext("parent")

	accepted := tool.Execute(ctx, map[string]any{"task": "persist this", "mode": "async"})
	if accepted == nil || accepted.IsError {
		t.Fatalf("spawn result = %#v", accepted)
	}
	var receipt struct {
		CompletionID string `json:"completion_id"`
		TaskID       string `json:"task_id"`
	}
	if err := json.NewDecoder(strings.NewReader(accepted.ForLLM)).Decode(&receipt); err != nil {
		t.Fatalf("decode accepted result: %v\n%s", err, accepted.ForLLM)
	}
	completionID, err := uuid.Parse(receipt.CompletionID)
	if err != nil || receipt.TaskID == "" {
		t.Fatalf("receipt = %#v, parse error = %v", receipt, err)
	}
	manager.Close()

	// A restarted manager has no in-memory tasks but can still retrieve the row.
	restarted := NewSubagentManager(nil, nil, "", nil, nil, SubagentConfig{})
	restarted.SetTaskStore(taskStore)
	getTool := NewSpawnTool(restarted, "parent", 0)
	got := getTool.Execute(ctx, map[string]any{
		"action":        "get",
		"completion_id": completionID.String(),
	})
	if got == nil || got.IsError || !strings.Contains(got.ForLLM, "durable spawn result") {
		t.Fatalf("durable get result = %#v", got)
	}

	otherRoot := store.WithAgentID(ctx, uuid.New())
	if result := getTool.Execute(otherRoot, map[string]any{
		"action":        "get",
		"completion_id": completionID.String(),
	}); result == nil || !result.IsError {
		t.Fatalf("other root retrieved completion: %#v", result)
	}

	restarted.Close()
}

func TestSpawnAsyncRejectsAcceptanceWhenDurableCreateFails(t *testing.T) {
	provider := &recordingSubagentProvider{}
	manager := NewSubagentManager(
		provider,
		nil,
		"model",
		nil,
		NewRegistry,
		SubagentConfig{MaxConcurrent: 1, MaxSpawnDepth: 1, MaxChildrenPerAgent: 1},
	)
	taskStore := newRecordingSubagentTaskStore()
	taskStore.createErr = errors.New("database unavailable")
	manager.SetTaskStore(taskStore)

	_, err := manager.Spawn(
		subagentTestContext("parent"),
		"parent",
		0,
		"must not run",
		"durable",
		"",
		"test",
		"chat",
		"",
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "persist accepted subagent") {
		t.Fatalf("spawn error = %v", err)
	}
	if provider.calls != 0 {
		t.Fatalf("provider calls = %d, want 0", provider.calls)
	}
	manager.Close()
}

func TestSpawnAsyncDoesNotLabelAnnouncementDeliveredBeforeTerminalPersistence(t *testing.T) {
	messageBus := bus.New()
	manager := NewSubagentManager(
		&recordingSubagentProvider{response: "terminal result"},
		nil,
		"model",
		messageBus,
		NewRegistry,
		SubagentConfig{MaxConcurrent: 1, MaxSpawnDepth: 1, MaxChildrenPerAgent: 1},
	)
	taskStore := newRecordingSubagentTaskStore()
	taskStore.updateErr = errors.New("terminal database write failed")
	manager.SetTaskStore(taskStore)

	_, err := manager.Spawn(
		subagentTestContext("parent"),
		"parent",
		0,
		"complete but fail persistence",
		"durability-ordering",
		"",
		"test",
		"chat",
		"",
		nil,
	)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	manager.Close()

	select {
	case metadata := <-taskStore.metadata:
		t.Fatalf("announcement metadata recorded before terminal persistence: %#v", metadata)
	default:
	}
}

func TestDelegateAsyncDurableGetIsSourceAgentScoped(t *testing.T) {
	taskStore := newRecordingSubagentTaskStore()
	tool := newDelegateTestTool(t, noopAgentLink{}, func(_ context.Context, _ DelegateRequest) (DelegateResult, error) {
		return DelegateResult{Content: "durable delegation result"}, nil
	})
	tool.SetTaskStore(taskStore)
	ctx := makeDelegateCtx(t)

	accepted := tool.Execute(ctx, map[string]any{
		"agent_key": "child-agent",
		"task":      "persist delegation",
		"mode":      "async",
	})
	if accepted == nil || accepted.IsError {
		t.Fatalf("delegate result = %#v", accepted)
	}
	var receipt struct {
		DelegationID string `json:"delegation_id"`
	}
	if err := json.Unmarshal([]byte(accepted.ForLLM), &receipt); err != nil {
		t.Fatalf("decode delegation receipt: %v", err)
	}
	if _, err := uuid.Parse(receipt.DelegationID); err != nil {
		t.Fatalf("delegation id = %q: %v", receipt.DelegationID, err)
	}
	tool.Close()
	select {
	case metadata := <-taskStore.metadata:
		if metadata[asyncCompletionDeliveryKey] != asyncCompletionDeliveryMissed {
			t.Fatalf("noninteractive delegate announcement metadata = %#v, want undelivered", metadata)
		}
	default:
		t.Fatal("noninteractive delegate did not record get-only fallback")
	}
	delegationID := uuid.MustParse(receipt.DelegationID)
	if err := taskStore.UpdateMetadata(
		ctx,
		store.AgentIDFromContext(ctx),
		delegationID,
		map[string]any{
			asyncCompletionMediaKey: []persistedCompletionMedia{{
				Path:     ".delegations/" + receipt.DelegationID + "/report.pdf",
				MimeType: "application/pdf",
				Filename: "report.pdf",
			}},
		},
	); err != nil {
		t.Fatalf("persist logical completion media: %v", err)
	}

	restarted := NewDelegateTool(nil, nil, nil, nil)
	restarted.SetTaskStore(taskStore)
	defer restarted.Close()
	got := restarted.Execute(ctx, map[string]any{
		"action":        "get",
		"delegation_id": receipt.DelegationID,
	})
	if got == nil || got.IsError ||
		!strings.Contains(got.ForLLM, "durable delegation result") ||
		!strings.Contains(got.ForLLM, ".delegations/"+receipt.DelegationID+"/report.pdf") {
		t.Fatalf("durable delegate get = %#v", got)
	}

	otherAgent := store.WithAgentID(ctx, uuid.New())
	if result := restarted.Execute(otherAgent, map[string]any{
		"action":        "get",
		"delegation_id": receipt.DelegationID,
	}); result == nil || !result.IsError {
		t.Fatalf("other agent retrieved delegation: %#v", result)
	}
}

func TestDelegateAsyncRunningPersistenceDoesNotRetainAdmissionPermit(t *testing.T) {
	taskStore := newRecordingSubagentTaskStore()
	var runningAttempts atomic.Int32
	runningDeadline := make(chan time.Duration, 1)
	persistenceStarted := make(chan struct{}, 1)
	persistenceRelease := make(chan struct{})
	defer func() {
		select {
		case <-persistenceRelease:
		default:
			close(persistenceRelease)
		}
	}()
	taskStore.updateHook = func(ctx context.Context, status string) error {
		if status != TaskStatusRunning {
			return nil
		}
		if runningAttempts.Add(1) != 1 {
			return nil
		}
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("running persistence has no deadline")
		}
		runningDeadline <- time.Until(deadline)
		persistenceStarted <- struct{}{}
		<-persistenceRelease
		return errors.New("transient running persistence failure")
	}

	admission := orchestration.NewChildRunAdmission(1, 4)
	runStarted := make(chan string, 2)
	tool := NewDelegateToolWithAdmission(noopAgentLink{}, noopAgentCRUD{}, nil, func(_ context.Context, req DelegateRequest) (DelegateResult, error) {
		runStarted <- req.Task
		return DelegateResult{Content: "done"}, nil
	}, admission)
	tool.SetWorkspace(t.TempDir())
	t.Cleanup(tool.Close)
	tool.SetTaskStore(taskStore)

	first := tool.Execute(makeDelegateCtx(t), map[string]any{
		"agent_key": "child-agent",
		"task":      "first",
		"mode":      "async",
	})
	if first == nil || first.IsError {
		t.Fatalf("first delegate result = %#v", first)
	}
	select {
	case task := <-runStarted:
		if task != "first" {
			t.Fatalf("first started task = %q", task)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first delegate run did not start")
	}
	select {
	case <-persistenceStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("running persistence did not start")
	}

	second := tool.Execute(makeDelegateCtx(t), map[string]any{
		"agent_key": "child-agent",
		"task":      "second",
		"mode":      "async",
	})
	if second == nil || second.IsError {
		t.Fatalf("second delegate result = %#v", second)
	}
	select {
	case task := <-runStarted:
		if task != "second" {
			t.Fatalf("second started task = %q", task)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("running-status persistence retained the only admission permit")
	}
	close(persistenceRelease)
	tool.Close()

	select {
	case remaining := <-runningDeadline:
		if remaining <= 0 || remaining > 500*time.Millisecond {
			t.Fatalf("running persistence deadline = %s, want at most 500ms", remaining)
		}
	default:
		t.Fatal("running persistence was not attempted")
	}
	if attempts := runningAttempts.Load(); attempts != 2 {
		t.Fatalf("running persistence attempts = %d, want one per accepted delegation", attempts)
	}
}

func TestCompletionMediaDescriptorsNeverPersistHostPaths(t *testing.T) {
	workspace := t.TempDir()
	inside := filepath.Join(workspace, ".uploads", "report.pdf")
	outside := filepath.Join(t.TempDir(), "secret.txt")
	descriptors := completionMediaDescriptors([]bus.MediaFile{
		{Path: inside, MimeType: "application/pdf", Filename: "report.pdf"},
		{Path: outside, MimeType: "text/plain", Filename: "secret.txt"},
	}, workspace, "")

	if len(descriptors) != 1 || descriptors[0].Path != ".uploads/report.pdf" {
		t.Fatalf("completion media = %#v, want one logical workspace path", descriptors)
	}
	encoded, err := json.Marshal(descriptors)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), workspace) || strings.Contains(string(encoded), outside) {
		t.Fatalf("completion media leaked host path: %s", encoded)
	}
	if payload := persistedCompletionMediaPayload([]map[string]any{{
		"path": outside,
	}}); len(payload) != 0 {
		t.Fatalf("absolute persisted media was returned: %#v", payload)
	}
}
