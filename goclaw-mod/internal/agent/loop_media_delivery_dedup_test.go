package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func TestProcessToolResultBlocksMessageSelfSendForQueuedMedia(t *testing.T) {
	workspace := t.TempDir()
	workspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(workspace, "generated-image.png")
	if err := os.WriteFile(mediaPath, []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}

	delivered := tools.NewDeliveredMedia()
	ctx := context.Background()
	ctx = tools.WithToolWorkspace(ctx, workspace)
	ctx = tools.WithToolChannel(ctx, "lark-test")
	ctx = tools.WithToolChatID(ctx, "chat-1")
	ctx = tools.WithDeliveredMedia(ctx, delivered)

	loop := &Loop{id: "test-agent"}
	state := &runState{}
	loop.processToolResult(
		ctx,
		state,
		&RunRequest{RunID: "run-1"},
		func(AgentEvent) {},
		providers.ToolCall{ID: "create-1", Name: "create_image"},
		"create_image",
		&tools.Result{
			ForLLM: "MEDIA:" + mediaPath,
			Media: []bus.MediaFile{{
				Path:     mediaPath,
				MimeType: "image/png",
			}},
		},
		false,
	)

	if len(state.mediaResults) != 1 {
		t.Fatalf("queued media = %d, want 1", len(state.mediaResults))
	}

	messageTool := tools.NewMessageTool(workspace, true)
	messageTool.SetMessageBus(bus.New())
	result := messageTool.Execute(ctx, map[string]any{
		"action":  "send",
		"channel": "lark-test",
		"target":  "chat-1",
		"message": "MEDIA:" + mediaPath,
	})
	if !result.IsError {
		t.Fatal("message self-send succeeded for media already queued by the agent loop")
	}
	if !strings.Contains(result.ForLLM, "already queued for automatic delivery") {
		t.Fatalf("message self-send blocked for the wrong reason: %q", result.ForLLM)
	}

	otherPath := filepath.Join(workspace, "manual-attachment.txt")
	if err := os.WriteFile(otherPath, []byte("attachment"), 0o644); err != nil {
		t.Fatal(err)
	}
	result = messageTool.Execute(ctx, map[string]any{
		"action":  "send",
		"channel": "lark-test",
		"target":  "chat-1",
		"message": "Attachments:\nMEDIA:" + mediaPath + "\nMEDIA:" + otherPath,
	})
	if !result.IsError {
		t.Fatal("mixed message self-send included media already queued by the agent loop")
	}
	if !strings.Contains(result.ForLLM, "already queued for automatic delivery") {
		t.Fatalf("mixed message self-send blocked for the wrong reason: %q", result.ForLLM)
	}
}

func TestProcessToolResultMarksLegacyMediaAsQueued(t *testing.T) {
	workspace := t.TempDir()
	workspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(workspace, "legacy-output.pdf")
	if err := os.WriteFile(mediaPath, []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}

	delivered := tools.NewDeliveredMedia()
	ctx := tools.WithToolWorkspace(context.Background(), workspace)
	ctx = tools.WithDeliveredMedia(ctx, delivered)

	state := &runState{}
	(&Loop{id: "test-agent"}).processToolResult(
		ctx,
		state,
		&RunRequest{RunID: "run-legacy"},
		func(AgentEvent) {},
		providers.ToolCall{ID: "exec-1", Name: "exec"},
		"exec",
		&tools.Result{ForLLM: "MEDIA:" + mediaPath},
		false,
	)

	if len(state.mediaResults) != 1 {
		t.Fatalf("queued legacy media = %d, want 1", len(state.mediaResults))
	}
	if !delivered.IsDelivered(mediaPath) {
		t.Fatalf("legacy media %q was not marked for automatic delivery", mediaPath)
	}
}
