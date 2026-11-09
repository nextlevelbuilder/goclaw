package personal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
)

func withSSRFBypass(t *testing.T) {
	t.Helper()
	old := checkMediaURL
	checkMediaURL = func(string) error { return nil }
	t.Cleanup(func() { checkMediaURL = old })
}

func TestDownloadFileWithClientSendsSessionHeaders(t *testing.T) {
	withSSRFBypass(t)

	var gotUA, gotReferer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotReferer = r.Header.Get("Referer")
		_, _ = w.Write([]byte("file-bytes"))
	}))
	defer srv.Close()

	path, err := downloadFileWithClient(context.Background(), srv.Client(), "test-ua", 1024, srv.URL+"/file.bin")
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer os.Remove(path)
	if gotUA != "test-ua" {
		t.Fatalf("User-Agent = %q, want test-ua", gotUA)
	}
	if gotReferer != "https://chat.zalo.me/" {
		t.Fatalf("Referer = %q, want https://chat.zalo.me/", gotReferer)
	}
}

func TestDownloadFileWithClientRejectsOversize(t *testing.T) {
	withSSRFBypass(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("1234567890"))
	}))
	defer srv.Close()

	if _, err := downloadFileWithClient(context.Background(), srv.Client(), "", 4, srv.URL+"/big.bin"); err == nil {
		t.Fatal("oversize download succeeded, want error")
	} else if !strings.Contains(err.Error(), "file too large") {
		t.Fatalf("error = %v, want file too large", err)
	}
}

func TestExtractContentAndMediaUsesInjectedFetcher(t *testing.T) {
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "photo.png")
	png := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 32)...)
	if err := os.WriteFile(imagePath, png, 0644); err != nil {
		t.Fatal(err)
	}

	content := protocol.Content{Raw: json.RawMessage(`{"title":"brief.png","href":"https://example.com/brief.png"}`)}
	called := 0
	got, media := extractContentAndMediaWithFetcher(content, "chat.photo", "", func(context.Context, string) (string, error) {
		called++
		return imagePath, nil
	})
	if called != 1 {
		t.Fatalf("fetcher calls = %d, want 1", called)
	}
	if len(media) != 1 || media[0] != imagePath {
		t.Fatalf("media = %v, want %q", media, imagePath)
	}
	if !strings.Contains(got, "<media:image>") || !strings.Contains(got, "brief.png") {
		t.Fatalf("content = %q, want media tag and caption", got)
	}
}
