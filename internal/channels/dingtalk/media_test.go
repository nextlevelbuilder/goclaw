package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
)

// --- inbound ---------------------------------------------------------------

// The OSS-signed download URL rejects any request carrying a Content-Type header
// that was not part of the signature. This is the single most likely opaque 403
// in the media path, so the stub enforces it.
func TestMedia_DownloadOmitsContentType(t *testing.T) {
	oss := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "" {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, "SignatureDoesNotMatch")
			return
		}
		fmt.Fprint(w, "IMAGEBYTES")
	}))
	defer oss.Close()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/oauth2/accessToken":
			fmt.Fprint(w, `{"accessToken":"t","expireIn":7200}`)
		case pathMessageFilesDownload:
			var body map[string]string
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			if body["robotCode"] != "robot-1" {
				t.Errorf("robotCode = %q", body["robotCode"])
			}
			if body["downloadCode"] != "dc-1" {
				t.Errorf("downloadCode = %q", body["downloadCode"])
			}
			fmt.Fprintf(w, `{"downloadUrl":%q}`, oss.URL)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer api.Close()

	ch, _ := newTestChannelCfg(t, baseCfg())
	ch.client.apiBase = api.URL

	info, err := ch.fetchMedia(context.Background(), mediaRef{Kind: "picture", DownloadCode: "dc-1"})
	if err != nil {
		t.Fatalf("fetchMedia: %v", err)
	}
	t.Cleanup(func() { os.Remove(info.FilePath) })

	body, err := os.ReadFile(info.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "IMAGEBYTES" {
		t.Errorf("downloaded %q", body)
	}
	if info.Type != media.TypeImage {
		t.Errorf("Type = %q, want image", info.Type)
	}
	if filepath.Ext(info.FilePath) != ".jpg" {
		t.Errorf("temp file %q lost its extension; MIME sniffing downstream depends on it", info.FilePath)
	}
}

func TestMedia_DownloadURLMissing(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1.0/oauth2/accessToken" {
			fmt.Fprint(w, `{"accessToken":"t","expireIn":7200}`)
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	defer api.Close()

	ch, _ := newTestChannelCfg(t, baseCfg())
	ch.client.apiBase = api.URL

	if _, err := ch.fetchMedia(context.Background(), mediaRef{Kind: "file", DownloadCode: "dc"}); err == nil {
		t.Fatal("want error when downloadUrl is absent")
	}
}

// media_max_mb must be enforced, and the oversized bytes must not reach disk.
func TestMedia_RejectsOversizedInbound(t *testing.T) {
	oss := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(bytes.Repeat([]byte("x"), 3*1024*1024))
	}))
	defer oss.Close()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1.0/oauth2/accessToken" {
			fmt.Fprint(w, `{"accessToken":"t","expireIn":7200}`)
			return
		}
		fmt.Fprintf(w, `{"downloadUrl":%q}`, oss.URL)
	}))
	defer api.Close()

	cfg := baseCfg()
	cfg.MediaMaxMB = 1
	ch, _ := newTestChannelCfg(t, cfg)
	ch.client.apiBase = api.URL

	_, err := ch.fetchMedia(context.Background(), mediaRef{Kind: "file", DownloadCode: "dc"})
	if err == nil || !strings.Contains(err.Error(), "over the") {
		t.Fatalf("want a size-limit error, got %v", err)
	}
}

// A broken attachment must not silence the text that came with it.
func TestMedia_ResolveSkipsFailures(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1.0/oauth2/accessToken" {
			fmt.Fprint(w, `{"accessToken":"t","expireIn":7200}`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer api.Close()

	ch, _ := newTestChannelCfg(t, baseCfg())
	ch.client.apiBase = api.URL

	got := ch.resolveMedia(context.Background(), &inbound{
		Text:  "see attached",
		Media: []mediaRef{{Kind: "file", DownloadCode: "dc"}},
	})
	if len(got) != 0 {
		t.Errorf("failed download produced media: %+v", got)
	}
}

func TestMediaTypeMapping(t *testing.T) {
	for kind, want := range map[string]string{
		"picture": media.TypeImage,
		"audio":   media.TypeVoice,
		"video":   media.TypeVideo,
		"file":    media.TypeDocument,
		"unknown": media.TypeDocument,
	} {
		if got := mediaTypeFor(kind); got != want {
			t.Errorf("mediaTypeFor(%q) = %q, want %q", kind, got, want)
		}
	}
}

// --- outbound --------------------------------------------------------------

// stubUpload serves the oapi token, /media/upload, and the chunk transaction.
type stubUpload struct {
	mu          sync.Mutex
	uploadType  string
	formField   string
	chunkBodies [][]byte
	chunkNums   []int
	totalChunks int
	mediaID     string
	srv         *httptest.Server
}

func newStubUpload(t *testing.T, mediaID string) *stubUpload {
	t.Helper()
	s := &stubUpload{mediaID: mediaID}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gettoken":
			fmt.Fprint(w, `{"errcode":0,"access_token":"t","expires_in":7200}`)

		case pathMediaUpload:
			s.mu.Lock()
			s.uploadType = r.URL.Query().Get("type")
			s.mu.Unlock()
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				t.Errorf("parse multipart: %v", err)
			}
			for name := range r.MultipartForm.File {
				s.mu.Lock()
				s.formField = name
				s.mu.Unlock()
			}
			fmt.Fprintf(w, `{"errcode":0,"media_id":%q}`, s.mediaID)

		case pathChunkEnable:
			fmt.Fprint(w, `{"errcode":0,"upload_id":"up-1"}`)

		case pathChunkUpload:
			n, _ := strconv.Atoi(r.URL.Query().Get("chunk_number"))
			total, _ := strconv.Atoi(r.URL.Query().Get("total_chunks"))
			_ = r.ParseMultipartForm(32 << 20)
			f, _, _ := r.FormFile("file")
			data, _ := io.ReadAll(f)

			s.mu.Lock()
			s.chunkNums = append(s.chunkNums, n)
			s.chunkBodies = append(s.chunkBodies, data)
			s.totalChunks = total
			s.mu.Unlock()
			fmt.Fprint(w, `{"errcode":0}`)

		case pathChunkSubmit:
			fmt.Fprint(w, `{"errcode":0,"file_id":"file-1","download_code":"dl-1"}`)

		default:
			t.Errorf("unexpected oapi path %q", r.URL.Path)
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// DingTalk prefixes media ids with '@'. Passing it through verbatim makes the
// send fail with an unhelpful "invalid media".
func TestUpload_StripsAtPrefix(t *testing.T) {
	up := newStubUpload(t, "@lADPabc123")
	ch, _ := newTestChannelCfg(t, baseCfg())
	ch.client.oapiBase = up.srv.URL

	id, err := ch.upload(context.Background(), uploadImage, "pic.jpg", []byte("bytes"))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if id != "lADPabc123" {
		t.Errorf("mediaID = %q, want the '@' stripped", id)
	}
	if up.uploadType != "image" {
		t.Errorf("type query = %q", up.uploadType)
	}
	if up.formField != multipartMediaFile {
		t.Errorf("multipart field = %q, want %q", up.formField, multipartMediaFile)
	}
}

func TestUpload_ChunksAbove20MB(t *testing.T) {
	up := newStubUpload(t, "unused")
	ch, _ := newTestChannelCfg(t, baseCfg())
	ch.client.oapiBase = up.srv.URL

	const size = chunkThreshold + 3*1024*1024 // 23MB
	body := bytes.Repeat([]byte("z"), size)

	id, err := ch.upload(context.Background(), uploadFile, "big.zip", body)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if id != "file-1" {
		t.Errorf("id = %q, want the chunk transaction's file_id", id)
	}

	wantChunks := (size + chunkSizeSmall - 1) / chunkSizeSmall
	if len(up.chunkNums) != wantChunks {
		t.Fatalf("uploaded %d chunks, want %d", len(up.chunkNums), wantChunks)
	}
	// chunk_number is 1-based; a 0-based sequence is silently accepted and
	// reassembles wrong.
	for i, n := range up.chunkNums {
		if n != i+1 {
			t.Errorf("chunk %d reported chunk_number %d, want %d", i, n, i+1)
		}
	}
	if up.totalChunks != wantChunks {
		t.Errorf("total_chunks = %d, want %d", up.totalChunks, wantChunks)
	}

	var reassembled int
	for _, b := range up.chunkBodies {
		reassembled += len(b)
	}
	if reassembled != size {
		t.Errorf("chunks total %d bytes, want %d", reassembled, size)
	}
}

func TestUpload_SmallFileSkipsChunking(t *testing.T) {
	up := newStubUpload(t, "m-1")
	ch, _ := newTestChannelCfg(t, baseCfg())
	ch.client.oapiBase = up.srv.URL

	if _, err := ch.upload(context.Background(), uploadFile, "small.txt", []byte("hi")); err != nil {
		t.Fatal(err)
	}
	if len(up.chunkNums) != 0 {
		t.Errorf("small file used the chunk transaction")
	}
}

func TestChunkSizeFor(t *testing.T) {
	for size, want := range map[int]int{
		1 * 1024 * 1024:   chunkSizeSmall,
		chunkTier50MB:     chunkSizeSmall,
		chunkTier50MB + 1: chunkSizeMedium,
		chunkTier100MB:    chunkSizeMedium,
		200 * 1024 * 1024: chunkSizeLarge,
	} {
		if got := chunkSizeFor(size); got != want {
			t.Errorf("chunkSizeFor(%d) = %d, want %d", size, got, want)
		}
	}
}

// An oversized image must be rejected before the bytes go out, so the operator
// sees the real limit rather than DingTalk's opaque failure.
func TestCheckUploadSize(t *testing.T) {
	if err := checkUploadSize(uploadImage, maxImageBytes+1); err == nil {
		t.Error("oversized image accepted")
	}
	if err := checkUploadSize(uploadVoice, maxVoiceBytes+1); err == nil {
		t.Error("oversized voice accepted")
	}
	// video/file are not capped here: they take the chunk path instead.
	if err := checkUploadSize(uploadFile, 200*1024*1024); err != nil {
		t.Errorf("large file rejected: %v", err)
	}
	if err := checkUploadSize(uploadVideo, 200*1024*1024); err != nil {
		t.Errorf("large video rejected: %v", err)
	}
}

func TestUploadKindFor(t *testing.T) {
	tests := []struct {
		contentType, path string
		want              uploadKind
	}{
		{"image/png", "a.png", uploadImage},
		{"audio/amr", "a.amr", uploadVoice},
		{"video/mp4", "a.mp4", uploadVideo},
		{"application/pdf", "a.pdf", uploadFile},
		{"", "a.jpg", uploadImage},    // falls back to the extension
		{"", "a.unknown", uploadFile}, // and to file when that fails too
	}
	for _, tc := range tests {
		if got := uploadKindFor(tc.contentType, tc.path); got != tc.want {
			t.Errorf("uploadKindFor(%q,%q) = %q, want %q", tc.contentType, tc.path, got, tc.want)
		}
	}
}

// sampleVideo needs a cover image we do not have, so a video is delivered as a
// file: a worse preview, but a working download.
func TestBuildMediaPayload(t *testing.T) {
	tests := []struct {
		kind       uploadKind
		wantMsgKey string
		wantField  string
	}{
		{uploadImage, "sampleImageMsg", "photoURL"},
		{uploadVoice, "sampleAudio", "mediaId"},
		{uploadVideo, "sampleFile", "mediaId"},
		{uploadFile, "sampleFile", "mediaId"},
	}
	for _, tc := range tests {
		msgKey, msgParam, err := buildMediaPayload(tc.kind, "mid", "report.pdf")
		if err != nil {
			t.Fatal(err)
		}
		if msgKey != tc.wantMsgKey {
			t.Errorf("kind %q: msgKey = %q, want %q", tc.kind, msgKey, tc.wantMsgKey)
		}
		var decoded map[string]string
		if err := json.Unmarshal([]byte(msgParam), &decoded); err != nil {
			t.Fatalf("msgParam is not JSON: %v", err)
		}
		if decoded[tc.wantField] != "mid" {
			t.Errorf("kind %q: msgParam[%s] = %q", tc.kind, tc.wantField, decoded[tc.wantField])
		}
	}
}

// Media never rides the session webhook, even when one is live.
func TestSendMedia_AlwaysProactive(t *testing.T) {
	api := newStubDingTalk(t)
	hook := newStubWebhook(t)
	up := newStubUpload(t, "@m-1")

	ch, _ := newTestChannelCfg(t, baseCfg())
	ch.client.apiBase = api.srv.URL
	ch.client.oapiBase = up.srv.URL
	if err := ch.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ch.Stop(context.Background()) })

	tmp := filepath.Join(t.TempDir(), "pic.png")
	if err := os.WriteFile(tmp, []byte("PNGDATA"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:   "staff-1",
		Media:    []bus.MediaAttachment{{URL: tmp, ContentType: "image/png"}},
		Metadata: map[string]string{"dingtalk_chat_type": "direct", "dingtalk_session_webhook": hook.srv.URL},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if hook.hits() != 0 {
		t.Errorf("media went through the session webhook, which cannot carry it")
	}
	calls := api.calls()
	if len(calls) != 1 || calls[0].Body["msgKey"] != "sampleImageMsg" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestSendMedia_MissingFileErrors(t *testing.T) {
	api := newStubDingTalk(t)
	ch := sendingChannel(t, baseCfg(), api)

	err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:   "staff-1",
		Media:    []bus.MediaAttachment{{URL: "/nonexistent/file.png", ContentType: "image/png"}},
		Metadata: map[string]string{"dingtalk_chat_type": "direct"},
	})
	if err == nil {
		t.Fatal("want error for a missing attachment file")
	}
}
