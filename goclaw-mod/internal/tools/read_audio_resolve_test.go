package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

type testMediaPathLoader struct {
	path string
	root string
}

func (l testMediaPathLoader) LoadPath(string) (string, error) {
	return l.path, nil
}

func (l testMediaPathLoader) MediaRootPath() string {
	return l.root
}

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

func TestResolveAudioFileRequiresExactMediaID(t *testing.T) {
	workspace := t.TempDir()
	audioPath := filepath.Join(workspace, ".uploads", "recording.mp3")
	if err := os.MkdirAll(filepath.Dir(audioPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(audioPath, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := providers.MediaRef{
		ID:       uuid.NewString(),
		Kind:     "audio",
		Path:     audioPath,
		MimeType: "audio/mpeg",
	}
	tool := NewReadAudioTool(nil, nil)
	ctx := WithToolWorkspace(context.Background(), workspace)
	ctx = WithMediaAudioRefs(ctx, []providers.MediaRef{ref})

	if gotPath, _, err := tool.resolveAudioFile(ctx, "recording.mp3"); err == nil {
		t.Fatalf("non-ID value resolved to %q, want exact media_id error", gotPath)
	}
	gotPath, gotMime, err := tool.resolveAudioFile(ctx, ref.ID)
	if err != nil {
		t.Fatalf("exact media_id returned error: %v", err)
	}
	wantPath, err := filepath.EvalSymlinks(audioPath)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != wantPath || gotMime != "audio/mpeg" {
		t.Fatalf("resolved (%q, %q), want (%q, audio/mpeg)", gotPath, gotMime, wantPath)
	}
}

func TestResolveAudioFileOmittedMediaIDUsesNewestRef(t *testing.T) {
	workspace := t.TempDir()
	oldPath := filepath.Join(workspace, ".uploads", "old.mp3")
	latestPath := filepath.Join(workspace, ".uploads", "latest.mp3")
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{oldPath, latestPath} {
		if err := os.WriteFile(path, []byte("audio"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	refs := []providers.MediaRef{
		{ID: uuid.NewString(), Kind: "audio", Path: oldPath, MimeType: "audio/mpeg"},
		{ID: uuid.NewString(), Kind: "audio", Path: latestPath, MimeType: "audio/mpeg"},
	}
	ctx := WithToolWorkspace(context.Background(), workspace)
	ctx = WithMediaAudioRefs(ctx, refs)

	gotPath, _, err := NewReadAudioTool(nil, nil).resolveAudioFile(ctx, "")
	if err != nil {
		t.Fatalf("omitted media_id returned error: %v", err)
	}
	wantPath, err := filepath.EvalSymlinks(latestPath)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != wantPath {
		t.Fatalf("path = %q, want newest %q", gotPath, wantPath)
	}
}

func TestResolveAudioFileRejectsRefPathOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	outsidePath := filepath.Join(t.TempDir(), "secret.mp3")
	if err := os.WriteFile(outsidePath, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := providers.MediaRef{
		ID:       uuid.NewString(),
		Kind:     "audio",
		Path:     outsidePath,
		MimeType: "audio/mpeg",
	}
	ctx := WithToolWorkspace(context.Background(), workspace)
	ctx = WithMediaAudioRefs(ctx, []providers.MediaRef{ref})

	gotPath, _, err := NewReadAudioTool(nil, nil).resolveAudioFile(ctx, ref.ID)
	if err == nil {
		t.Fatalf("outside ref path resolved to %q, want containment error", gotPath)
	}
}

func TestResolveAudioFileRejectsLegacyLoaderPathOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	outsidePath := filepath.Join(t.TempDir(), "legacy.mp3")
	if err := os.WriteFile(outsidePath, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := providers.MediaRef{
		ID:       uuid.NewString(),
		Kind:     "audio",
		MimeType: "audio/mpeg",
	}
	ctx := WithToolWorkspace(context.Background(), workspace)
	ctx = WithMediaAudioRefs(ctx, []providers.MediaRef{ref})

	gotPath, _, err := NewReadAudioTool(nil, testMediaPathLoader{path: outsidePath}).resolveAudioFile(ctx, ref.ID)
	if err == nil {
		t.Fatalf("outside legacy loader path resolved to %q, want containment error", gotPath)
	}
}

func TestResolveAudioFileAcceptsConfiguredLegacyMediaRoot(t *testing.T) {
	workspace := t.TempDir()
	mediaRoot := filepath.Join(t.TempDir(), ".media")
	audioPath := filepath.Join(mediaRoot, "session-hash", "legacy.mp3")
	if err := os.MkdirAll(filepath.Dir(audioPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(audioPath, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := providers.MediaRef{
		ID:       uuid.NewString(),
		Kind:     "audio",
		MimeType: "audio/mpeg",
	}
	ctx := WithToolWorkspace(context.Background(), workspace)
	ctx = WithMediaAudioRefs(ctx, []providers.MediaRef{ref})

	gotPath, _, err := NewReadAudioTool(nil, testMediaPathLoader{
		path: audioPath,
		root: mediaRoot,
	}).resolveAudioFile(ctx, ref.ID)
	if err != nil {
		t.Fatalf("configured legacy media path returned error: %v", err)
	}
	wantPath, err := filepath.EvalSymlinks(audioPath)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != wantPath {
		t.Fatalf("path = %q, want %q", gotPath, wantPath)
	}
}

func TestResolveAudioFileAcceptsLegacyLoaderPathInsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	audioPath := filepath.Join(workspace, ".uploads", "legacy.mp3")
	if err := os.MkdirAll(filepath.Dir(audioPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(audioPath, []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := providers.MediaRef{
		ID:       uuid.NewString(),
		Kind:     "audio",
		MimeType: "audio/mpeg",
	}
	ctx := WithToolWorkspace(context.Background(), workspace)
	ctx = WithMediaAudioRefs(ctx, []providers.MediaRef{ref})

	gotPath, _, err := NewReadAudioTool(nil, testMediaPathLoader{path: audioPath}).resolveAudioFile(ctx, ref.ID)
	if err != nil {
		t.Fatalf("inside legacy loader path returned error: %v", err)
	}
	wantPath, err := filepath.EvalSymlinks(audioPath)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != wantPath {
		t.Fatalf("path = %q, want %q", gotPath, wantPath)
	}
}

func TestResolveAudioFileAcceptsDelegationInputRef(t *testing.T) {
	ctx, inputs, _ := delegationArtifactToolContext(t)
	audioPath := filepath.Join(inputs, "recording.mp3")
	if err := os.WriteFile(audioPath, []byte("audio"), 0o440); err != nil {
		t.Fatal(err)
	}
	ref := providers.MediaRef{
		ID:       uuid.NewString(),
		Kind:     "audio",
		Path:     audioPath,
		MimeType: "audio/mpeg",
	}
	ctx = WithMediaAudioRefs(ctx, []providers.MediaRef{ref})

	gotPath, _, err := NewReadAudioTool(nil, nil).resolveAudioFile(ctx, ref.ID)
	if err != nil {
		t.Fatalf("delegation input ref returned error: %v", err)
	}
	wantPath, err := filepath.EvalSymlinks(audioPath)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != wantPath {
		t.Fatalf("path = %q, want staged input %q", gotPath, wantPath)
	}
}
