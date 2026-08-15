package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/mymmrac/telego"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

type telegramTestSTT struct {
	name  string
	input audio.STTInput
}

func (s *telegramTestSTT) Name() string { return s.name }

func (s *telegramTestSTT) Transcribe(_ context.Context, in audio.STTInput, _ audio.STTOptions) (*audio.TranscriptResult, error) {
	s.input = in
	return &audio.TranscriptResult{Text: "xin chao", Provider: s.name}, nil
}

// --- buildMediaTags tests ---

// TestBuildMediaTags_NoTranscript_Legacy verifies that the pre-patch behaviour is
// preserved: audio/voice items without a transcript still produce plain tags,
// and all other media types are unaffected.
func TestBuildMediaTags_NoTranscript_Legacy(t *testing.T) {
	tests := []struct {
		name  string
		items []MediaInfo
		want  string
	}{
		{
			name:  "image",
			items: []MediaInfo{{Type: "image"}},
			want:  "<media:image>",
		},
		{
			name:  "video",
			items: []MediaInfo{{Type: "video"}},
			want:  "<media:video>",
		},
		{
			name:  "animation",
			items: []MediaInfo{{Type: "animation"}},
			want:  "<media:video>",
		},
		{
			name:  "audio without transcript",
			items: []MediaInfo{{Type: "audio"}},
			want:  "<media:audio>",
		},
		{
			name:  "voice without transcript",
			items: []MediaInfo{{Type: "voice"}},
			want:  "<media:voice>",
		},
		{
			name:  "document",
			items: []MediaInfo{{Type: "document"}},
			want:  "<media:document>",
		},
		{
			name:  "empty list",
			items: []MediaInfo{},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildMediaTags(tt.items)
			if got != tt.want {
				t.Errorf("buildMediaTags(%v) = %q, want %q", tt.items, got, tt.want)
			}
		})
	}
}

// TestBuildMediaTags_VoiceWithTranscript verifies that a voice item with a
// populated Transcript field generates the <transcript> sub-block.
func TestBuildMediaTags_VoiceWithTranscript(t *testing.T) {
	items := []MediaInfo{{Type: "voice", Transcript: "xin chào"}}
	got := buildMediaTags(items)

	if !strings.HasPrefix(got, "<media:voice>") {
		t.Errorf("expected output to start with <media:voice>, got: %q", got)
	}
	if !strings.Contains(got, "<transcript>") {
		t.Errorf("expected <transcript> block, got: %q", got)
	}
	if !strings.Contains(got, "xin chào") {
		t.Errorf("expected transcript text in output, got: %q", got)
	}
	if !strings.Contains(got, "</transcript>") {
		t.Errorf("expected closing </transcript>, got: %q", got)
	}
}

// TestBuildMediaTags_AudioWithTranscript verifies the same for audio type.
func TestBuildMediaTags_AudioWithTranscript(t *testing.T) {
	items := []MediaInfo{{Type: "audio", Transcript: "hello world"}}
	got := buildMediaTags(items)

	if !strings.HasPrefix(got, "<media:audio>") {
		t.Errorf("expected output to start with <media:audio>, got: %q", got)
	}
	if !strings.Contains(got, "<transcript>hello world</transcript>") {
		t.Errorf("expected transcript content, got: %q", got)
	}
}

// TestBuildMediaTags_TranscriptHTMLEscaping verifies that special HTML characters
// in the transcript are properly escaped to prevent XML injection.
func TestBuildMediaTags_TranscriptHTMLEscaping(t *testing.T) {
	items := []MediaInfo{{Type: "voice", Transcript: `<script>alert("xss")</script>`}}
	got := buildMediaTags(items)

	// Raw angle brackets must NOT appear inside the transcript block.
	if strings.Contains(got, "<script>") {
		t.Errorf("unescaped <script> tag found in output — XSS risk: %q", got)
	}
	// Escaped form must be present.
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("expected HTML-escaped content, got: %q", got)
	}
}

// TestBuildMediaTags_MultipleItems verifies correct handling of mixed media lists,
// including one voice with transcript and others without.
func TestBuildMediaTags_MultipleItems(t *testing.T) {
	items := []MediaInfo{
		{Type: "image"},
		{Type: "voice", Transcript: "hey there"},
		{Type: "document"},
	}
	got := buildMediaTags(items)
	parts := strings.Split(got, "\n")

	// Should have 3 top-level entries (image, voice block [2 lines], document)
	// but since voice produces 2 lines the split will have 4 parts.
	if !strings.Contains(parts[0], "<media:image>") {
		t.Errorf("first part should be image tag, got: %q", parts[0])
	}
	if !strings.Contains(got, "<media:voice>") {
		t.Errorf("expected voice tag, not found in: %q", got)
	}
	if !strings.Contains(got, "hey there") {
		t.Errorf("expected transcript text, not found in: %q", got)
	}
	if !strings.Contains(got, "<media:document>") {
		t.Errorf("expected document tag, not found in: %q", got)
	}
}

// TestBuildMediaTags_UnknownType verifies that an unrecognised media type is
// silently ignored (no panic, no output).
func TestBuildMediaTags_UnknownType(t *testing.T) {
	items := []MediaInfo{{Type: "sticker"}}
	got := buildMediaTags(items)
	if got != "" {
		t.Errorf("expected empty string for unknown type, got: %q", got)
	}
}

func TestTranscribeMediaAudioUsesChannelTypeAndPreservesMIME(t *testing.T) {
	mgr := audio.NewManager(audio.ManagerConfig{})
	tenantSTT := &telegramTestSTT{name: "tenant"}
	telegramSTT := &telegramTestSTT{name: "proxy"}
	mgr.RegisterSTT(tenantSTT)
	mgr.SetSTTChain([]string{"tenant"})
	mgr.RegisterChannelSTT(channels.TypeTelegram, telegramSTT)

	ch := &Channel{
		BaseChannel: channels.NewBaseChannel("telegram-main", nil, nil),
		audioMgr:    mgr,
	}
	ch.SetType(channels.TypeTelegram)

	got, err := ch.transcribeMediaAudio(context.Background(), MediaInfo{
		Type:        "voice",
		FilePath:    "/tmp/voice.ogg",
		FileName:    "voice.ogg",
		ContentType: "audio/ogg; codecs=opus",
	})
	if err != nil {
		t.Fatalf("transcribeMediaAudio returned error: %v", err)
	}
	if got != "xin chao" {
		t.Fatalf("transcript = %q, want channel override transcript", got)
	}
	if tenantSTT.input.FilePath != "" {
		t.Fatalf("tenant STT received input; expected Telegram channel override to win")
	}
	if telegramSTT.input.MimeType != "audio/ogg; codecs=opus" {
		t.Fatalf("mime = %q, want preserved Telegram voice MIME", telegramSTT.input.MimeType)
	}
	if telegramSTT.input.FilePath != "/tmp/voice.ogg" {
		t.Fatalf("file path = %q, want /tmp/voice.ogg", telegramSTT.input.FilePath)
	}
}

func TestTelegramAudioSTTMimeFallback(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		want        string
	}{
		{name: "preserves opus", contentType: "audio/ogg; codecs=opus", want: "audio/ogg; codecs=opus"},
		{name: "empty defaults ogg", contentType: "", want: "audio/ogg"},
		{name: "octet stream defaults ogg", contentType: "application/octet-stream", want: "audio/ogg"},
		{name: "trims whitespace", contentType: " audio/mpeg ", want: "audio/mpeg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := telegramAudioSTTMime(tt.contentType); got != tt.want {
				t.Fatalf("telegramAudioSTTMime(%q) = %q, want %q", tt.contentType, got, tt.want)
			}
		})
	}
}

func TestResolveMediaTelegramVoiceDownloadsOggOpusFixture(t *testing.T) {
	const token = "123456789:aaaabbbbaaaabbbbaaaabbbbaaaabbbbccc"
	const fileID = "telegram-voice-file"
	oggOpus := []byte("OggS\x00\x02telegram-voice-opus-fixture")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bot" + token + "/getFile":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_id":"` + fileID + `","file_unique_id":"voice-unique","file_path":"voice/file.ogg"}}`))
		case "/file/bot" + token + "/voice/file.ogg":
			w.Header().Set("Content-Type", "audio/ogg")
			_, _ = w.Write(oggOpus)
		default:
			t.Fatalf("unexpected Telegram API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	ch, err := New(config.TelegramConfig{
		Token:     token,
		APIServer: server.URL,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("New channel: %v", err)
	}

	mediaList, mediaErrors := ch.resolveMedia(context.Background(), &telego.Message{
		Voice: &telego.Voice{
			FileID:   fileID,
			MimeType: "audio/ogg; codecs=opus",
			FileSize: int64(len(oggOpus)),
		},
	})
	if len(mediaErrors) > 0 {
		t.Fatalf("resolveMedia errors: %+v", mediaErrors)
	}
	if len(mediaList) != 1 {
		t.Fatalf("got %d media items, want 1", len(mediaList))
	}

	voice := mediaList[0]
	if voice.Type != "voice" {
		t.Fatalf("media type = %q, want voice", voice.Type)
	}
	if voice.ContentType != "audio/ogg; codecs=opus" {
		t.Fatalf("content type = %q, want Telegram voice MIME", voice.ContentType)
	}
	if voice.FilePath == "" {
		t.Fatalf("expected downloaded file path")
	}
	t.Cleanup(func() { _ = os.Remove(voice.FilePath) })

	data, err := os.ReadFile(voice.FilePath)
	if err != nil {
		t.Fatalf("read downloaded voice fixture: %v", err)
	}
	if !strings.HasPrefix(string(data), "OggS") {
		t.Fatalf("downloaded fixture does not look like OGG data: %q", string(data))
	}
	if got := telegramAudioSTTMime(voice.ContentType); got != "audio/ogg; codecs=opus" {
		t.Fatalf("STT mime = %q, want preserved Telegram voice MIME", got)
	}
}

func TestExtractMediaRefsPreservesDocumentMetadata(t *testing.T) {
	msg := &telego.Message{
		Document: &telego.Document{
			FileID:   "telegram-file-id",
			FileName: "codex.zip",
			MimeType: "application/zip",
			FileSize: 1234,
		},
	}

	got := extractMediaRefs(msg)
	want := []channels.MediaRef{{
		Type:        "document",
		FileID:      "telegram-file-id",
		FileSize:    1234,
		FileName:    "codex.zip",
		ContentType: "application/zip",
	}}

	if len(got) != len(want) {
		t.Fatalf("got %d refs, want %d", len(got), len(want))
	}
	if got[0] != want[0] {
		t.Fatalf("ref = %+v, want %+v", got[0], want[0])
	}
}

func TestPrependMediaInfoFilesKeepsHistoryBeforeCurrent(t *testing.T) {
	current := []bus.MediaFile{{
		Path:     "/workspace/.uploads/current.pdf",
		MimeType: "application/pdf",
		Filename: "current.pdf",
	}}
	history := []MediaInfo{{
		Type:        "document",
		FilePath:    "/workspace/.uploads/codex.zip",
		ContentType: "application/zip",
		FileName:    "codex.zip",
	}}

	got := prependMediaInfoFiles(current, history)
	if len(got) != 2 {
		t.Fatalf("got %d files, want 2", len(got))
	}
	if got[0].Path != history[0].FilePath || got[1].Path != current[0].Path {
		t.Fatalf("order = [%q, %q], want history before current", got[0].Path, got[1].Path)
	}
}
