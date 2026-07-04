package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// A model-generated image (Codex native image_generation) must reach
// RunResult.Media — not just the assistant message's MediaRefs — otherwise
// chat channels never receive it (only the web UI reads session history).
func TestFinalizeStage_GeneratedImageReachesMediaResults(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "generated.png")
	if err := os.WriteFile(imgPath, []byte("fake-png-bytes"), 0644); err != nil {
		t.Fatal(err)
	}

	deps := &PipelineDeps{
		FlushMessages:  func(context.Context, string, []providers.Message) error { return nil },
		UpdateMetadata: func(context.Context, string, providers.Usage, int) error { return nil },
		// Simulates persistAssistantImages: real implementation decodes base64,
		// writes to workspace/media/, and appends MediaRefs to msg.
		PersistAssistantImages: func(msg *providers.Message, _ string) {
			msg.MediaRefs = append(msg.MediaRefs, providers.MediaRef{
				ID:       "img-1",
				MimeType: "image/png",
				Kind:     "image",
				Path:     imgPath,
				Prompt:   "a luxury jewelry poster",
			})
			msg.Images = nil
		},
	}

	stage := NewFinalizeStage(deps)
	state := defaultState()
	state.Observe.FinalContent = "Here's the poster."
	state.Observe.AssistantImages = []providers.ImageContent{
		{MimeType: "image/png", Data: "ZmFrZS1wbmctYnl0ZXM="},
	}

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if len(state.Tool.MediaResults) != 1 {
		t.Fatalf("MediaResults = %d entries, want 1 (generated image missing from outbound media)", len(state.Tool.MediaResults))
	}
	got := state.Tool.MediaResults[0]
	if got.Path != imgPath {
		t.Errorf("MediaResults[0].Path = %q, want %q", got.Path, imgPath)
	}
	if got.ContentType != "image/png" {
		t.Errorf("MediaResults[0].ContentType = %q, want image/png", got.ContentType)
	}
	if got.Prompt != "a luxury jewelry poster" {
		t.Errorf("MediaResults[0].Prompt = %q, want the generation prompt", got.Prompt)
	}
	if got.Size != int64(len("fake-png-bytes")) {
		t.Errorf("MediaResults[0].Size = %d, want %d (stat'd from disk)", got.Size, len("fake-png-bytes"))
	}
}

// No generated images → no spurious MediaResults entries.
func TestFinalizeStage_NoGeneratedImagesNoMediaResults(t *testing.T) {
	t.Parallel()

	deps := &PipelineDeps{
		FlushMessages:  func(context.Context, string, []providers.Message) error { return nil },
		UpdateMetadata: func(context.Context, string, providers.Usage, int) error { return nil },
	}

	stage := NewFinalizeStage(deps)
	state := defaultState()
	state.Observe.FinalContent = "just text, no image"

	if err := stage.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if len(state.Tool.MediaResults) != 0 {
		t.Fatalf("MediaResults = %d entries, want 0", len(state.Tool.MediaResults))
	}
}
