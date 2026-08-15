package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// assistantMediaRefs returns the MediaRefs on the assistant message the stage
// persisted. Execute flushes and clears the buffer, so the flushed batch is the
// only place the final message can be observed.
func assistantMediaRefs(t *testing.T, flushed []providers.Message) []providers.MediaRef {
	t.Helper()
	for i := len(flushed) - 1; i >= 0; i-- {
		if flushed[i].Role == "assistant" {
			return flushed[i].MediaRefs
		}
	}
	t.Fatal("no assistant message persisted")
	return nil
}

// An agent that generates an image and then attaches a copy of it under a
// different name produces two entries for one picture. Deduplicating by path
// alone kept both, and the user received the same image twice in the chat.
func TestFinalizeStage_IdenticalImageUnderTwoPathsSendsOnce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	bytes := []byte("identical-image-bytes")
	generated := filepath.Join(dir, "poster_20260815-122109_291214.png")
	attached := filepath.Join(dir, "poster.jpg")
	for _, p := range []string{generated, attached} {
		if err := os.WriteFile(p, bytes, 0644); err != nil {
			t.Fatal(err)
		}
	}

	var flushed []providers.Message
	deps := &PipelineDeps{
		FlushMessages: func(_ context.Context, _ string, msgs []providers.Message) error {
			flushed = append(flushed, msgs...)
			return nil
		},
		UpdateMetadata: func(context.Context, string, providers.Usage, providers.Usage, int) error { return nil },
		PersistAssistantImages: func(msg *providers.Message, _ string) {
			msg.MediaRefs = append(msg.MediaRefs, providers.MediaRef{
				ID:       filepath.Base(generated),
				MimeType: "image/png",
				Kind:     "image",
				Path:     generated,
			})
			msg.Images = nil
		},
	}

	stage := NewFinalizeStage(deps)
	state := defaultState()
	state.Observe.FinalContent = "Đây anh."
	// The agent attached its own copy through a tool.
	state.Tool.MediaResults = []MediaResult{{Path: attached, ContentType: "image/jpeg"}}
	state.Observe.AssistantImages = []providers.ImageContent{
		{MimeType: "image/png", Data: "aWRlbnRpY2FsLWltYWdlLWJ5dGVz"},
	}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if len(state.Tool.MediaResults) != 1 {
		paths := make([]string, 0, len(state.Tool.MediaResults))
		for _, m := range state.Tool.MediaResults {
			paths = append(paths, m.Path)
		}
		t.Fatalf("MediaResults = %d entries %v, want 1 (same image delivered twice)", len(paths), paths)
	}
	// Session history must match what was delivered.
	if refs := assistantMediaRefs(t, flushed); len(refs) != 1 {
		t.Fatalf("assistant MediaRefs = %d, want 1", len(refs))
	}
}

// Different pictures must still both be delivered.
func TestFinalizeStage_DistinctImagesAreBothKept(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	generated := filepath.Join(dir, "generated.png")
	other := filepath.Join(dir, "other.png")
	if err := os.WriteFile(generated, []byte("first-image"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("second-image-different"), 0644); err != nil {
		t.Fatal(err)
	}

	var flushed []providers.Message
	deps := &PipelineDeps{
		FlushMessages: func(_ context.Context, _ string, msgs []providers.Message) error {
			flushed = append(flushed, msgs...)
			return nil
		},
		UpdateMetadata: func(context.Context, string, providers.Usage, providers.Usage, int) error { return nil },
		PersistAssistantImages: func(msg *providers.Message, _ string) {
			msg.MediaRefs = append(msg.MediaRefs, providers.MediaRef{
				ID: filepath.Base(generated), MimeType: "image/png", Kind: "image", Path: generated,
			})
			msg.Images = nil
		},
	}

	stage := NewFinalizeStage(deps)
	state := defaultState()
	state.Tool.MediaResults = []MediaResult{{Path: other, ContentType: "image/png"}}
	state.Observe.AssistantImages = []providers.ImageContent{
		{MimeType: "image/png", Data: "Zmlyc3QtaW1hZ2U="},
	}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if len(state.Tool.MediaResults) != 2 {
		t.Fatalf("MediaResults = %d entries, want 2 distinct images", len(state.Tool.MediaResults))
	}
	if refs := assistantMediaRefs(t, flushed); len(refs) != 2 {
		t.Fatalf("assistant MediaRefs = %d, want 2", len(refs))
	}
}

// A path repeated verbatim collapses without needing to read the file.
func TestFinalizeStage_RepeatedPathSendsOnce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	p := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(p, []byte("pdf"), 0644); err != nil {
		t.Fatal(err)
	}

	stage := NewFinalizeStage(&PipelineDeps{
		FlushMessages:  func(context.Context, string, []providers.Message) error { return nil },
		UpdateMetadata: func(context.Context, string, providers.Usage, providers.Usage, int) error { return nil },
	})
	state := defaultState()
	state.Tool.MediaResults = []MediaResult{
		{Path: p, ContentType: "application/pdf"},
		{Path: p, ContentType: "application/pdf"},
	}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if len(state.Tool.MediaResults) != 1 {
		t.Fatalf("MediaResults = %d entries, want 1", len(state.Tool.MediaResults))
	}
}

// Media the runtime cannot read on disk (remote or already cleaned up) must not
// be dropped just because content comparison is impossible.
func TestFinalizeStage_UnreadableMediaIsKept(t *testing.T) {
	t.Parallel()

	stage := NewFinalizeStage(&PipelineDeps{
		FlushMessages:  func(context.Context, string, []providers.Message) error { return nil },
		UpdateMetadata: func(context.Context, string, providers.Usage, providers.Usage, int) error { return nil },
	})
	state := defaultState()
	state.Tool.MediaResults = []MediaResult{
		{Path: "/nonexistent/a.png", ContentType: "image/png"},
		{Path: "/nonexistent/b.png", ContentType: "image/png"},
	}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if len(state.Tool.MediaResults) != 2 {
		t.Fatalf("MediaResults = %d entries, want 2 (unreadable media must survive)", len(state.Tool.MediaResults))
	}
}
