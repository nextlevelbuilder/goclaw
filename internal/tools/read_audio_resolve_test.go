package tools

import (
	"context"
	"strings"
	"testing"
)

func TestReadAudioCallProvider_TranscriptionModelWithoutCreds_FailsFast(t *testing.T) {
	tool := &ReadAudioTool{}
	params := map[string]any{
		"_provider_type": "openai",
		"mime":           "audio/mpeg",
		"data":           []byte("fake"),
	}

	_, _, err := tool.callProvider(context.Background(), nil, "openai", "gpt-4o-mini-transcribe", params)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "requires API credentials") {
		t.Fatalf("unexpected error: %v", err)
	}
}
