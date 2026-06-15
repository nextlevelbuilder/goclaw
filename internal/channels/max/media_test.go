package max

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// =====================================================================
// Helpers
// =====================================================================

// newMediaChannel returns a Channel ready for media tests.
func newMediaChannel(t *testing.T) *Channel {
	t.Helper()
	creds := instanceCreds{BotToken: "tok", BotID: 256747471, Username: "test"}
	cfg := instanceConfig{Mode: "polling", PollingTimeout: 30, DMPolicy: "open"}
	c, err := New("max-media-test", creds, cfg, bus.New(), nil, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// =====================================================================
// downloadInboundMedia — happy path & errors
// =====================================================================

func TestDownloadInboundMedia_EmptyAttachments(t *testing.T) {
	c := newMediaChannel(t)
	paths := c.downloadInboundMedia(context.Background(), nil)
	if paths != nil {
		t.Errorf("expected nil paths for empty input, got %v", paths)
	}
}

func TestDownloadInboundMedia_SkipsNonFileTypes(t *testing.T) {
	c := newMediaChannel(t)
	atts := []Attachment{
		{Type: AttachmentTypeContact, Payload: AttachmentPayload{}},
		{Type: AttachmentTypeShare, Payload: AttachmentPayload{}},
		{Type: AttachmentTypeLocation, Payload: AttachmentPayload{}},
		{Type: AttachmentTypeInlineKeyboard, Payload: AttachmentPayload{}},
	}
	paths := c.downloadInboundMedia(context.Background(), atts)
	if paths != nil {
		t.Errorf("expected nil paths (all non-file), got %v", paths)
	}
}

func TestDownloadInboundMedia_HappyPath(t *testing.T) {
	// Fake CDN that returns 100 bytes of image data.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", "100")
		w.Write(make([]byte, 100))
	}))
	defer srv.Close()

	c := newMediaChannel(t)
	atts := []Attachment{
		{Type: AttachmentTypeImage, Payload: AttachmentPayload{URL: srv.URL + "/img.jpg"}},
	}
	paths := c.downloadInboundMedia(context.Background(), atts)
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d: %v", len(paths), paths)
	}
	defer os.Remove(paths[0])

	// Verify file exists and has the right size.
	info, err := os.Stat(paths[0])
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() != 100 {
		t.Errorf("size = %d, want 100", info.Size())
	}
	if !strings.HasPrefix(info.Name(), "goclaw_max_image_") {
		t.Errorf("name = %q, expected goclaw_max_image_* prefix", info.Name())
	}
	if !strings.HasSuffix(info.Name(), ".jpg") {
		t.Errorf("name = %q, expected .jpg suffix", info.Name())
	}
}

func TestDownloadInboundMedia_ServerError_LogsAndContinues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newMediaChannel(t)
	atts := []Attachment{
		{Type: AttachmentTypeImage, Payload: AttachmentPayload{URL: srv.URL + "/fail.jpg"}},
	}
	// Should not panic. Should return empty paths after retries exhausted.
	paths := c.downloadInboundMedia(context.Background(), atts)
	if len(paths) != 0 {
		t.Errorf("expected 0 paths on server error, got %d", len(paths))
		for _, p := range paths {
			os.Remove(p)
		}
	}
}

func TestDownloadInboundMedia_TooLarge_Skipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Pretend we have a huge file via Content-Length.
		w.Header().Set("Content-Length", "999999999")
		w.WriteHeader(http.StatusOK)
		// Don't actually write all that — Content-Length pre-check rejects.
	}))
	defer srv.Close()

	c := newMediaChannel(t)
	atts := []Attachment{
		{Type: AttachmentTypeImage, Payload: AttachmentPayload{URL: srv.URL + "/huge.jpg"}},
	}
	paths := c.downloadInboundMedia(context.Background(), atts)
	if len(paths) != 0 {
		t.Errorf("expected 0 paths for too-large file, got %d", len(paths))
		for _, p := range paths {
			os.Remove(p)
		}
	}
}

func TestDownloadInboundMedia_NoURL(t *testing.T) {
	c := newMediaChannel(t)
	atts := []Attachment{
		{Type: AttachmentTypeImage, Payload: AttachmentPayload{URL: ""}},
	}
	paths := c.downloadInboundMedia(context.Background(), atts)
	if len(paths) != 0 {
		t.Errorf("expected 0 paths, got %d", len(paths))
	}
}

func TestDownloadInboundMedia_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow handler — context will cancel first.
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := newMediaChannel(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	atts := []Attachment{
		{Type: AttachmentTypeImage, Payload: AttachmentPayload{URL: srv.URL + "/x.jpg"}},
	}
	paths := c.downloadInboundMedia(ctx, atts)
	if len(paths) != 0 {
		t.Errorf("expected 0 paths on ctx cancel, got %d", len(paths))
	}
}

// =====================================================================
// classifyUploadType
// =====================================================================

func TestClassifyUploadType(t *testing.T) {
	tests := []struct {
		name string
		ct   string
		path string
		want string
	}{
		{"jpeg via mime", "image/jpeg", "x.jpg", "image"},
		{"png via mime", "image/png", "/tmp/y.png", "image"},
		{"video via mime", "video/mp4", "/foo/x.mp4", "video"},
		{"audio via mime", "audio/ogg", "/foo/x.ogg", "audio"},
		{"unknown mime, jpg ext", "application/octet-stream", "img.jpg", "image"},
		{"no mime, mp4 ext", "", "/x/video.mp4", "video"},
		{"no mime, mp3 ext", "", "/x/song.mp3", "audio"},
		{"no mime, txt ext", "", "/x/data.txt", "file"},
		{"no hint", "", "data", "file"},
		{"empty", "", "", "file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyUploadType(tt.ct, tt.path)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// =====================================================================
// guessExtension
// =====================================================================

func TestGuessExtension(t *testing.T) {
	tests := []struct {
		name string
		att  Attachment
		url  string
		want string
	}{
		{
			name: "filename takes precedence",
			att:  Attachment{Type: AttachmentTypeFile, Payload: AttachmentPayload{Filename: "doc.pdf"}},
			url:  "https://x.example/file?token=abc",
			want: ".pdf",
		},
		{
			name: "url path hint",
			att:  Attachment{Type: AttachmentTypeImage},
			url:  "https://x.example/img.png?signature=z",
			want: ".png",
		},
		{
			name: "type fallback image",
			att:  Attachment{Type: AttachmentTypeImage},
			url:  "https://x.example/no-ext",
			want: ".jpg",
		},
		{
			name: "type fallback video",
			att:  Attachment{Type: AttachmentTypeVideo},
			url:  "https://x.example/no-ext",
			want: ".mp4",
		},
		{
			name: "type fallback audio",
			att:  Attachment{Type: AttachmentTypeAudio},
			url:  "https://x.example/no-ext",
			want: ".ogg",
		},
		{
			name: "type fallback sticker",
			att:  Attachment{Type: AttachmentTypeSticker},
			url:  "https://x.example/no-ext",
			want: ".webp",
		},
		{
			name: "no hint at all",
			att:  Attachment{Type: AttachmentTypeFile},
			url:  "https://x.example/no-ext",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := guessExtension(tt.att, tt.url)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// =====================================================================
// sanitizeExt
// =====================================================================

func TestSanitizeExt(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{".jpg", ".jpg"},
		{".JPG", ".jpg"},
		{".PnG", ".png"},
		{".jpg!?", ".jpg"},
		{"", ""},
		{"jpg", ""}, // no leading dot
		{".", ""},
		{".thisistoolong", ".thisi"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := sanitizeExt(tt.in)
			if got != tt.want {
				t.Errorf("sanitizeExt(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// =====================================================================
// urlHost — log helper
// =====================================================================

func TestUrlHost(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"https://example.com/path?token=xyz", "example.com"},
		{"http://192.168.1.1/x", "192.168.1.1"},
		{"https://foo.bar.baz", "foo.bar.baz"},
		{"not a url", "?"},
		{"", "?"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := urlHost(tt.in)
			if got != tt.want {
				t.Errorf("urlHost(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// =====================================================================
// uploadAndAttachMedia — empty input
// =====================================================================

func TestUploadAndAttachMedia_EmptyInput(t *testing.T) {
	c := newMediaChannel(t)
	atts, errs := c.uploadAndAttachMedia(context.Background(), nil)
	if atts != nil || errs != nil {
		t.Errorf("expected (nil,nil), got (%v, %v)", atts, errs)
	}
}

func TestUploadOneMedia_FileNotFound(t *testing.T) {
	c := newMediaChannel(t)
	_, err := c.uploadOneMedia(context.Background(), bus.MediaAttachment{
		URL: "/path/that/does/not/exist/file.png",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestUploadOneMedia_EmptyURL(t *testing.T) {
	c := newMediaChannel(t)
	_, err := c.uploadOneMedia(context.Background(), bus.MediaAttachment{URL: ""})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

// =====================================================================
// buildAttachmentFromUploadResponse
// =====================================================================

func TestBuildAttachmentFromUploadResponse(t *testing.T) {
	tests := []struct {
		name     string
		typ      string
		resp     uploadResponse
		wantType string
		wantErr  bool
	}{
		{
			name:     "image with photo_ids",
			typ:      "image",
			resp:     uploadResponse{PhotoIDs: map[int64]string{42: "tok-abc"}},
			wantType: AttachmentTypeImage,
		},
		{
			name:     "image with legacy photos",
			typ:      "image",
			resp:     uploadResponse{Photos: map[int64]string{99: "tok-x"}},
			wantType: AttachmentTypeImage,
		},
		{
			name:    "image empty",
			typ:     "image",
			resp:    uploadResponse{},
			wantErr: true,
		},
		{
			name:     "video with token",
			typ:      "video",
			resp:     uploadResponse{Token: "vtok"},
			wantType: AttachmentTypeVideo,
		},
		{
			name:    "video empty",
			typ:     "video",
			resp:    uploadResponse{},
			wantErr: true,
		},
		{
			name:     "audio with token",
			typ:      "audio",
			resp:     uploadResponse{Token: "atok"},
			wantType: AttachmentTypeAudio,
		},
		{
			name:     "file with token",
			typ:      "file",
			resp:     uploadResponse{Token: "ftok"},
			wantType: AttachmentTypeFile,
		},
		{
			name:    "unknown type",
			typ:     "xyz",
			resp:    uploadResponse{Token: "x"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			att, err := buildAttachmentFromUploadResponse(tt.typ, tt.resp)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && att.Type != tt.wantType {
				t.Errorf("got type %q, want %q", att.Type, tt.wantType)
			}
		})
	}
}
