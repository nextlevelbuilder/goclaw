package tools

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
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
	waitForLifecycleStatus(t, taskStore, TaskStatusCompleted)

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

	manager.Close()
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
	waitForLifecycleStatus(t, taskStore, TaskStatusCompleted)
	select {
	case metadata := <-taskStore.metadata:
		if metadata[asyncCompletionDeliveryKey] != asyncCompletionDeliveryMissed {
			t.Fatalf("noninteractive delegate announcement metadata = %#v, want undelivered", metadata)
		}
	case <-time.After(time.Second):
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

func waitForLifecycleStatus(
	t *testing.T,
	taskStore *recordingSubagentTaskStore,
	want string,
) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case update := <-taskStore.updates:
			if update.status == want {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for lifecycle status %q", want)
		}
	}
}
