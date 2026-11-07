package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// aspectTestPNG returns an encoded PNG of the given size.
func aspectTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// captureJSONServer records every request body it receives and answers with respond.
func captureJSONServer(t *testing.T, respond map[string]any) (*httptest.Server, <-chan map[string]any) {
	t.Helper()
	bodies := make(chan map[string]any, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		bodies <- body
		_ = json.NewEncoder(w).Encode(respond)
	}))
	t.Cleanup(srv.Close)
	return srv, bodies
}

func TestNearestAspectRatio(t *testing.T) {
	minimaxLike := []string{"1:1", "16:9", "4:3", "3:2", "2:3", "3:4", "9:16", "21:9"}
	cases := []struct {
		requested string
		supported []string
		want      string
	}{
		{"16:9", minimaxLike, "16:9"}, // already supported, returned as-is
		{"4:5", minimaxLike, "3:4"},   // closest portrait ratio
		{"5:4", minimaxLike, "4:3"},   // closest landscape ratio
		{"2:1", minimaxLike, "16:9"},  // 16:9 is nearer than 21:9 on the log scale
		{"1:2", minimaxLike, "9:16"},
		{"custom", minimaxLike, "1:1"}, // unparsable
		{"", minimaxLike, "1:1"},
		{"4:5", []string{"1:1", "16:9", "9:16"}, "1:1"}, // dall-e-3 set: 1:1 beats 9:16
	}
	for _, tc := range cases {
		if got := nearestAspectRatio(tc.requested, tc.supported); got != tc.want {
			t.Errorf("nearestAspectRatio(%q, %v) = %q, want %q", tc.requested, tc.supported, got, tc.want)
		}
	}
}

func TestGptImageSize(t *testing.T) {
	cases := []struct{ ratio, want string }{
		{"1:1", "1024x1024"}, // the three documented sizes come out unchanged
		{"3:2", "1536x1024"},
		{"2:3", "1024x1536"},
		{"4:5", "1024x1280"}, // exact
		{"5:4", "1280x1024"},
		{"2:1", "2048x1024"},
		{"1:2", "1024x2048"},
		{"4:3", "1360x1024"},  // 1365 rounded to a multiple of 16
		{"16:9", "1824x1024"}, // 1820 rounded to a multiple of 16
		{"21:9", "2384x1024"},
		{"custom", "1024x1024"},
	}
	for _, tc := range cases {
		if got := gptImageSize(tc.ratio); got != tc.want {
			t.Errorf("gptImageSize(%q) = %q, want %q", tc.ratio, got, tc.want)
		}
	}
}

func TestOpenAIImageSize(t *testing.T) {
	cases := []struct {
		model, ratio, want string
	}{
		{"gpt-image-1.5", "1:1", "1024x1024"},
		{"gpt-image-1.5", "4:5", "1024x1280"},
		{"gpt-image-2", "3:2", "1536x1024"},
		{"gpt-image-2.5-flare", "1:2", "1024x2048"},
		{"gpt-image-1-mini", "16:9", "1824x1024"},
		{"chatgpt-image-latest", "9:16", "1024x1824"},
		{"dall-e-3", "16:9", "1792x1024"},
		{"dall-e-3", "4:5", "1024x1024"}, // no custom sizes: closest documented one
		{"dall-e-2", "16:9", ""},         // no documented non-square size
		{"flux-1-schnell", "16:9", ""},   // unknown model keeps the provider default
	}
	for _, tc := range cases {
		if got := openAIImageSize(tc.model, tc.ratio); got != tc.want {
			t.Errorf("openAIImageSize(%q, %q) = %q, want %q", tc.model, tc.ratio, got, tc.want)
		}
	}
}

func TestAspectRatioToDashScopeSize(t *testing.T) {
	cases := []struct {
		params map[string]any
		want   string
	}{
		{map[string]any{"aspect_ratio": "4:5"}, "1152*1440"},
		{map[string]any{"aspect_ratio": "2:3"}, "768*1152"},
		{map[string]any{"aspect_ratio": "21:9"}, "1344*576"},
		{map[string]any{"aspect_ratio": "2:1"}, "1440*720"},
		{map[string]any{"aspect_ratio": "1:2"}, "720*1440"},
		{map[string]any{"aspect_ratio": "16:9"}, "1280*720"},
		{map[string]any{}, "1024*1024"},
		{map[string]any{"size": "2048*2048", "aspect_ratio": "4:5"}, "2048*2048"}, // explicit size wins
	}
	for _, tc := range cases {
		if got := aspectRatioToDashScopeSize(tc.params); got != tc.want {
			t.Errorf("aspectRatioToDashScopeSize(%v) = %q, want %q", tc.params, got, tc.want)
		}
	}
}

func TestAspectRatioToBytePlusSize(t *testing.T) {
	cases := []struct {
		params map[string]any
		want   string
	}{
		{map[string]any{"aspect_ratio": "4:5"}, "1152x1440"},
		{map[string]any{"aspect_ratio": "5:4"}, "1440x1152"},
		{map[string]any{"aspect_ratio": "3:2"}, "1152x768"},
		{map[string]any{"aspect_ratio": "2:1"}, "1440x720"},
		{map[string]any{"aspect_ratio": "1:2"}, "720x1440"},
		{map[string]any{"aspect_ratio": "9:16"}, "720x1280"},
		{map[string]any{}, "1024x1024"},
		{map[string]any{"size": "2K", "aspect_ratio": "4:5"}, "2K"}, // explicit size wins
	}
	for _, tc := range cases {
		if got := aspectRatioToBytePlusSize(tc.params); got != tc.want {
			t.Errorf("aspectRatioToBytePlusSize(%v) = %q, want %q", tc.params, got, tc.want)
		}
	}
}

func TestImageDimensions(t *testing.T) {
	width, height, ok := imageDimensions(aspectTestPNG(t, 4, 5))
	if !ok || width != 4 || height != 5 {
		t.Fatalf("imageDimensions(png) = %d, %d, %v; want 4, 5, true", width, height, ok)
	}
	if _, _, ok := imageDimensions([]byte("not an image")); ok {
		t.Fatal("imageDimensions should fail on non-image bytes")
	}
}

func TestCreateImageExecute_RejectsUnsupportedValues(t *testing.T) {
	tool := NewCreateImageTool(nil)

	res := tool.Execute(context.Background(), map[string]any{"prompt": "a cat", "aspect_ratio": "3:1"})
	if res == nil || !res.IsError {
		t.Fatalf("expected an error result for an unsupported ratio, got %+v", res)
	}
	if !strings.Contains(res.ForLLM, "unsupported aspect_ratio") || !strings.Contains(res.ForLLM, "4:5") {
		t.Fatalf("error should name the problem and list supported values, got %q", res.ForLLM)
	}

	res = tool.Execute(context.Background(), map[string]any{"prompt": "a cat", "image_size": "8K"})
	if res == nil || !res.IsError || !strings.Contains(res.ForLLM, "unsupported image_size") {
		t.Fatalf("expected an error result for an unsupported image_size, got %+v", res)
	}
}

func TestGeminiNativeImageGen_SendsImageConfig(t *testing.T) {
	srv, bodies := captureJSONServer(t, map[string]any{
		"candidates": []map[string]any{{
			"content": map[string]any{"parts": []map[string]any{{
				"inlineData": map[string]any{
					"mimeType": "image/png",
					"data":     base64.StdEncoding.EncodeToString(aspectTestPNG(t, 4, 5)),
				},
			}}},
		}},
	})
	tool := &CreateImageTool{}
	call := func(params map[string]any) map[string]any {
		t.Helper()
		if _, _, err := tool.callGeminiNativeImageGen(context.Background(), "key", srv.URL, "gemini-3-pro-image", "a cat", params); err != nil {
			t.Fatalf("callGeminiNativeImageGen(%v): %v", params, err)
		}
		generationConfig, _ := (<-bodies)["generationConfig"].(map[string]any)
		return generationConfig
	}

	// The ratio goes out exactly as requested, even one outside the documented enum: the API decides.
	imageConfig, _ := call(map[string]any{"aspect_ratio": "2:1"})["imageConfig"].(map[string]any)
	if imageConfig["aspectRatio"] != "2:1" {
		t.Fatalf("imageConfig.aspectRatio = %v, want 2:1", imageConfig["aspectRatio"])
	}
	if _, has := imageConfig["imageSize"]; has {
		t.Fatalf("imageSize must stay absent when not requested, got %v", imageConfig)
	}

	// image_size rides along as imageSize; 1:1 stays implicit even then.
	imageConfig, _ = call(map[string]any{"aspect_ratio": "1:1", "image_size": "2K"})["imageConfig"].(map[string]any)
	if imageConfig["imageSize"] != "2K" {
		t.Fatalf("imageConfig.imageSize = %v, want 2K", imageConfig["imageSize"])
	}
	if _, has := imageConfig["aspectRatio"]; has {
		t.Fatalf("1:1 must not send aspectRatio, got %v", imageConfig)
	}

	// Everything at its default: no imageConfig at all, so older image models keep working.
	if generationConfig := call(map[string]any{"aspect_ratio": "1:1"}); generationConfig["imageConfig"] != nil {
		t.Fatalf("defaults must not send imageConfig, got %v", generationConfig)
	}
}

func TestOpenRouterImageGen_SendsImageConfig(t *testing.T) {
	srv, bodies := captureJSONServer(t, map[string]any{
		"choices": []map[string]any{{"message": map[string]any{
			"content": "",
			"images": []map[string]any{{"image_url": map[string]any{
				"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(aspectTestPNG(t, 4, 5)),
			}}},
		}}},
	})
	tool := &CreateImageTool{}

	if _, _, err := tool.callImageGenAPI(context.Background(), "key", srv.URL, "google/gemini-3-pro-image-preview", "a cat", "4:5", map[string]any{"aspect_ratio": "4:5", "image_size": "2K"}); err != nil {
		t.Fatalf("callImageGenAPI: %v", err)
	}
	imageConfig, _ := (<-bodies)["image_config"].(map[string]any)
	if imageConfig["aspect_ratio"] != "4:5" || imageConfig["image_size"] != "2K" {
		t.Fatalf("image_config = %v, want aspect_ratio 4:5 and image_size 2K", imageConfig)
	}

	if _, _, err := tool.callImageGenAPI(context.Background(), "key", srv.URL, "google/gemini-2.5-flash-image", "a cat", "1:1", map[string]any{"aspect_ratio": "1:1"}); err != nil {
		t.Fatalf("callImageGenAPI(1:1): %v", err)
	}
	if body := <-bodies; body["image_config"] != nil {
		t.Fatalf("defaults must not send image_config, got %v", body["image_config"])
	}
}

func TestStandardImageGen_SendsOpenAISizeForKnownModels(t *testing.T) {
	srv, bodies := captureJSONServer(t, map[string]any{
		"data": []map[string]any{{"b64_json": base64.StdEncoding.EncodeToString(aspectTestPNG(t, 4, 5))}},
	})
	tool := &CreateImageTool{}

	// gpt-image gets the exact ratio as a custom size.
	if _, _, err := tool.callStandardImageGenAPI(context.Background(), "key", srv.URL, "gpt-image-1.5", "a cat", map[string]any{"aspect_ratio": "4:5"}); err != nil {
		t.Fatalf("callStandardImageGenAPI(gpt-image-1.5): %v", err)
	}
	if body := <-bodies; body["size"] != "1024x1280" {
		t.Fatalf("size = %v, want 1024x1280", body["size"])
	}

	// Unknown models keep the provider default: no size field at all.
	if _, _, err := tool.callStandardImageGenAPI(context.Background(), "key", srv.URL, "flux-1-schnell", "a cat", map[string]any{"aspect_ratio": "4:5"}); err != nil {
		t.Fatalf("callStandardImageGenAPI(flux): %v", err)
	}
	if body := <-bodies; body["size"] != nil {
		t.Fatalf("unknown model must not send size, got %v", body["size"])
	}
}

func TestOpenAIImageEditJSON_SendsSize(t *testing.T) {
	srv, bodies := captureJSONServer(t, map[string]any{
		"data": []map[string]any{{"b64_json": base64.StdEncoding.EncodeToString(aspectTestPNG(t, 4, 5))}},
	})
	tool := &CreateImageTool{}
	refs := []*referenceImage{{Data: aspectTestPNG(t, 2, 2), MimeType: "image/png"}}

	if _, _, err := tool.callOpenAIImageEditJSON(context.Background(), "key", srv.URL, "gpt-image-1.5", "a cat", refs, "1024x1280"); err != nil {
		t.Fatalf("callOpenAIImageEditJSON: %v", err)
	}
	if body := <-bodies; body["size"] != "1024x1280" {
		t.Fatalf("size = %v, want 1024x1280", body["size"])
	}

	if _, _, err := tool.callOpenAIImageEditJSON(context.Background(), "key", srv.URL, "gpt-image-1.5", "a cat", refs, ""); err != nil {
		t.Fatalf("callOpenAIImageEditJSON(no size): %v", err)
	}
	if body := <-bodies; body["size"] != nil {
		t.Fatalf("empty size must not be sent, got %v", body["size"])
	}
}
