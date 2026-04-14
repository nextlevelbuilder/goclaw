package providers

import (
	"encoding/json"
	"io"
	"testing"
)

// TestBuildStreamJSONInput_MimeRouting verifies that buildStreamJSONInput
// picks the correct Anthropic content block type based on MIME:
//   - application/pdf → "document"
//   - image/*         → "image"
//
// Regression guard: earlier versions hardcoded "image" for every block,
// causing PDF passthrough to fail because the Anthropic API rejects
// image blocks with non-image MIME types.
func TestBuildStreamJSONInput_MimeRouting(t *testing.T) {
	cases := []struct {
		name      string
		images    []ImageContent
		wantTypes []string
	}{
		{
			name: "png image → image block",
			images: []ImageContent{
				{MimeType: "image/png", Data: "abc"},
			},
			wantTypes: []string{"image"},
		},
		{
			name: "pdf → document block",
			images: []ImageContent{
				{MimeType: "application/pdf", Data: "abc"},
			},
			wantTypes: []string{"document"},
		},
		{
			name: "mixed png + pdf → image then document",
			images: []ImageContent{
				{MimeType: "image/jpeg", Data: "xxx"},
				{MimeType: "application/pdf", Data: "yyy"},
			},
			wantTypes: []string{"image", "document"},
		},
		{
			name: "unknown MIME falls back to image",
			images: []ImageContent{
				{MimeType: "application/octet-stream", Data: "zzz"},
			},
			wantTypes: []string{"image"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := buildStreamJSONInput("describe", tc.images)
			raw, err := io.ReadAll(r)
			if err != nil {
				t.Fatalf("read stdin: %v", err)
			}

			var msg struct {
				Message struct {
					Content []map[string]any `json:"content"`
				} `json:"message"`
			}
			if err := json.Unmarshal(raw, &msg); err != nil {
				t.Fatalf("parse stream-json: %v\nraw: %s", err, raw)
			}

			// Expect N media blocks + 1 trailing text block.
			wantLen := len(tc.wantTypes) + 1
			if got := len(msg.Message.Content); got != wantLen {
				t.Fatalf("content blocks = %d, want %d\nraw: %s", got, wantLen, raw)
			}

			for i, wantType := range tc.wantTypes {
				gotType, _ := msg.Message.Content[i]["type"].(string)
				if gotType != wantType {
					t.Errorf("block[%d].type = %q, want %q", i, gotType, wantType)
				}
				source, _ := msg.Message.Content[i]["source"].(map[string]any)
				if source == nil {
					t.Errorf("block[%d].source is nil", i)
					continue
				}
				if gotMime, _ := source["media_type"].(string); gotMime != tc.images[i].MimeType {
					t.Errorf("block[%d].source.media_type = %q, want %q", i, gotMime, tc.images[i].MimeType)
				}
			}

			// Trailing text block.
			last := msg.Message.Content[wantLen-1]
			if last["type"] != "text" {
				t.Errorf("trailing block type = %v, want text", last["type"])
			}
			if last["text"] != "describe" {
				t.Errorf("trailing block text = %v, want describe", last["text"])
			}
		})
	}
}

// TestBuildStreamJSONInput_NoText covers the edge case where the caller
// passes images with an empty prompt — no text block should be emitted.
func TestBuildStreamJSONInput_NoText(t *testing.T) {
	r := buildStreamJSONInput("", []ImageContent{
		{MimeType: "image/png", Data: "abc"},
	})
	raw, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdin: %v", err)
	}
	var msg struct {
		Message struct {
			Content []map[string]any `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("parse stream-json: %v", err)
	}
	if len(msg.Message.Content) != 1 {
		t.Errorf("content blocks = %d, want 1 (image only)", len(msg.Message.Content))
	}
}
