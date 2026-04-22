package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// minimalPNGForProviders is a 1x1 transparent PNG in base64 used by native image tests.
const minimalPNGForProviders = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

// TestCodexGenerateImage_BuildsNativeRequest verifies that GenerateImage sends the
// correct JSON body to the Responses API: model, stream:false, input, tools, and
// tool_choice. The test captures the raw request body from a mock server and
// asserts each required field is present and well-formed.
func TestCodexGenerateImage_BuildsNativeRequest(t *testing.T) {
	var captured []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture request body for inspection.
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			http.Error(w, "read error", http.StatusInternalServerError)
			return
		}
		captured = body

		// Return a minimal non-streaming JSON response with an image result.
		imgB64 := minimalPNGForProviders
		resp := map[string]any{
			"id":     "resp_test",
			"status": "completed",
			"output": []map[string]any{
				{
					"type":          "image_generation_call",
					"result":        imgB64,
					"output_format": "png",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	p := NewCodexProvider("codex-test", &staticTokenSource{token: "tok"}, server.URL, "gpt-image-2")
	p.retryConfig.Attempts = 1

	req := NativeImageRequest{
		Model:        "gpt-image-2",
		Prompt:       "A red circle on a white background",
		AspectRatio:  "16:9",
		OutputFormat: "png",
	}
	result, err := p.GenerateImage(context.Background(), req)
	if err != nil {
		t.Fatalf("GenerateImage returned error: %v", err)
	}
	if len(result.Data) == 0 {
		t.Fatal("GenerateImage returned empty Data")
	}

	// Verify outbound request body shape.
	var body map[string]any
	if err := json.Unmarshal(captured, &body); err != nil {
		t.Fatalf("unmarshal captured body: %v", err)
	}

	// model field
	if model, _ := body["model"].(string); model != "gpt-image-2" {
		t.Errorf("body[model] = %q, want %q", model, "gpt-image-2")
	}

	// stream must be false (non-streaming for create_image)
	if stream, _ := body["stream"].(bool); stream {
		t.Error("body[stream] should be false")
	}

	// input must be an array with one user message
	inputs, ok := body["input"].([]any)
	if !ok || len(inputs) != 1 {
		t.Fatalf("body[input]: expected []any length 1, got %T len %d", body["input"], len(inputs))
	}
	userMsg, ok := inputs[0].(map[string]any)
	if !ok {
		t.Fatalf("input[0] is not a map: %T", inputs[0])
	}
	if role, _ := userMsg["role"].(string); role != "user" {
		t.Errorf("input[0].role = %q, want %q", role, "user")
	}
	contents, ok := userMsg["content"].([]any)
	if !ok || len(contents) != 1 {
		t.Fatalf("input[0].content: expected []any length 1, got %T len %d", userMsg["content"], len(contents))
	}
	contentPart, ok := contents[0].(map[string]any)
	if !ok {
		t.Fatalf("content[0] is not a map: %T", contents[0])
	}
	if typ, _ := contentPart["type"].(string); typ != "input_text" {
		t.Errorf("content[0].type = %q, want %q", typ, "input_text")
	}
	if text, _ := contentPart["text"].(string); text != req.Prompt {
		t.Errorf("content[0].text = %q, want %q", text, req.Prompt)
	}

	// tools must contain one image_generation entry
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("body[tools]: expected []any length 1, got %T len %d", body["tools"], len(tools))
	}
	tool, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("tools[0] is not a map: %T", tools[0])
	}
	if typ, _ := tool["type"].(string); typ != "image_generation" {
		t.Errorf("tools[0].type = %q, want %q", typ, "image_generation")
	}
	// size should map to 1792x1024 for 16:9
	wantSize := SizeFromAspect("16:9")
	if size, _ := tool["size"].(string); size != wantSize {
		t.Errorf("tools[0].size = %q, want %q", size, wantSize)
	}
	if fmt.Sprint(tool["output_format"]) != "png" {
		t.Errorf("tools[0].output_format = %v, want png", tool["output_format"])
	}

	// tool_choice must force image_generation
	toolChoice, ok := body["tool_choice"].(map[string]any)
	if !ok {
		t.Fatalf("body[tool_choice] is not a map: %T", body["tool_choice"])
	}
	if typ, _ := toolChoice["type"].(string); typ != "image_generation" {
		t.Errorf("tool_choice.type = %q, want %q", typ, "image_generation")
	}
}

// TestCodexGenerateImage_SSEFallback verifies that GenerateImage correctly parses
// an SSE-format response when the server returns streamed lines instead of a JSON blob.
func TestCodexGenerateImage_SSEFallback(t *testing.T) {
	imgB64 := minimalPNGForProviders

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Emit a response.completed SSE event with an image_generation_call.
		ev := codexSSEEvent{
			Type: "response.completed",
			Response: &codexAPIResponse{
				ID:     "resp_sse",
				Status: "completed",
				Output: []codexItem{
					{
						ID:           "ig_1",
						Type:         "image_generation_call",
						OutputFormat: "png",
						Result:       imgB64,
					},
				},
				Usage: &codexUsage{InputTokens: 5, OutputTokens: 5, TotalTokens: 10},
			},
		}
		b, _ := json.Marshal(ev)
		fmt.Fprintf(w, "data: %s\n\n", b)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	p := NewCodexProvider("codex-test", &staticTokenSource{token: "tok"}, server.URL, "gpt-image-2")
	p.retryConfig.Attempts = 1

	result, err := p.GenerateImage(context.Background(), NativeImageRequest{
		Prompt:       "A blue square",
		OutputFormat: "png",
	})
	if err != nil {
		t.Fatalf("GenerateImage SSE fallback: %v", err)
	}
	if result.MimeType != "image/png" {
		t.Errorf("MimeType = %q, want image/png", result.MimeType)
	}
	want, _ := base64.StdEncoding.DecodeString(imgB64)
	if len(result.Data) != len(want) {
		t.Errorf("Data length = %d, want %d", len(result.Data), len(want))
	}
	if result.Usage == nil {
		t.Error("Usage is nil")
	} else if result.Usage.TotalTokens != 10 {
		t.Errorf("Usage.TotalTokens = %d, want 10", result.Usage.TotalTokens)
	}
}

// TestCodexGenerateImage_NoPrompt verifies that an empty prompt returns an error
// before making any HTTP request.
func TestCodexGenerateImage_NoPrompt(t *testing.T) {
	p := NewCodexProvider("codex-test", &staticTokenSource{token: "tok"}, "http://localhost", "gpt-image-2")
	p.retryConfig.Attempts = 1

	_, err := p.GenerateImage(context.Background(), NativeImageRequest{Prompt: ""})
	if err == nil {
		t.Fatal("expected error for empty prompt, got nil")
	}
}

// TestSizeFromAspect verifies the aspect ratio → pixel dimension mapping.
func TestSizeFromAspect(t *testing.T) {
	cases := []struct {
		ratio string
		want  string
	}{
		{"1:1", "1024x1024"},
		{"16:9", "1792x1024"},
		{"9:16", "1024x1792"},
		{"4:3", "1365x1024"},
		{"3:4", "1024x1365"},
		{"", "1024x1024"},
		{"custom", "1024x1024"},
	}
	for _, tc := range cases {
		got := SizeFromAspect(tc.ratio)
		if got != tc.want {
			t.Errorf("SizeFromAspect(%q) = %q, want %q", tc.ratio, got, tc.want)
		}
	}
}
