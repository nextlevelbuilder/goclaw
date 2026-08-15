package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func delegationArtifactToolContext(t *testing.T) (context.Context, string, string) {
	t.Helper()
	root := t.TempDir()
	inputs := filepath.Join(root, "inputs")
	outputs := filepath.Join(root, "outputs")
	if err := os.MkdirAll(inputs, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outputs, 0750); err != nil {
		t.Fatal(err)
	}
	ctx := WithDelegationID(context.Background(), uuid.NewString())
	ctx = WithDelegationArtifactInputs(ctx, inputs)
	ctx = WithToolWorkspace(ctx, outputs)
	return ctx, inputs, outputs
}

func TestDelegationArtifactFilePolicy(t *testing.T) {
	ctx, inputs, outputs := delegationArtifactToolContext(t)
	if err := os.WriteFile(filepath.Join(inputs, "source.txt"), []byte("staged"), 0440); err != nil {
		t.Fatal(err)
	}

	read := NewReadFileTool(outputs, true).Execute(ctx, map[string]any{"path": "inputs/source.txt"})
	if read.IsError || !strings.Contains(read.ForLLM, "staged") {
		t.Fatalf("read staged input = %#v", read)
	}
	listed := NewListFilesTool(outputs, true).Execute(ctx, map[string]any{"path": "inputs"})
	if listed.IsError || !strings.Contains(listed.ForLLM, "source.txt") {
		t.Fatalf("list staged inputs = %#v", listed)
	}

	writeInput := NewWriteFileTool(outputs, true).Execute(ctx, map[string]any{
		"path": "inputs/source.txt", "content": "mutated",
	})
	if !writeInput.IsError || !strings.Contains(writeInput.ForLLM, "read-only") {
		t.Fatalf("input mutation = %#v, want read-only error", writeInput)
	}
	editInput := NewEditTool(outputs, true).Execute(ctx, map[string]any{
		"path": "inputs/source.txt", "old_string": "staged", "new_string": "mutated",
	})
	if !editInput.IsError || !strings.Contains(editInput.ForLLM, "read-only") {
		t.Fatalf("input edit = %#v, want read-only error", editInput)
	}

	written := NewWriteFileTool(outputs, true).Execute(ctx, map[string]any{
		"path": "report.txt", "content": "result", "deliver": true,
	})
	if written.IsError || len(written.Media) != 0 {
		t.Fatalf("prepublication write delivery = %#v", written)
	}
	if got, err := os.ReadFile(filepath.Join(outputs, "report.txt")); err != nil || string(got) != "result" {
		t.Fatalf("output file = %q, %v", got, err)
	}

	if result := NewSendFileTool(outputs, true).Execute(ctx, map[string]any{"path": "report.txt"}); !result.IsError {
		t.Fatalf("send_file published prepublication output: %#v", result)
	}
	if result := (&MessageTool{}).Execute(ctx, map[string]any{
		"action": "send", "message": "MEDIA:report.txt",
	}); !result.IsError {
		t.Fatalf("message published prepublication output: %#v", result)
	}
}

func TestReadDocumentArchiveUsesLogicalDelegationInputPath(t *testing.T) {
	ctx, inputs, outputs := delegationArtifactToolContext(t)
	archive := filepath.Join(inputs, "archive.zip")
	if err := os.WriteFile(archive, []byte("not a real zip"), 0440); err != nil {
		t.Fatal(err)
	}

	result := NewReadDocumentTool(nil, nil).Execute(ctx, map[string]any{
		"path":   "inputs/archive.zip",
		"prompt": "inspect archive",
	})
	if result.IsError {
		t.Fatalf("read_document archive = %#v", result)
	}
	if strings.Contains(result.ForLLM, inputs) || strings.Contains(result.ForLLM, outputs) {
		t.Fatalf("archive result leaked host path: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "inputs/archive.zip") {
		t.Fatalf("archive result omitted logical path: %s", result.ForLLM)
	}
}

func TestDelegationArtifactDisablesVirtualMemoryRouting(t *testing.T) {
	ctx, _, outputs := delegationArtifactToolContext(t)
	agentID := uuid.New()
	ctx = store.WithAgentID(ctx, agentID)
	ctx = store.WithUserID(ctx, "user-1")
	memStore := newMockMemoryStore()
	memStore.docs[docKey(agentID.String(), "user-1", "MEMORY.md")] = "database-secret"
	interceptor := NewMemoryInterceptor(memStore, outputs)

	if err := os.WriteFile(filepath.Join(outputs, "MEMORY.md"), []byte("physical-output"), 0640); err != nil {
		t.Fatal(err)
	}
	reader := NewReadFileTool(outputs, true)
	reader.SetMemoryInterceptor(interceptor)
	result := reader.Execute(ctx, map[string]any{"path": "MEMORY.md"})
	if result.IsError || strings.Contains(result.ForLLM, "database-secret") ||
		!strings.Contains(result.ForLLM, "physical-output") {
		t.Fatalf("delegation memory read routed virtually: %#v", result)
	}

	writer := NewWriteFileTool(outputs, true)
	writer.SetMemoryInterceptor(interceptor)
	result = writer.Execute(ctx, map[string]any{"path": "MEMORY.md", "content": "new-output"})
	if result.IsError {
		t.Fatalf("physical MEMORY.md write failed: %#v", result)
	}
	if got := memStore.docs[docKey(agentID.String(), "user-1", "MEMORY.md")]; got != "database-secret" {
		t.Fatalf("virtual memory mutated: %q", got)
	}
}

func TestDelegationStructuredMediaPathsResolveOnlyStagedInputs(t *testing.T) {
	ctx, inputs, _ := delegationArtifactToolContext(t)
	staged := filepath.Join(inputs, "clip.mp4")
	if err := os.WriteFile(staged, []byte("video"), 0440); err != nil {
		t.Fatal(err)
	}

	got, err := resolveStructuredMediaPath(ctx, "inputs/clip.mp4", "video")
	if err != nil {
		t.Fatalf("resolve staged media: %v", err)
	}
	want, err := filepath.EvalSymlinks(staged)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("resolved path = %q, want %q", got, want)
	}

	_, err = resolveStructuredMediaPath(ctx, "inputs/missing.mp4", "video")
	if err == nil {
		t.Fatal("missing staged media unexpectedly resolved")
	}
	if strings.Contains(err.Error(), filepath.Dir(inputs)) {
		t.Fatalf("error leaked exchange host root: %v", err)
	}
}

func TestCreateImageReferenceUsesLogicalDelegationInputPath(t *testing.T) {
	ctx, inputs, _ := delegationArtifactToolContext(t)
	staged := filepath.Join(inputs, "reference.png")
	want := []byte("staged-reference")
	if err := os.WriteFile(staged, want, 0440); err != nil {
		t.Fatal(err)
	}

	refs, err := NewCreateImageTool(nil).resolveReferenceImages(ctx, map[string]any{
		"ref_images": []any{map[string]any{"path": "inputs/reference.png"}},
	})
	if err != nil {
		t.Fatalf("resolve staged create_image reference: %v", err)
	}
	if len(refs) != 1 || string(refs[0].Data) != string(want) {
		t.Fatalf("resolved refs = %#v, want staged reference", refs)
	}
}

func TestDelegationArtifactResultPolicySuppressesUnpublishedMedia(t *testing.T) {
	ctx, _, outputs := delegationArtifactToolContext(t)
	result := &Result{
		ForLLM: "created\nMEDIA:" + filepath.Join(outputs, "generated.png") + "\nkeep this",
		Media: []bus.MediaFile{{
			Path: filepath.Join(outputs, "generated.png"),
		}},
	}

	ApplyDelegationArtifactResultPolicy(ctx, result)

	if len(result.Media) != 0 {
		t.Fatalf("unpublished media remained: %#v", result.Media)
	}
	if strings.Contains(result.ForLLM, "MEDIA:") || !strings.Contains(result.ForLLM, "keep this") {
		t.Fatalf("artifact result policy = %q", result.ForLLM)
	}
}

func TestDelegationArtifactResultPolicyPreservesMediaDiscussion(t *testing.T) {
	ctx := delegationArtifactTestContext()
	result := &Result{
		ForLLM: "The literal marker MEDIA: is documented here.\nGenerated MEDIA:outputs/report.pdf successfully.",
	}

	ApplyDelegationArtifactResultPolicy(ctx, result)

	if !strings.Contains(result.ForLLM, "The literal marker MEDIA: is documented here.") {
		t.Fatalf("legitimate MEDIA discussion was removed: %q", result.ForLLM)
	}
	if strings.Contains(result.ForLLM, "MEDIA:outputs/report.pdf") {
		t.Fatalf("unpublished media path leaked: %q", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "Generated  successfully.") {
		t.Fatalf("surrounding tool result text was removed: %q", result.ForLLM)
	}
}
