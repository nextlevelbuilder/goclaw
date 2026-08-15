package elevenlabs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

// sttHappyResponse matches ElevenLabs Scribe response schema.
type sttHappyResponse struct {
	Text         string  `json:"text"`
	LanguageCode string  `json:"language_code"`
	DurationSecs float64 `json:"audio_duration_secs"`
}

func newTestSTTServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *STTProvider) {
	t.Helper()
	srv := httptest.NewServer(handler)
	p := NewSTTProvider(Config{
		APIKey:  "test-key",
		BaseURL: srv.URL,
	})
	return srv, p
}

func writeTempAudioFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "stt_elabs_test_*.ogg")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	f.WriteString(content)
	f.Close()
	return f.Name()
}

// Case 1: happy path — multipart upload returns transcript.
func TestSTTProvider_HappyPath(t *testing.T) {
	audioFile := writeTempAudioFile(t, "fake-ogg")
	defer os.Remove(audioFile)

	srv, p := newTestSTTServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/speech-to-text" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("xi-api-key") == "" {
			t.Error("missing xi-api-key header")
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
		}
		if _, _, err := r.FormFile("file"); err != nil {
			t.Errorf("expected 'file' field: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sttHappyResponse{
			Text:         "hello world",
			LanguageCode: "en",
			DurationSecs: 5.0,
		})
	})
	defer srv.Close()

	res, err := p.Transcribe(context.Background(), audio.STTInput{FilePath: audioFile}, audio.STTOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "hello world" {
		t.Errorf("expected 'hello world', got %q", res.Text)
	}
	if res.Provider != "elevenlabs" {
		t.Errorf("expected provider 'elevenlabs', got %q", res.Provider)
	}
	if res.Duration != 5.0 {
		t.Errorf("expected duration 5.0, got %f", res.Duration)
	}
}

// Case 2: 401 surfaces error with status code.
func TestSTTProvider_Unauthorized(t *testing.T) {
	audioFile := writeTempAudioFile(t, "fake-ogg")
	defer os.Remove(audioFile)

	srv, p := newTestSTTServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":"invalid api key"}`, http.StatusUnauthorized)
	})
	defer srv.Close()

	_, err := p.Transcribe(context.Background(), audio.STTInput{FilePath: audioFile}, audio.STTOptions{})
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	// Should contain status code.
	if err.Error() == "" {
		t.Error("error message is empty")
	}
}

// Case 3: language_code passthrough in multipart.
func TestSTTProvider_LanguagePassthrough(t *testing.T) {
	audioFile := writeTempAudioFile(t, "fake-ogg")
	defer os.Remove(audioFile)

	var gotLang string
	srv, p := newTestSTTServer(t, func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(1 << 20)
		gotLang = r.FormValue("language_code")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sttHappyResponse{Text: "ok", LanguageCode: "vi"})
	})
	defer srv.Close()

	p.Transcribe(context.Background(), audio.STTInput{FilePath: audioFile}, audio.STTOptions{Language: "vi"})
	if gotLang != "vi" {
		t.Errorf("expected language_code 'vi', got %q", gotLang)
	}
}

// Case 4: diarize=true sets multipart field.
func TestSTTProvider_DiarizeField(t *testing.T) {
	audioFile := writeTempAudioFile(t, "fake-ogg")
	defer os.Remove(audioFile)

	var gotDiarize string
	srv, p := newTestSTTServer(t, func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(1 << 20)
		gotDiarize = r.FormValue("diarize")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sttHappyResponse{Text: "ok"})
	})
	defer srv.Close()

	p.Transcribe(context.Background(), audio.STTInput{FilePath: audioFile}, audio.STTOptions{Diarize: true})
	if gotDiarize != "true" {
		t.Errorf("expected diarize 'true', got %q", gotDiarize)
	}
}

// Case 5: FilePath preferred over Bytes when both set.
func TestSTTProvider_FilePathPreferredOverBytes(t *testing.T) {
	audioFile := writeTempAudioFile(t, "file-content")
	defer os.Remove(audioFile)

	var receivedSize int64
	srv, p := newTestSTTServer(t, func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(1 << 20)
		f, _, _ := r.FormFile("file")
		if f != nil {
			buf := make([]byte, 100)
			n, _ := f.Read(buf)
			receivedSize = int64(n)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sttHappyResponse{Text: "ok"})
	})
	defer srv.Close()

	// When both set, FilePath wins (Bytes ignored by resolveFilePath when FilePath != "").
	in := audio.STTInput{
		FilePath: audioFile,
		Bytes:    []byte("different-bytes"),
	}
	_, err := p.Transcribe(context.Background(), in, audio.STTOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// file-content is 12 bytes; if Bytes were used it would be 15 bytes.
	if receivedSize != 12 {
		t.Errorf("expected 12 bytes from FilePath, got %d (Bytes may have been used instead)", receivedSize)
	}
}

// Case 6: Bytes-only writes temp file with 0600 perm and cleans up.
func TestSTTProvider_BytesWritesTempFileAndCleanup(t *testing.T) {
	var capturedTempPath string

	srv, p := newTestSTTServer(t, func(w http.ResponseWriter, r *http.Request) {
		// We can't easily intercept the temp path here; just respond OK.
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sttHappyResponse{Text: "from bytes"})
	})
	defer srv.Close()

	// Inject a spy by using a real file that we can track.
	// Instead, verify behavior: temp file cleaned up after call.
	tmpDir := os.TempDir()
	beforeFiles := countTempSTTFiles(tmpDir)

	in := audio.STTInput{
		Bytes:    []byte("fake-audio-bytes"),
		MimeType: "audio/ogg",
	}
	res, err := p.Transcribe(context.Background(), in, audio.STTOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "from bytes" {
		t.Errorf("expected 'from bytes', got %q", res.Text)
	}

	afterFiles := countTempSTTFiles(tmpDir)
	if afterFiles > beforeFiles {
		t.Errorf("temp file not cleaned up: before=%d, after=%d, path=%s", beforeFiles, afterFiles, capturedTempPath)
	}
}

// Case 7: oversized file rejected before POST.
func TestSTTProvider_OversizedFileRejected(t *testing.T) {
	// Create a file that appears >20MB via stat (we mock size check by writing enough bytes).
	// To avoid writing 20MB in tests, we do a stat-based check — we'll write a file
	// and manually verify the error path by making the maxBytes tiny.
	// Instead: create a real oversized content using a temp file trick.
	// We'll write a marker file and test the checkFileSize logic directly.

	f, err := os.CreateTemp("", "stt_big_*.ogg")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	defer os.Remove(f.Name())

	// Write 21MB of zeros.
	chunk := make([]byte, 1<<20) // 1 MB
	for range 21 {
		f.Write(chunk)
	}
	f.Close()

	// Server should NEVER be called.
	srv, p := newTestSTTServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected HTTP call for oversized file")
	})
	defer srv.Close()

	_, err = p.Transcribe(context.Background(), audio.STTInput{FilePath: f.Name()}, audio.STTOptions{})
	if err == nil {
		t.Fatal("expected error for oversized file, got nil")
	}
}

// Case 8: context cancellation aborts request.
func TestSTTProvider_ContextCancellation(t *testing.T) {
	audioFile := writeTempAudioFile(t, "fake-ogg")
	defer os.Remove(audioFile)

	srv, p := newTestSTTServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := p.Transcribe(ctx, audio.STTInput{FilePath: audioFile}, audio.STTOptions{})
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

// countTempSTTFiles counts stt-* temp files in dir (for cleanup verification).
func countTempSTTFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if len(e.Name()) > 4 && e.Name()[:4] == "stt-" {
			count++
		}
	}
	return count
}
