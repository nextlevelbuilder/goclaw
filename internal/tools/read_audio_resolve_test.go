package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

type readAudioUnsupportedProvider struct {
	name      string
	chatCalls int
	images    int
}

func (p *readAudioUnsupportedProvider) Name() string         { return p.name }
func (p *readAudioUnsupportedProvider) DefaultModel() string { return "claude-3-5-sonnet-latest" }

func (p *readAudioUnsupportedProvider) Chat(_ context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	p.chatCalls++
	for _, msg := range req.Messages {
		p.images += len(msg.Images)
	}
	return &providers.ChatResponse{Content: "unexpected"}, nil
}

func (p *readAudioUnsupportedProvider) ChatStream(_ context.Context, _ providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return nil, nil
}

// TestReadAudioCallProvider_TranscriptionModelWithoutCreds_FailsFast asserts
// that when no API credentials are present, a transcription-named model
// returns a clear error rather than silently falling back to chat/completions
// (which would then explode in a confusing way for transcription-only setups).
func TestReadAudioCallProvider_TranscriptionModelWithoutCreds_FailsFast(t *testing.T) {
	tool := &ReadAudioTool{}

	params := map[string]any{
		"_provider_type": "openai",
		"data":           []byte{0x00, 0x01},
		"mime":           "audio/mpeg",
	}

	_, _, err := tool.callProvider(context.Background(), nil, "openai", "gpt-4o-mini-transcribe", params)
	if err == nil {
		t.Fatalf("expected fail-fast error for transcription model with nil credentials, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "credential") {
		t.Errorf("expected error to mention credentials, got: %v", err)
	}
}

// TestReadAudioCallProvider_TranscriptionModelWithoutCreds_OpenAICompat_FailsFast
// covers the openai_compat ptype variant — the bug the original PR found:
// previously a transcription model under openai_compat fell through to the
// generic chat-API fallback because only ptype=="openai" entered the
// transcription branch.
func TestReadAudioCallProvider_TranscriptionModelWithoutCreds_OpenAICompat_FailsFast(t *testing.T) {
	tool := &ReadAudioTool{}

	params := map[string]any{
		"_provider_type": "openai_compat",
		"data":           []byte{0x00, 0x01},
		"mime":           "audio/mpeg",
	}

	_, _, err := tool.callProvider(context.Background(), nil, "dashscope", "whisper-1", params)
	if err == nil {
		t.Fatalf("expected fail-fast error for transcription model with nil credentials (openai_compat), got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "credential") {
		t.Errorf("expected error to mention credentials, got: %v", err)
	}
}

// TestReadAudioCallProvider_GeminiWithoutCreds_FailsFast preserves the existing
// gemini fail-fast behavior (was previously a soft log + fallback that would
// then NPE on the registry path in tests; the broader guard makes it explicit).
func TestReadAudioCallProvider_GeminiWithoutCreds_FailsFast(t *testing.T) {
	tool := &ReadAudioTool{}

	params := map[string]any{
		"_provider_type": "gemini",
		"data":           []byte{0x00, 0x01},
		"mime":           "audio/mpeg",
	}

	_, _, err := tool.callProvider(context.Background(), nil, "gemini", "gemini-2.5-flash", params)
	if err == nil {
		t.Fatalf("expected fail-fast error for gemini with nil credentials, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "credential") {
		t.Errorf("expected error to mention credentials, got: %v", err)
	}
}

func TestReadAudioCallProvider_UnsupportedProviderDoesNotSendAudioAsImage(t *testing.T) {
	reg := providers.NewRegistry(nil)
	fake := &readAudioUnsupportedProvider{name: "anthropic"}
	reg.Register(fake)
	tool := &ReadAudioTool{registry: reg}

	params := map[string]any{
		"_provider_type": "anthropic",
		"data":           []byte{0x4f, 0x67, 0x67, 0x53},
		"mime":           "audio/ogg; codecs=opus",
	}

	_, _, err := tool.callProvider(context.Background(), nil, "anthropic", "claude-3-5-sonnet-latest", params)
	if err == nil {
		t.Fatalf("expected unsupported audio route error, got nil")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "unsupported audio route") {
		t.Fatalf("expected unsupported audio route error, got: %v", err)
	}
	if !strings.Contains(msg, "anthropic") || !strings.Contains(msg, "claude-3-5-sonnet-latest") {
		t.Fatalf("expected provider and model in error, got: %v", err)
	}
	if fake.chatCalls != 0 || fake.images != 0 {
		t.Fatalf("unsupported audio route called chat fallback: calls=%d images=%d", fake.chatCalls, fake.images)
	}
}
