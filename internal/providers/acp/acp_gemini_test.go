package acp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestGeminiProtocolMapping(t *testing.T) {
	if os.Getenv("ACP_GEMINI_E2E") == "" {
		t.Skip("set ACP_GEMINI_E2E=1 to run this test (requires gemini binary)")
	}
	// Enable debug logging to stderr
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pp := NewProcessPool("gemini", []string{"--acp", "--yolo"}, ".", 10*time.Minute)
	defer pp.Close()

	proc, err := pp.GetOrSpawn(ctx, "test-session")
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	sid, err := proc.NewSession(ctx)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	fmt.Println("-> Testing Gemini Protocol Mapping...")

	var collectedText string
	_, err = proc.Prompt(ctx, sid, []ContentBlock{
		{Type: "text", Text: "Hello, reply with 'ACP_SUCCESS' and nothing else."},
	}, func(su SessionUpdate) {
		if su.Message != nil {
			for _, b := range su.Message.Content {
				if b.Type == "text" {
					fmt.Printf("<- MAPPED CHUNK: %s\n", b.Text)
					collectedText += b.Text
				}
			}
		}
	})

	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	fmt.Printf("== COLLECTED TOTAL: %s ==\n", collectedText)
	if collectedText == "" {
		t.Error("No text collected via mapped Message field!")
	}
}
