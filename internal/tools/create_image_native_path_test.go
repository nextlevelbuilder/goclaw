package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
func (p *nativeImageProvider) DefaultModel() string { return p.model }
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

func TestCreateImageTool_ResolveReferenceImage_Path(t *testing.T) {
	tmpDir := t.TempDir()
	refFile := filepath.Join(tmpDir, "ref.png")
	pngBytes := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(refFile, pngBytes, 0644); err != nil {
		t.Fatalf("failed to write temp ref file: %v", err)
	}

	fakeProvider := &nativeImageProvider{
		name:       "openai-codex",
		model:      "gpt-image-2",
		returnData: pngBytes,
	}

	reg := providers.NewRegistry(nil)
	reg.Register(fakeProvider)

	tool := NewCreateImageTool(reg)
	ctx := WithToolWorkspace(context.Background(), tmpDir)

	chainJSON := []byte(`{"providers":[{"provider":"openai-codex","model":"gpt-image-2","enabled":true,"timeout":30,"max_retries":1}]}`)
	settings := BuiltinToolSettings{"create_image": chainJSON}
	ctx = WithBuiltinToolSettings(ctx, settings)

	result := tool.Execute(ctx, map[string]any{
		"prompt":     "generate a picture",
		"ref_images": []any{map[string]any{"path": refFile}},
	})

	if result.IsError {
		t.Fatalf("Execute returned error: %q", result.ForLLM)
	}

	if fakeProvider.calledWith == nil {
		t.Fatal("GenerateImage was not called")
	}

	if len(fakeProvider.calledWith.RefImages) != 1 {
		t.Fatalf("expected 1 reference image, got %d", len(fakeProvider.calledWith.RefImages))
	}
	if fakeProvider.calledWith.RefImages[0].Base64 == "" {
		t.Error("RefImages[0].Base64 was not populated")
	}
	if fakeProvider.calledWith.RefImages[0].Strength != 0.6 {
		t.Errorf("RefImages[0].Strength = %f, want 0.6", fakeProvider.calledWith.RefImages[0].Strength)
	}
}

func TestCreateImageTool_ResolveReferenceImage_URL(t *testing.T) {
	fakeProvider := &nativeImageProvider{
		name:       "openai-codex",
		model:      "gpt-image-2",
		returnData: []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a},
	}

	reg := providers.NewRegistry(nil)
	reg.Register(fakeProvider)

	tool := NewCreateImageTool(reg)
	ctx := WithToolWorkspace(context.Background(), t.TempDir())

	chainJSON := []byte(`{"providers":[{"provider":"openai-codex","model":"gpt-image-2","enabled":true,"timeout":30,"max_retries":1}]}`)
	settings := BuiltinToolSettings{"create_image": chainJSON}
	ctx = WithBuiltinToolSettings(ctx, settings)

	result := tool.Execute(ctx, map[string]any{
		"prompt":     "generate a picture",
		"ref_images": []any{map[string]any{"url": "https://example.com/ref.png"}},
	})

	if result.IsError {
		t.Fatalf("Execute returned error: %q", result.ForLLM)
	}

	if fakeProvider.calledWith == nil {
		t.Fatal("GenerateImage was not called")
	}

	if len(fakeProvider.calledWith.RefImages) != 1 {
		t.Fatalf("expected 1 reference image, got %d", len(fakeProvider.calledWith.RefImages))
	}
	if fakeProvider.calledWith.RefImages[0].URL != "https://example.com/ref.png" {
		t.Errorf("RefImageUrl = %q, want https://example.com/ref.png", fakeProvider.calledWith.RefImages[0].URL)
	}
}

func TestCreateImageTool_ResolveReferenceImage_ID(t *testing.T) {
	fakeProvider := &nativeImageProvider{
		name:       "openai-codex",
		model:      "gpt-image-2",
		returnData: []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a},
	}

	reg := providers.NewRegistry(nil)
	reg.Register(fakeProvider)

	tool := NewCreateImageTool(reg)
	ctx := WithToolWorkspace(context.Background(), t.TempDir())

	// Put mock image content in context
	mockImg := providers.ImageContent{
		MimeType: "image/png",
		Data:     "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==", // 1x1 png base64
	}
	ctx = WithMediaImages(ctx, []providers.ImageContent{mockImg})

	chainJSON := []byte(`{"providers":[{"provider":"openai-codex","model":"gpt-image-2","enabled":true,"timeout":30,"max_retries":1}]}`)
	settings := BuiltinToolSettings{"create_image": chainJSON}
	ctx = WithBuiltinToolSettings(ctx, settings)

	result := tool.Execute(ctx, map[string]any{
		"prompt":     "generate a picture",
		"ref_images": []any{map[string]any{"id": "latest"}},
	})

	if result.IsError {
		t.Fatalf("Execute returned error: %q", result.ForLLM)
	}

	if fakeProvider.calledWith == nil {
		t.Fatal("GenerateImage was not called")
	}

	if len(fakeProvider.calledWith.RefImages) != 1 {
		t.Fatalf("expected 1 reference image, got %d", len(fakeProvider.calledWith.RefImages))
	}
	if fakeProvider.calledWith.RefImages[0].Base64 != mockImg.Data {
		t.Errorf("RefImageBase64 = %q, want %q", fakeProvider.calledWith.RefImages[0].Base64, mockImg.Data)
	}
}

func TestCreateImageTool_MultipleReferenceImages_MixedSources(t *testing.T) {
	tmpDir := t.TempDir()
	refFile := filepath.Join(tmpDir, "ref.png")
	pngBytes := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(refFile, pngBytes, 0644); err != nil {
		t.Fatalf("failed to write temp ref file: %v", err)
	}

	fakeProvider := &nativeImageProvider{
		name:       "openai-codex",
		model:      "gpt-image-2",
		returnData: pngBytes,
	}

	reg := providers.NewRegistry(nil)
	reg.Register(fakeProvider)

	tool := NewCreateImageTool(reg)
	ctx := WithToolWorkspace(context.Background(), tmpDir)

	chainJSON := []byte(`{"providers":[{"provider":"openai-codex","model":"gpt-image-2","enabled":true,"timeout":30,"max_retries":1}]}`)
	settings := BuiltinToolSettings{"create_image": chainJSON}
	ctx = WithBuiltinToolSettings(ctx, settings)

	result := tool.Execute(ctx, map[string]any{
		"prompt": "generate a picture",
		"ref_images": []any{
			map[string]any{
				"path":     refFile,
				"strength": 0.9,
			},
			map[string]any{
				"url":      "https://example.com/ref2.png",
				"strength": 0.4,
			},
		},
	})

	if result.IsError {
		t.Fatalf("Execute returned error: %q", result.ForLLM)
	}

	if fakeProvider.calledWith == nil {
		t.Fatal("GenerateImage was not called")
	}

	if len(fakeProvider.calledWith.RefImages) != 2 {
		t.Fatalf("expected 2 reference images, got %d", len(fakeProvider.calledWith.RefImages))
	}

	// Verify image 1 (path)
	if fakeProvider.calledWith.RefImages[0].Base64 == "" {
		t.Error("RefImages[0].Base64 was not populated")
	}
	if fakeProvider.calledWith.RefImages[0].Strength != 0.9 {
		t.Errorf("RefImages[0].Strength = %f, want 0.9", fakeProvider.calledWith.RefImages[0].Strength)
	}

	// Verify image 2 (url)
	if fakeProvider.calledWith.RefImages[1].URL != "https://example.com/ref2.png" {
		t.Errorf("RefImages[1].URL = %q, want https://example.com/ref2.png", fakeProvider.calledWith.RefImages[1].URL)
	}
	if fakeProvider.calledWith.RefImages[1].Strength != 0.4 {
		t.Errorf("RefImages[1].Strength = %f, want 0.4", fakeProvider.calledWith.RefImages[1].Strength)
	}
}

type fakeCreds struct {
	key  string
	base string
}

func (c fakeCreds) APIKey() string  { return c.key }
func (c fakeCreds) APIBase() string { return c.base }

func TestCreateImageTool_OpenAIEdits_JSON(t *testing.T) {
	pngBytes := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52}
	mockResponse := `{
		"data": [
			{
				"b64_json": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
			}
		]
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/edits" {
			t.Errorf("expected path /images/edits, got %s", r.URL.Path)
		}
		ct := r.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "application/json") {
			t.Errorf("expected application/json content type, got %s", ct)
		}
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read body: %v", err)
		}
		var reqBody struct {
			Model          string `json:"model"`
			Prompt         string `json:"prompt"`
			ResponseFormat string `json:"response_format"`
			Images         []struct {
				ImageURL string `json:"image_url"`
			} `json:"images"`
		}
		if err := json.Unmarshal(bodyBytes, &reqBody); err != nil {
			t.Fatalf("failed to unmarshal body: %v", err)
		}
		if reqBody.Prompt != "sunny beach" {
			t.Errorf("expected prompt 'sunny beach', got %s", reqBody.Prompt)
		}
		if reqBody.Model != "gpt-image-2" {
			t.Errorf("expected model 'gpt-image-2', got %s", reqBody.Model)
		}
		if reqBody.ResponseFormat != "b64_json" {
			t.Errorf("expected response_format 'b64_json', got %s", reqBody.ResponseFormat)
		}
		if len(reqBody.Images) != 1 {
			t.Fatalf("expected 1 image, got %d", len(reqBody.Images))
		}
		expectedURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)
		if reqBody.Images[0].ImageURL != expectedURL {
			t.Errorf("image url mismatch")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResponse))
	}))
	defer server.Close()

	creds := fakeCreds{key: "fake-key", base: server.URL}
	reg := providers.NewRegistry(nil)
	tool := NewCreateImageTool(reg)
	ctx := WithToolWorkspace(context.Background(), t.TempDir())

	refImg := &referenceImage{
		Data:     pngBytes,
		MimeType: "image/png",
	}

	params := map[string]any{
		"prompt":         "sunny beach",
		"ref_images":     []*referenceImage{refImg},
		"_provider_type": "openai",
	}

	imageBytes, _, err := tool.callProvider(ctx, creds, "openai", "gpt-image-2", params)
	if err != nil {
		t.Fatalf("callProvider returned error: %v", err)
	}

	if len(imageBytes) == 0 {
		t.Error("returned imageBytes is empty")
	}
}

func TestCreateImageTool_DeduplicateReferenceImages(t *testing.T) {
	fakeProvider := &nativeImageProvider{
		name:       "openai-codex",
		model:      "gpt-image-2",
		returnData: []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a},
	}

	reg := providers.NewRegistry(nil)
	reg.Register(fakeProvider)

	tool := NewCreateImageTool(reg)
	ctx := WithToolWorkspace(context.Background(), t.TempDir())

	chainJSON := []byte(`{"providers":[{"provider":"openai-codex","model":"gpt-image-2","enabled":true,"timeout":30,"max_retries":1}]}`)
	settings := BuiltinToolSettings{"create_image": chainJSON}
	ctx = WithBuiltinToolSettings(ctx, settings)

	// Pass duplicate parameters as sent by the user
	url1 := "https://example.com/image1.png"
	url2 := "https://example.com/image2.png"

	result := tool.Execute(ctx, map[string]any{
		"prompt": "generate a picture",
		"ref_images": []any{
			map[string]any{
				"url":      url1,
				"strength": 0.8,
			},
			map[string]any{
				"url":      url2,
				"strength": 0.8,
			},
			map[string]any{
				"url":      url1, // Duplicated
				"strength": 0.8,
			},
			map[string]any{
				"url":      url2, // Duplicated
				"strength": 0.8,
			},
		},
	})

	if result.IsError {
		t.Fatalf("Execute returned error: %q", result.ForLLM)
	}

	if fakeProvider.calledWith == nil {
		t.Fatal("GenerateImage was not called")
	}

	// Verify that the length is 2 instead of 4
	gotLen := len(fakeProvider.calledWith.RefImages)
	if gotLen != 2 {
		t.Fatalf("expected 2 unique reference images, got %d (duplicated URL error!)", gotLen)
	}

	// Verify that the priority order of ref_images is preserved (strength = 0.8)
	if fakeProvider.calledWith.RefImages[0].Strength != 0.8 {
		t.Errorf("RefImages[0].Strength = %f, want 0.8", fakeProvider.calledWith.RefImages[0].Strength)
	}
	if fakeProvider.calledWith.RefImages[1].Strength != 0.8 {
		t.Errorf("RefImages[1].Strength = %f, want 0.8", fakeProvider.calledWith.RefImages[1].Strength)
	}
}

func TestCreateImageTool_OpenAIEdits_JSON_MultipleImages(t *testing.T) {
	pngBytes1 := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52}
	pngBytes2 := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x53}
	mockResponse := `{
		"data": [
			{
				"b64_json": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
			}
		]
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/edits" {
			t.Errorf("expected path /images/edits, got %s", r.URL.Path)
		}
		ct := r.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "application/json") {
			t.Errorf("expected application/json content type, got %s", ct)
		}
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read body: %v", err)
		}
		var reqBody struct {
			Prompt string `json:"prompt"`
			Images []struct {
				ImageURL string `json:"image_url"`
			} `json:"images"`
		}
		if err := json.Unmarshal(bodyBytes, &reqBody); err != nil {
			t.Fatalf("failed to unmarshal body: %v", err)
		}
		if reqBody.Prompt != "two captains" {
			t.Errorf("expected prompt 'two captains', got %s", reqBody.Prompt)
		}
		if len(reqBody.Images) != 2 {
			t.Fatalf("expected 2 images, got %d", len(reqBody.Images))
		}
		expectedURL1 := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes1)
		expectedURL2 := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes2)
		if reqBody.Images[0].ImageURL != expectedURL1 || reqBody.Images[1].ImageURL != expectedURL2 {
			t.Error("images mismatch")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResponse))
	}))
	defer server.Close()

	creds := fakeCreds{key: "fake-key", base: server.URL}
	reg := providers.NewRegistry(nil)
	tool := NewCreateImageTool(reg)
	ctx := WithToolWorkspace(context.Background(), t.TempDir())

	refImg1 := &referenceImage{Data: pngBytes1, MimeType: "image/png"}
	refImg2 := &referenceImage{Data: pngBytes2, MimeType: "image/png"}

	params := map[string]any{
		"prompt":         "two captains",
		"ref_images":     []*referenceImage{refImg1, refImg2},
		"_provider_type": "openai",
	}

	imageBytes, _, err := tool.callProvider(ctx, creds, "openai", "gpt-image-2", params)
	if err != nil {
		t.Fatalf("callProvider returned error: %v", err)
	}
	if len(imageBytes) == 0 {
		t.Error("returned imageBytes is empty")
	}
}

func TestCreateImageTool_OpenAIEdits_JSON_WithDescription(t *testing.T) {
	pngBytes := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52}
	mockResponse := `{
		"data": [
			{
				"b64_json": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
			}
		]
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read body: %v", err)
		}
		var reqBody struct {
			Prompt string `json:"prompt"`
		}
		if err := json.Unmarshal(bodyBytes, &reqBody); err != nil {
			t.Fatalf("failed to unmarshal body: %v", err)
		}
		expectedPrompt := "two captains\n\n[Reference Image Roles]\n- image_1.png: Lâm\n- image_2.png: Quân"
		if reqBody.Prompt != expectedPrompt {
			t.Errorf("expected prompt %q, got %q", expectedPrompt, reqBody.Prompt)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResponse))
	}))
	defer server.Close()

	creds := fakeCreds{key: "fake-key", base: server.URL}
	reg := providers.NewRegistry(nil)
	tool := NewCreateImageTool(reg)
	ctx := WithToolWorkspace(context.Background(), t.TempDir())

	refImg1 := &referenceImage{Data: pngBytes, MimeType: "image/png", Description: "Lâm"}
	refImg2 := &referenceImage{Data: pngBytes, MimeType: "image/png", Description: "Quân"}

	params := map[string]any{
		"prompt":         "two captains",
		"ref_images":     []*referenceImage{refImg1, refImg2},
		"_provider_type": "openai",
	}

	_, _, err := tool.callProvider(ctx, creds, "openai", "gpt-image-2", params)
	if err != nil {
		t.Fatalf("callProvider returned error: %v", err)
	}
}
