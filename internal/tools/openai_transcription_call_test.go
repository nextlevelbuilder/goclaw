package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsTranscriptionModel(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"whisper-1", true},
		{"gpt-4o-transcribe", true},
		{"gpt-4o-mini-transcribe", true},
		{"gpt-4o-transcribe-diarize", true},
		{"gpt-4o-audio-preview", false},
		{"gpt-4o", false},
		{"gpt-4o-mini", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			if got := isTranscriptionModel(tt.model); got != tt.want {
				t.Errorf("isTranscriptionModel(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestExtFromMime(t *testing.T) {
	tests := []struct {
		mime string
		want string
	}{
		{"audio/wav", ".wav"},
		{"audio/mpeg", ".mp3"},
		{"audio/mp4", ".m4a"},
		{"audio/ogg", ".ogg"},
		{"audio/opus", ".ogg"},
		{"audio/flac", ".flac"},
		{"audio/webm", ".webm"},
		{"application/octet-stream", ".mp3"},
	}
	for _, tt := range tests {
		t.Run(tt.mime, func(t *testing.T) {
			if got := extFromMime(tt.mime); got != tt.want {
				t.Errorf("extFromMime(%q) = %v, want %v", tt.mime, got, tt.want)
			}
		})
	}
}

func TestOpenAITranscriptionCall_Success(t *testing.T) {
	wantText := "Hello, this is a test transcription."
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify method and path.
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/audio/transcriptions") {
			t.Errorf("expected path ending in /audio/transcriptions, got %s", r.URL.Path)
		}

		// Verify multipart form.
		if err := r.ParseMultipartForm(50 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if r.FormValue("model") != "gpt-4o-transcribe" {
			t.Errorf("model = %q, want gpt-4o-transcribe", r.FormValue("model"))
		}
		if r.FormValue("response_format") != "json" {
			t.Errorf("response_format = %q, want json", r.FormValue("response_format"))
		}
		if r.FormValue("prompt") != "Transcribe this audio" {
			t.Errorf("prompt = %q, want 'Transcribe this audio'", r.FormValue("prompt"))
		}

		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("form file: %v", err)
		}
		data, _ := io.ReadAll(file)
		if len(data) == 0 {
			t.Error("file data is empty")
		}

		resp, _ := json.Marshal(map[string]string{"text": wantText})
		w.Header().Set("Content-Type", "application/json")
		w.Write(resp)
	}))
	defer server.Close()

	resp, err := openaiTranscriptionCall(context.Background(), "test-key", server.URL, "gpt-4o-transcribe", "Transcribe this audio", []byte("fake-audio-data"), "audio/mpeg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != wantText {
		t.Errorf("content = %q, want %q", resp.Content, wantText)
	}
	if resp.Usage != nil {
		t.Errorf("expected nil usage for response without usage field, got %+v", resp.Usage)
	}
}

func TestOpenAITranscriptionCall_WithUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, _ := json.Marshal(map[string]any{
			"text": "transcribed text",
			"usage": map[string]int{
				"prompt_tokens":     10,
				"completion_tokens": 20,
				"total_tokens":      30,
			},
		})
		w.Header().Set("Content-Type", "application/json")
		w.Write(resp)
	}))
	defer server.Close()

	resp, err := openaiTranscriptionCall(context.Background(), "test-key", server.URL, "gpt-4o-transcribe", "", []byte("audio"), "audio/wav")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("prompt_tokens = %d, want 10", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 20 {
		t.Errorf("completion_tokens = %d, want 20", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 30 {
		t.Errorf("total_tokens = %d, want 30", resp.Usage.TotalTokens)
	}
}

func TestOpenAITranscriptionCall_EmptyPrompt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(50 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		// When prompt is empty, the field should be omitted.
		if v := r.FormValue("prompt"); v != "" {
			t.Errorf("prompt field should be empty, got %q", v)
		}
		resp, _ := json.Marshal(map[string]string{"text": "some text"})
		w.Write(resp)
	}))
	defer server.Close()

	_, err := openaiTranscriptionCall(context.Background(), "test-key", server.URL, "whisper-1", "", []byte("audio"), "audio/mpeg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenAITranscriptionCall_EmptyTextError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, _ := json.Marshal(map[string]string{"text": ""})
		w.Write(resp)
	}))
	defer server.Close()

	_, err := openaiTranscriptionCall(context.Background(), "test-key", server.URL, "gpt-4o-transcribe", "prompt", []byte("audio"), "audio/mpeg")
	if err == nil {
		t.Fatal("expected error for empty transcription response")
	}
	if !strings.Contains(err.Error(), "empty transcription") {
		t.Errorf("error = %q, want 'empty transcription' message", err.Error())
	}
}

func TestOpenAITranscriptionCall_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"invalid model"}}`))
	}))
	defer server.Close()

	_, err := openaiTranscriptionCall(context.Background(), "test-key", server.URL, "bad-model", "", []byte("audio"), "audio/mpeg")
	if err == nil {
		t.Fatal("expected error for HTTP 400")
	}
	if !strings.Contains(err.Error(), "HTTP 400") {
		t.Errorf("error = %q, want HTTP 400", err.Error())
	}
}
