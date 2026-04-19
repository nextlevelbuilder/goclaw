package proxy_stt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
)

// proxyTestResponse mirrors the STT proxy JSON response for test assertions.
// Proxy returns "transcript" (not "text" like Scribe).
type proxyTestResponse struct {
	Transcript string `json:"transcript"`
}

const proxyEndpoint = "/transcribe_audio"

// writeTempAudio writes a fake audio file and returns its path.
func writeTempAudio(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "stt_proxy_test_*.ogg")
	if err != nil {
		t.Fatalf("create temp audio file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp audio file: %v", err)
	}
	f.Close()
	return f.Name()
}

func newProvider(proxyURL, apiKey, tenantID string) *Provider {
	return NewProvider(media.STTConfig{
		ProxyURL: proxyURL,
		APIKey:   apiKey,
		TenantID: tenantID,
	})
}

// Case 1: NoProxy — empty ProxyURL returns ("", nil) without HTTP call.
func TestTranscribeAudio_NoProxy(t *testing.T) {
	p := newProvider("", "", "")
	res, err := p.Transcribe(context.Background(), audio.STTInput{FilePath: "/any/file.ogg"}, audio.STTOptions{})
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if res.Text != "" {
		t.Fatalf("expected empty transcript, got: %q", res.Text)
	}
}

// Case 2: EmptyFilePath — empty filePath is silent no-op even when STT configured.
func TestTranscribeAudio_EmptyFilePath(t *testing.T) {
	p := newProvider("https://stt.example.com", "", "")
	res, err := p.Transcribe(context.Background(), audio.STTInput{FilePath: ""}, audio.STTOptions{})
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if res.Text != "" {
		t.Fatalf("expected empty transcript, got: %q", res.Text)
	}
}

// Case 3: MissingFile — non-existent file returns error (not silent).
func TestTranscribeAudio_MissingFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected HTTP call for missing file")
	}))
	defer srv.Close()

	p := newProvider(srv.URL, "", "")
	_, err := p.Transcribe(context.Background(), audio.STTInput{FilePath: "/nonexistent/file.ogg"}, audio.STTOptions{})
	if err == nil {
		t.Fatal("expected an error for missing file, got nil")
	}
}

// Case 4: Success — happy path returns transcript string.
func TestTranscribeAudio_Success(t *testing.T) {
	audioFile := writeTempAudio(t, "fake-ogg-bytes")
	defer os.Remove(audioFile)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != proxyEndpoint {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
		}
		if _, _, err := r.FormFile("file"); err != nil {
			t.Errorf("expected 'file' field in multipart form: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(proxyTestResponse{Transcript: "hello world"})
	}))
	defer srv.Close()

	p := newProvider(srv.URL, "", "")
	res, err := p.Transcribe(context.Background(), audio.STTInput{FilePath: audioFile}, audio.STTOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", res.Text)
	}
	if res.Provider != "proxy" {
		t.Errorf("expected provider 'proxy', got %q", res.Provider)
	}
}

// Case 5: BearerToken — STTAPIKey sent as Authorization: Bearer header.
func TestTranscribeAudio_BearerToken(t *testing.T) {
	audioFile := writeTempAudio(t, "fake-ogg-bytes")
	defer os.Remove(audioFile)

	const wantKey = "super-secret-key"
	var gotAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(proxyTestResponse{Transcript: "ok"})
	}))
	defer srv.Close()

	p := newProvider(srv.URL, wantKey, "")
	if _, err := p.Transcribe(context.Background(), audio.STTInput{FilePath: audioFile}, audio.STTOptions{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer "+wantKey {
		t.Errorf("expected Authorization %q, got %q", "Bearer "+wantKey, gotAuth)
	}
}

// Case 6: NoAuthHeader — empty APIKey sends no Authorization header.
func TestTranscribeAudio_NoAuthHeader(t *testing.T) {
	audioFile := writeTempAudio(t, "fake-ogg-bytes")
	defer os.Remove(audioFile)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("expected no Authorization header, got %q", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(proxyTestResponse{Transcript: "ok"})
	}))
	defer srv.Close()

	p := newProvider(srv.URL, "", "")
	if _, err := p.Transcribe(context.Background(), audio.STTInput{FilePath: audioFile}, audio.STTOptions{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Case 7: TenantID — STTTenantID forwarded as multipart "tenant_id" field.
func TestTranscribeAudio_TenantID(t *testing.T) {
	audioFile := writeTempAudio(t, "fake-ogg-bytes")
	defer os.Remove(audioFile)

	const wantTenant = "acme-corp"
	var gotTenant string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err == nil {
			gotTenant = r.FormValue("tenant_id")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(proxyTestResponse{Transcript: "ok"})
	}))
	defer srv.Close()

	p := newProvider(srv.URL, "", wantTenant)
	if _, err := p.Transcribe(context.Background(), audio.STTInput{FilePath: audioFile}, audio.STTOptions{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotTenant != wantTenant {
		t.Errorf("expected tenant_id %q, got %q", wantTenant, gotTenant)
	}
}

// Case 8: NoTenantField — empty TenantID sends no "tenant_id" form field.
func TestTranscribeAudio_NoTenantField(t *testing.T) {
	audioFile := writeTempAudio(t, "fake-ogg-bytes")
	defer os.Remove(audioFile)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err == nil {
			if tid := r.FormValue("tenant_id"); tid != "" {
				t.Errorf("expected no tenant_id field, got %q", tid)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(proxyTestResponse{Transcript: "ok"})
	}))
	defer srv.Close()

	p := newProvider(srv.URL, "", "")
	if _, err := p.Transcribe(context.Background(), audio.STTInput{FilePath: audioFile}, audio.STTOptions{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Case 9: UpstreamError — non-200 response surfaces as error mentioning status code.
func TestTranscribeAudio_UpstreamError(t *testing.T) {
	audioFile := writeTempAudio(t, "fake-ogg-bytes")
	defer os.Remove(audioFile)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	p := newProvider(srv.URL, "", "")
	_, err := p.Transcribe(context.Background(), audio.STTInput{FilePath: audioFile}, audio.STTOptions{})
	if err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("expected error to mention status 503, got: %v", err)
	}
}

// Case 10: InvalidJSON — 200 response with malformed JSON returns parse error.
func TestTranscribeAudio_InvalidJSON(t *testing.T) {
	audioFile := writeTempAudio(t, "fake-ogg-bytes")
	defer os.Remove(audioFile)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	p := newProvider(srv.URL, "", "")
	_, err := p.Transcribe(context.Background(), audio.STTInput{FilePath: audioFile}, audio.STTOptions{})
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// Case 11: EmptyTranscript — 200 + {"transcript":""} returns ("", nil), not an error.
func TestTranscribeAudio_EmptyTranscript(t *testing.T) {
	audioFile := writeTempAudio(t, "fake-ogg-bytes")
	defer os.Remove(audioFile)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(proxyTestResponse{Transcript: ""})
	}))
	defer srv.Close()

	p := newProvider(srv.URL, "", "")
	res, err := p.Transcribe(context.Background(), audio.STTInput{FilePath: audioFile}, audio.STTOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Text != "" {
		t.Errorf("expected empty transcript, got %q", res.Text)
	}
}

// Case 12: ContextCancelled — cancelled context causes HTTP call to fail fast.
func TestTranscribeAudio_ContextCancelled(t *testing.T) {
	audioFile := writeTempAudio(t, "fake-ogg-bytes")
	defer os.Remove(audioFile)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	p := newProvider(srv.URL, "", "")
	_, err := p.Transcribe(ctx, audio.STTInput{FilePath: audioFile}, audio.STTOptions{})
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}
