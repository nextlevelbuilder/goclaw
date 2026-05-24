package providers

import (
	"context"
	"errors"
	"testing"
)

type testFallbackProvider struct {
	name      string
	model     string
	err       error
	streamErr error
	calls     int
}

func (p *testFallbackProvider) Chat(_ context.Context, req ChatRequest) (*ChatResponse, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return &ChatResponse{Content: req.Model, FinishReason: "stop"}, nil
}

func (p *testFallbackProvider) ChatStream(_ context.Context, req ChatRequest, onChunk func(StreamChunk)) (*ChatResponse, error) {
	p.calls++
	if p.streamErr != nil {
		if req.Model == "primary-model" {
			onChunk(StreamChunk{Content: "partial"})
		}
		return nil, p.streamErr
	}
	return &ChatResponse{Content: req.Model, FinishReason: "stop"}, nil
}

func (p *testFallbackProvider) DefaultModel() string { return p.model }
func (p *testFallbackProvider) Name() string         { return p.name }

func TestModelFallbackProviderFallsBackOnClassifiedError(t *testing.T) {
	primary := &testFallbackProvider{
		name:  "primary",
		model: "primary-model",
		err:   &HTTPError{Status: 429, Body: "rate limited"},
	}
	backup := &testFallbackProvider{name: "backup", model: "backup-model"}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "primary",
		Provider:     primary,
		Model:        "primary-model",
	}, []FallbackCandidate{
		{ProviderName: "backup", Provider: backup, Model: "backup-model"},
	}, 2, false)

	resp, err := provider.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "backup-model" {
		t.Fatalf("Chat() content = %q, want backup model", resp.Content)
	}
	if primary.calls != 1 || backup.calls != 1 {
		t.Fatalf("calls primary=%d backup=%d, want 1/1", primary.calls, backup.calls)
	}
}

func TestModelFallbackProviderDoesNotFallbackAfterStreamChunk(t *testing.T) {
	streamErr := &HTTPError{Status: 429, Body: "rate limited"}
	primary := &testFallbackProvider{
		name:      "primary",
		model:     "primary-model",
		streamErr: streamErr,
	}
	backup := &testFallbackProvider{name: "backup", model: "backup-model"}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "primary",
		Provider:     primary,
		Model:        "primary-model",
	}, []FallbackCandidate{
		{ProviderName: "backup", Provider: backup, Model: "backup-model"},
	}, 2, false)

	var chunks int
	_, err := provider.ChatStream(context.Background(), ChatRequest{}, func(StreamChunk) {
		chunks++
	})
	if err == nil {
		t.Fatal("ChatStream() error = nil, want primary stream error")
	}
	if chunks != 1 {
		t.Fatalf("chunks = %d, want 1", chunks)
	}
	if backup.calls != 0 {
		t.Fatalf("backup calls = %d, want 0 after partial stream", backup.calls)
	}
}

func TestModelFallbackProviderFallsBackToSameModelOnDifferentProvider(t *testing.T) {
	primary := &testFallbackProvider{
		name:  "primary",
		model: "shared-model",
		err:   &HTTPError{Status: 404, Body: "model not found"},
	}
	backup := &testFallbackProvider{name: "backup", model: "shared-model"}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "primary",
		Provider:     primary,
		Model:        "shared-model",
	}, []FallbackCandidate{
		{ProviderName: "backup", Provider: backup, Model: "shared-model"},
	}, 0, false)

	resp, err := provider.Chat(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "shared-model" {
		t.Fatalf("Chat() content = %q, want shared model from backup", resp.Content)
	}
	if primary.calls != 1 || backup.calls != 1 {
		t.Fatalf("calls primary=%d backup=%d, want 1/1", primary.calls, backup.calls)
	}
}

func TestModelFallbackProviderDoesNotFallbackOnUnknownError(t *testing.T) {
	unknownErr := errors.New("request serialization failed")
	primary := &testFallbackProvider{
		name:  "primary",
		model: "primary-model",
		err:   unknownErr,
	}
	backup := &testFallbackProvider{name: "backup", model: "backup-model"}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "primary",
		Provider:     primary,
		Model:        "primary-model",
	}, []FallbackCandidate{
		{ProviderName: "backup", Provider: backup, Model: "backup-model"},
	}, 0, false)

	_, err := provider.Chat(context.Background(), ChatRequest{})
	if !errors.Is(err, unknownErr) {
		t.Fatalf("Chat() error = %v, want original unknown error", err)
	}
	if backup.calls != 0 {
		t.Fatalf("backup calls = %d, want 0 for unknown error", backup.calls)
	}
}

func TestModelFallbackProviderMaxAttemptsCapsTotalAttempts(t *testing.T) {
	primary := &testFallbackProvider{
		name:  "primary",
		model: "primary-model",
		err:   &HTTPError{Status: 429, Body: "rate limited"},
	}
	backup := &testFallbackProvider{name: "backup", model: "backup-model"}
	provider := NewModelFallbackProvider(FallbackCandidate{
		ProviderName: "primary",
		Provider:     primary,
		Model:        "primary-model",
	}, []FallbackCandidate{
		{ProviderName: "backup", Provider: backup, Model: "backup-model"},
	}, 1, false)

	_, err := provider.Chat(context.Background(), ChatRequest{})
	if err == nil {
		t.Fatal("Chat() error = nil, want exhausted after primary only")
	}
	if primary.calls != 1 || backup.calls != 0 {
		t.Fatalf("calls primary=%d backup=%d, want 1/0", primary.calls, backup.calls)
	}
}
