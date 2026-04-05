package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMinimaxImageAspectRatio(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]any
		want   string
	}{
		{
			name:   "explicit ratio",
			params: map[string]any{"aspect_ratio": "16:9"},
			want:   "16:9",
		},
		{
			name:   "default empty",
			params: map[string]any{},
			want:   "1:1",
		},
		{
			name:   "unknown ratio falls back to 1:1",
			params: map[string]any{"aspect_ratio": "21:9"},
			want:   "1:1",
		},
		{
			name:   "legacy size wins over aspect_ratio",
			params: map[string]any{"size": "1280*720", "aspect_ratio": "1:1"},
			want:   "16:9",
		},
		{
			name:   "legacy size 1024*1024",
			params: map[string]any{"size": "1024*1024"},
			want:   "1:1",
		},
		{
			name:   "unknown size ignored uses aspect_ratio",
			params: map[string]any{"size": "custom", "aspect_ratio": "9:16"},
			want:   "9:16",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := minimaxImageAspectRatio(tt.params); got != tt.want {
				t.Fatalf("minimaxImageAspectRatio() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCallMinimaxImageGen_ImageBase64Response(t *testing.T) {
	wantPNG := []byte{0x89, 0x50, 0x4e, 0x47}
	b64 := base64.StdEncoding.EncodeToString(wantPNG)

	var gotAuth string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/image_generation" {
			t.Errorf("path = %q, want /v1/image_generation", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"image_base64": []string{b64}},
		})
	}))
	defer srv.Close()

	out, usage, err := callMinimaxImageGen(context.Background(), "test-key", srv.URL+"/v1", "image-01", "a prompt",
		map[string]any{"aspect_ratio": "16:9"})
	if err != nil {
		t.Fatalf("callMinimaxImageGen: %v", err)
	}
	if usage != nil {
		t.Fatalf("usage = %#v, want nil", usage)
	}
	if string(out) != string(wantPNG) {
		t.Fatalf("decoded bytes mismatch")
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if body["model"] != "image-01" || body["prompt"] != "a prompt" ||
		body["aspect_ratio"] != "16:9" || body["response_format"] != "base64" {
		t.Fatalf("request fields = %#v", body)
	}
}

func TestCallMinimaxImageGen_LegacyImageListFallback(t *testing.T) {
	want := []byte("x")
	b64 := base64.StdEncoding.EncodeToString(want)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"image_list": []map[string]any{{"base64_image": b64}},
			},
		})
	}))
	defer srv.Close()

	out, _, err := callMinimaxImageGen(context.Background(), "k", srv.URL+"/v1", "m", "p", nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(want) {
		t.Fatalf("got %q, want %q", out, want)
	}
}

func TestCallMinimaxImageGen_BaseRespError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"base_resp": map[string]any{"status_code": 1004, "status_msg": "bad"},
			"data":      map[string]any{"image_base64": []string{}},
		})
	}))
	defer srv.Close()

	_, _, err := callMinimaxImageGen(context.Background(), "k", srv.URL+"/v1", "m", "p", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}
