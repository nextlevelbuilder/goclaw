package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// fakeSubagentTaskStore is a minimal in-memory implementation. We only
// need Create + UpdateMetadata for spawn_job; the rest are no-op
// stubs to satisfy the SubagentTaskStore interface.
type fakeSubagentTaskStore struct {
	mu          sync.Mutex
	created     []store.SubagentTaskData
	createErr   error
	metaUpdates []map[string]any
}

func (f *fakeSubagentTaskStore) Create(_ context.Context, task *store.SubagentTaskData) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	f.created = append(f.created, *task)
	return nil
}
func (f *fakeSubagentTaskStore) UpdateMetadata(_ context.Context, _ uuid.UUID, meta map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.metaUpdates = append(f.metaUpdates, meta)
	return nil
}
func (f *fakeSubagentTaskStore) Get(context.Context, uuid.UUID) (*store.SubagentTaskData, error) {
	return nil, nil
}
func (f *fakeSubagentTaskStore) UpdateStatus(context.Context, uuid.UUID, string, *string, int, int64, int64) error {
	return nil
}
func (f *fakeSubagentTaskStore) ListByParent(context.Context, string, string) ([]store.SubagentTaskData, error) {
	return nil, nil
}
func (f *fakeSubagentTaskStore) ListBySession(context.Context, string) ([]store.SubagentTaskData, error) {
	return nil, nil
}
func (f *fakeSubagentTaskStore) ListRunningAcrossTenants(context.Context, int) ([]store.SubagentTaskData, error) {
	return nil, nil
}
func (f *fakeSubagentTaskStore) Archive(context.Context, time.Duration) (int64, error) {
	return 0, nil
}

// validArgs builds a complete arg map for spawn_job. Tests
// override individual fields to exercise validation paths.
func validArgs() map[string]any {
	return map[string]any{
		"kind":          "impl",
		"command":       "forge",
		"args":          []any{"-p", "do the thing"},
		"cwd":           "/data/workspace-eng/worktrees/test-task",
		"worktree_path": "/data/workspace-eng/worktrees/test-task",
		"sinks": []any{
			map[string]any{"type": "discord", "channel": "discord-eng", "thread_id": "1217211714185986128"},
		},
	}
}

// withTenantAndChannel returns a context shaped like a real Discord
// inbound: tenant set, channel + chat in tool ctx. Without these the
// spawn refuses (callbacks would have nowhere to post).
func withTenantAndChannel(t *testing.T) context.Context {
	t.Helper()
	tid := uuid.New()
	ctx := store.WithTenantID(context.Background(), tid)
	ctx = WithToolChannel(ctx, "discord-eng")
	ctx = WithToolChatID(ctx, "1217211714185986128")
	ctx = WithToolSessionKey(ctx, "agent:eng:discord-eng:group:1217211714185986128")
	return ctx
}

func TestSpawnJob_HMACSecretRequired(t *testing.T) {
	// Tool registered with empty secret should error cleanly rather
	// than silently sign with nil key. Operators see the message in
	// the agent trace; user sees it in Discord.
	tool := NewSpawnJobTool(&fakeSubagentTaskStore{}, "", nil)
	res := tool.Execute(withTenantAndChannel(t), validArgs())
	if res == nil || !res.IsError {
		t.Fatalf("expected error result on missing secret, got %+v", res)
	}
	if !strings.Contains(res.ForLLM, "HMAC secret not configured") {
		t.Errorf("error should mention HMAC secret, got %q", res.ForLLM)
	}
}

func TestSpawnJob_MissingSink(t *testing.T) {
	tool := NewSpawnJobTool(&fakeSubagentTaskStore{}, "", []byte("secret"))
	args := validArgs()
	delete(args, "sinks")
	res := tool.Execute(withTenantAndChannel(t), args)
	if res == nil || !res.IsError {
		t.Fatal("expected error for missing sink")
	}
	if !strings.Contains(res.ForLLM, "sink") {
		t.Errorf("error should mention sink, got %q", res.ForLLM)
	}
}

func TestSpawnJob_RequiredFields(t *testing.T) {
	cases := []string{"kind", "command", "worktree_path"}
	for _, missing := range cases {
		t.Run("missing_"+missing, func(t *testing.T) {
			tool := NewSpawnJobTool(&fakeSubagentTaskStore{}, "", []byte("secret"))
			args := validArgs()
			delete(args, missing)
			res := tool.Execute(withTenantAndChannel(t), args)
			if res == nil || !res.IsError {
				t.Fatalf("expected error when %s missing, got %+v", missing, res)
			}
		})
	}
}

func TestSpawnJob_NoTenantInContext(t *testing.T) {
	// Without a tenant we can't tenant-scope the row write, so refuse.
	tool := NewSpawnJobTool(&fakeSubagentTaskStore{}, "", []byte("secret"))
	ctx := WithToolChannel(context.Background(), "discord-eng")
	ctx = WithToolChatID(ctx, "1217")
	res := tool.Execute(ctx, validArgs())
	if res == nil || !res.IsError {
		t.Fatal("expected error when tenant unset")
	}
	if !strings.Contains(res.ForLLM, "no tenant") {
		t.Errorf("error should mention tenant, got %q", res.ForLLM)
	}
}

func TestSpawnJob_NoChannelInContext(t *testing.T) {
	// Without a channel in ctx the completion callback would have
	// nowhere to post, so refuse before any state writes.
	tool := NewSpawnJobTool(&fakeSubagentTaskStore{}, "", []byte("secret"))
	ctx := store.WithTenantID(context.Background(), uuid.New())
	res := tool.Execute(ctx, validArgs())
	if res == nil || !res.IsError {
		t.Fatal("expected error when channel unset")
	}
}

func TestSpawnJob_HappyPath(t *testing.T) {
	// End-to-end: the tool should HMAC-sign the body, POST to the
	// fake agent service, write a subagent_tasks row, then patch
	// the row's metadata with the returned k8s job name.
	taskStore := &fakeSubagentTaskStore{}
	receivedSig := ""
	receivedBody := []byte(nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSig = r.Header.Get("X-Hub-Signature-256")
		receivedBody, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(SpawnJobResponse{
			JobID:      uuid.New().String(),
			K8sJobName: "job-impl-deadbeef",
			K8sJobUID:  "uid-1",
		})
	}))
	defer srv.Close()

	tool := NewSpawnJobTool(taskStore, srv.URL, []byte("topsecret"))
	res := tool.Execute(withTenantAndChannel(t), validArgs())
	if res == nil || res.IsError {
		t.Fatalf("expected success, got %+v", res)
	}

	// Row was written before the POST.
	if len(taskStore.created) != 1 {
		t.Fatalf("expected 1 row created, got %d", len(taskStore.created))
	}
	row := taskStore.created[0]
	if row.Status != "running" {
		t.Errorf("row status: got %q want running", row.Status)
	}
	if row.Metadata["kind"] != "impl" {
		t.Errorf("metadata kind: got %v want impl", row.Metadata["kind"])
	}
	if row.Metadata["worktree_path"] != "/data/workspace-eng/worktrees/test-task" {
		t.Errorf("metadata worktree_path lost")
	}
	if got := row.OriginChannel; got == nil || *got != "discord-eng" {
		t.Errorf("OriginChannel: got %v", got)
	}
	if got := row.OriginChatID; got == nil || *got != "1217211714185986128" {
		t.Errorf("OriginChatID: got %v", got)
	}

	// HMAC was sent and matches what the agent service will compute.
	if !strings.HasPrefix(receivedSig, "sha256=") {
		t.Errorf("missing or malformed signature header: %q", receivedSig)
	}
	wantSig := "sha256=" + computeHMACSignature(receivedBody, []byte("topsecret"))
	if receivedSig != wantSig {
		t.Errorf("signature mismatch:\n got  %q\n want %q", receivedSig, wantSig)
	}

	// Metadata was patched with the k8s job name post-success.
	if len(taskStore.metaUpdates) != 1 {
		t.Errorf("expected 1 metadata update, got %d", len(taskStore.metaUpdates))
	} else if taskStore.metaUpdates[0]["k8s_job_name"] != "job-impl-deadbeef" {
		t.Errorf("k8s_job_name not patched: %v", taskStore.metaUpdates[0])
	}
}

func TestSpawnJobJSONContract(t *testing.T) {
	req := SpawnJobRequest{
		JobID:         "11111111-2222-3333-4444-555555555555",
		Kind:          "impl",
		Command:       "forge",
		Args:          []string{"-p", "implement the thing"},
		Cwd:           "/data/workspace-eng/worktrees/task-1",
		WorkspaceRoot: "/data/workspace-eng",
		WorktreePath:  "/data/workspace-eng/worktrees/task-1",
		Timeout:       "45m",
		Resources: JobResources{
			CPURequest:    "500m",
			CPULimit:      "2",
			MemoryRequest: "1Gi",
			MemoryLimit:   "4Gi",
		},
		Env: map[string]string{
			"AGENT_MODE": "eng",
			"FOO":        "bar",
		},
		Sinks: []JobSink{
			{Type: "discord", Channel: "discord-eng", ThreadID: "1217"},
			{Type: "github_check", Owner: "cartridge-gg", Repo: "internal", CheckRunID: 99},
			{Type: "github_pr_review", Owner: "cartridge-gg", Repo: "internal", PRNumber: 77},
		},
		TenantID:      "tenant-1",
		ParentSession: "agent:eng:discord-eng:group:1217",
	}
	got, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"job_id":"11111111-2222-3333-4444-555555555555","kind":"impl","command":"forge","args":["-p","implement the thing"],"cwd":"/data/workspace-eng/worktrees/task-1","workspace_root":"/data/workspace-eng","worktree_path":"/data/workspace-eng/worktrees/task-1","timeout":"45m","resources":{"cpu_request":"500m","cpu_limit":"2","memory_request":"1Gi","memory_limit":"4Gi"},"env":{"AGENT_MODE":"eng","FOO":"bar"},"sinks":[{"type":"discord","channel":"discord-eng","thread_id":"1217"},{"type":"github_check","owner":"cartridge-gg","repo":"internal","check_run_id":99},{"type":"github_pr_review","owner":"cartridge-gg","repo":"internal","pr_number":77}],"tenant_id":"tenant-1","parent_session_key":"agent:eng:discord-eng:group:1217"}`
	if string(got) != want {
		t.Fatalf("spawn_job JSON contract drifted:\n got: %s\nwant: %s", got, want)
	}
}

func TestSpawnJob_AgentServiceErrorReturned(t *testing.T) {
	// 5xx from the agent service surfaces to the LLM as an error
	// result with status code + body excerpt. The row stays as
	// 'running' — recovery loop will catch it on next pod start.
	taskStore := &fakeSubagentTaskStore{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("k8s api unavailable"))
	}))
	defer srv.Close()

	tool := NewSpawnJobTool(taskStore, srv.URL, []byte("topsecret"))
	res := tool.Execute(withTenantAndChannel(t), validArgs())
	if res == nil || !res.IsError {
		t.Fatalf("expected error result on 500, got %+v", res)
	}
	if !strings.Contains(res.ForLLM, "500") {
		t.Errorf("error should include status code, got %q", res.ForLLM)
	}
	// Row was created before the failed POST.
	if len(taskStore.created) != 1 {
		t.Errorf("row should be written before POST, got %d", len(taskStore.created))
	}
}

func TestSpawnJob_RowWriteFailureSkipsPost(t *testing.T) {
	// If the row write fails, refuse to POST. Otherwise we'd create
	// a Job we can't track, with nothing in the recovery loop to
	// clean it up.
	taskStore := &fakeSubagentTaskStore{createErr: errors.New("db down")}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("agent service should NOT have been called")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tool := NewSpawnJobTool(taskStore, srv.URL, []byte("topsecret"))
	res := tool.Execute(withTenantAndChannel(t), validArgs())
	if res == nil || !res.IsError {
		t.Fatal("expected error on row write failure")
	}
}

// Sanity: the HMAC helper produces a stable hex-encoded sha256.
func TestComputeHMACSignature(t *testing.T) {
	got := computeHMACSignature([]byte("hello"), []byte("k"))
	// hex-encoded HMAC-SHA256 of "hello" with key "k". Verify with:
	//   python3 -c 'import hmac,hashlib; print(hmac.new(b"k", b"hello", hashlib.sha256).hexdigest())'
	want := "406e4b43f87095aa86ca6299d25e875921fefa180f02043bb29bec5681c0c2d0"
	if got != want {
		t.Errorf("HMAC mismatch:\n got  %s\n want %s", got, want)
	}
}

// roundTrip is a light reusable type for response decoding in
// future sub-tests. Keeping it here so adding cases stays cheap.
type roundTrip struct {
	body bytes.Buffer
}

var _ = roundTrip{} // suppress unused — kept for future tests
