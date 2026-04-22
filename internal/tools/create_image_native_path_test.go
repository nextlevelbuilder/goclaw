package tools

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// nativeImageProvider is a minimal fake that satisfies providers.NativeImageProvider.
// It records call arguments so tests can assert correct routing.
type nativeImageProvider struct {
	name        string
	model       string
	calledWith  *providers.NativeImageRequest
	returnData  []byte
	returnError error
}

func (p *nativeImageProvider) Name() string         { return p.name }
func (p *nativeImageProvider) DefaultModel() string  { return p.model }
func (p *nativeImageProvider) Chat(_ context.Context, _ providers.ChatRequest) (*providers.ChatResponse, error) {
	return &providers.ChatResponse{}, nil
}
func (p *nativeImageProvider) ChatStream(_ context.Context, _ providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return &providers.ChatResponse{}, nil
}
func (p *nativeImageProvider) GenerateImage(_ context.Context, req providers.NativeImageRequest) (*providers.NativeImageResult, error) {
	p.calledWith = &req
	if p.returnError != nil {
		return nil, p.returnError
	}
	return &providers.NativeImageResult{
		MimeType: "image/png",
		Data:     p.returnData,
	}, nil
}

// TestCreateImageTool_RoutesNativePath verifies that when the provider chain resolves to
// a provider that implements NativeImageProvider (e.g. CodexProvider via OAuth),
// the create_image tool uses the native path (GenerateImage) and not the credentialProvider
// path. Specifically: the tool must NOT fail with "does not expose API credentials".
func TestCreateImageTool_RoutesNativePath(t *testing.T) {
	// Build a minimal 8-byte PNG-like data (not a real PNG, but large enough for
	// the tool to write to disk without crashing).
	pngMagic := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		// IHDR chunk (minimal valid PNG) — 25 bytes: len(13) + type + data + crc
		0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, // width = 1
		0x00, 0x00, 0x00, 0x01, // height = 1
		0x08, 0x02, 0x00, 0x00, 0x00,
		0x90, 0x77, 0x53, 0xde,
		// IDAT chunk (minimal: zlib compressed 1x1 pixel)
		0x00, 0x00, 0x00, 0x0c,
		0x49, 0x44, 0x41, 0x54,
		0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00, 0x00, 0x00, 0x02, 0x00, 0x01,
		0xe2, 0x21, 0xbc, 0x33,
		// IEND chunk
		0x00, 0x00, 0x00, 0x00,
		0x49, 0x45, 0x4e, 0x44,
		0xae, 0x42, 0x60, 0x82,
	}

	fakeProvider := &nativeImageProvider{
		name:       "openai-codex",
		model:      "gpt-image-2",
		returnData: pngMagic,
	}

	// Register provider in a fresh registry.
	reg := providers.NewRegistry(nil)
	reg.Register(fakeProvider)

	// Build a chain that points to the fake native provider.
	chain := []MediaProviderEntry{
		{
			Provider:   "openai-codex",
			Model:      "gpt-image-2",
			Enabled:    true,
			Timeout:    30,
			MaxRetries: 1,
		},
	}

	// Inject workspace context so the tool can write the file.
	ctx := WithToolWorkspace(context.Background(), t.TempDir())

	tool := NewCreateImageTool(reg)

	// Execute via the chain directly (same code path as Execute, bypassing chain resolution).
	chainResult, err := ExecuteWithChain(ctx, chain, reg, tool.callProvider)
	if err != nil {
		t.Fatalf("ExecuteWithChain returned error: %v — native path was NOT used (credentialProvider path instead)", err)
	}

	// Verify the fake provider was called via the native interface.
	if fakeProvider.calledWith == nil {
		t.Fatal("NativeImageProvider.GenerateImage was not called")
	}

	// The native provider's GenerateImage should have received a prompt.
	// (Prompt is injected by callProvider from params["prompt"].)
	// We cannot assert non-empty here without injecting params, but we can assert
	// the chain result contains the returned bytes.
	if len(chainResult.Data) == 0 {
		t.Error("chainResult.Data is empty — native path should return image bytes")
	}

	// Provider and model must be populated in the chain result.
	if chainResult.Provider != "openai-codex" {
		t.Errorf("chainResult.Provider = %q, want openai-codex", chainResult.Provider)
	}
	if chainResult.Model != "gpt-image-2" {
		t.Errorf("chainResult.Model = %q, want gpt-image-2", chainResult.Model)
	}
}

// TestCreateImageTool_RoutesNativePath_WithPrompt verifies end-to-end that the
// Execute method (with prompt in args) routes via the native path and sets
// MediaPrompts[0] on the result.
func TestCreateImageTool_RoutesNativePath_WithPrompt(t *testing.T) {
	pngMagic := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, // PNG signature
		// IEND chunk (minimal: enough for pngEmbedPrompt to process)
		0x00, 0x00, 0x00, 0x00,
		0x49, 0x45, 0x4e, 0x44,
		0xae, 0x42, 0x60, 0x82,
	}

	wantPrompt := "a sunny day at the beach"

	fakeProvider := &nativeImageProvider{
		name:       "openai-codex",
		model:      "gpt-image-2",
		returnData: pngMagic,
	}

	reg := providers.NewRegistry(nil)
	reg.Register(fakeProvider)

	// Inject per-agent provider override so chain resolves to our fake.
	chainJSON := []byte(`{"providers":[{"provider":"openai-codex","model":"gpt-image-2","enabled":true,"timeout":30,"max_retries":1}]}`)
	settings := BuiltinToolSettings{"create_image": chainJSON}
	ctx := WithBuiltinToolSettings(context.Background(), settings)
	ctx = WithToolWorkspace(ctx, t.TempDir())

	tool := NewCreateImageTool(reg)
	result := tool.Execute(ctx, map[string]any{
		"prompt":       wantPrompt,
		"aspect_ratio": "1:1",
	})

	if result.IsError {
		t.Fatalf("Execute returned error: %q", result.ForLLM)
	}

	// Verify NativeImageProvider.GenerateImage was called with the correct prompt.
	if fakeProvider.calledWith == nil {
		t.Fatal("GenerateImage was not called on the native provider")
	}
	if fakeProvider.calledWith.Prompt != wantPrompt {
		t.Errorf("GenerateImage called with prompt %q, want %q",
			fakeProvider.calledWith.Prompt, wantPrompt)
	}

	// MediaPrompts must carry the prompt so MediaRef.Prompt gets populated downstream.
	if result.MediaPrompts == nil || result.MediaPrompts[0] != wantPrompt {
		t.Errorf("result.MediaPrompts[0] = %q, want %q", result.MediaPrompts[0], wantPrompt)
	}
	// Media must have one entry.
	if len(result.Media) != 1 {
		t.Errorf("result.Media length = %d, want 1", len(result.Media))
	}
}

// TestCreateImageTool_ThreadsImageModel verifies that params["image_model"] from the
// chain entry is forwarded into NativeImageRequest.ImageModel. This covers the data
// flow: chain entry JSON → callProvider → GenerateImage.
func TestCreateImageTool_ThreadsImageModel(t *testing.T) {
	pngMagic := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, // PNG signature
		0x00, 0x00, 0x00, 0x00,
		0x49, 0x45, 0x4e, 0x44,
		0xae, 0x42, 0x60, 0x82,
	}

	tests := []struct {
		name            string
		chainImageModel string
		wantImageModel  string
	}{
		{
			name:            "default (empty params.image_model)",
			chainImageModel: "",
			wantImageModel:  "", // provider validator defaults to gpt-image-2
		},
		{
			name:            "legacy gpt-image-1.5",
			chainImageModel: "gpt-image-1.5",
			wantImageModel:  "gpt-image-1.5",
		},
		{
			name:            "explicit gpt-image-2",
			chainImageModel: "gpt-image-2",
			wantImageModel:  "gpt-image-2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fakeProvider := &nativeImageProvider{
				name:       "openai-codex",
				model:      "gpt-image-2",
				returnData: pngMagic,
			}

			reg := providers.NewRegistry(nil)
			reg.Register(fakeProvider)

			// Build chain entry with optional image_model param.
			entryParams := map[string]any{
				"prompt":       "test image",
				"aspect_ratio": "1:1",
			}
			if tc.chainImageModel != "" {
				entryParams["image_model"] = tc.chainImageModel
			}
			chain := []MediaProviderEntry{
				{
					Provider:   "openai-codex",
					Model:      "gpt-image-2",
					Enabled:    true,
					Timeout:    30,
					MaxRetries: 1,
					Params:     entryParams,
				},
			}

			ctx := WithToolWorkspace(context.Background(), t.TempDir())
			tool := NewCreateImageTool(reg)

			_, err := ExecuteWithChain(ctx, chain, reg, tool.callProvider)
			if err != nil {
				t.Fatalf("ExecuteWithChain returned error: %v", err)
			}

			if fakeProvider.calledWith == nil {
				t.Fatal("GenerateImage was not called on the native provider")
			}

			gotImageModel := fakeProvider.calledWith.ImageModel
			if gotImageModel != tc.wantImageModel {
				t.Errorf("NativeImageRequest.ImageModel = %q, want %q", gotImageModel, tc.wantImageModel)
			}
		})
	}
}
