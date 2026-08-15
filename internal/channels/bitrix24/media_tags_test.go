package bitrix24

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
)

// TestClassifyMediaType pins the MIME → media.Type* mapping. The fallback to
// TypeDocument is the safest default: it routes through read_document's local
// parser + vision provider chain, which handles PDF / DOCX / generic binaries.
// Bogus audio/video classification on an unrelated MIME would mislead the LLM
// into calling read_audio / read_video on something they can't transcribe.
func TestClassifyMediaType(t *testing.T) {
	cases := []struct {
		mime string
		want string
	}{
		{"image/jpeg", media.TypeImage},
		{"image/png", media.TypeImage},
		{"image/webp", media.TypeImage},
		{"video/mp4", media.TypeVideo},
		{"video/webm", media.TypeVideo},
		// Bitrix ships voice notes as audio/* in the MIME — TypeAudio covers
		// both regular audio and voice notes here.
		{"audio/mpeg", media.TypeAudio},
		{"audio/ogg", media.TypeAudio},
		{"audio/wav", media.TypeAudio},
		// PDF / DOCX / generic binaries route to TypeDocument.
		{"application/pdf", media.TypeDocument},
		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", media.TypeDocument},
		{"application/octet-stream", media.TypeDocument},
		{"text/plain", media.TypeDocument},
		// Unknown / empty MIME stays as TypeDocument (safest fallback).
		{"", media.TypeDocument},
		{"weird/unknown", media.TypeDocument},
	}
	for _, tc := range cases {
		t.Run(tc.mime, func(t *testing.T) {
			if got := classifyMediaType(tc.mime); got != tc.want {
				t.Errorf("classifyMediaType(%q) = %q; want %q", tc.mime, got, tc.want)
			}
		})
	}
}

// TestMediaFilesToInfos pins the bus.MediaFile → media.MediaInfo conversion:
// FilePath / FileName / ContentType passthrough, Type via classifyMediaType,
// nil input → nil output, multiple files preserve order.
func TestMediaFilesToInfos(t *testing.T) {
	t.Run("nil input returns nil", func(t *testing.T) {
		if got := mediaFilesToInfos(nil); got != nil {
			t.Errorf("nil input: got %v, want nil", got)
		}
	})

	t.Run("empty slice returns nil", func(t *testing.T) {
		if got := mediaFilesToInfos([]bus.MediaFile{}); got != nil {
			t.Errorf("empty slice: got %v, want nil", got)
		}
	})

	t.Run("mixed kinds preserved with correct Type", func(t *testing.T) {
		in := []bus.MediaFile{
			{Path: "/tmp/a.pdf", MimeType: "application/pdf", Filename: "report.pdf"},
			{Path: "/tmp/b.mp3", MimeType: "audio/mpeg", Filename: "voice.mp3"},
			{Path: "/tmp/c.jpg", MimeType: "image/jpeg", Filename: "photo.jpg"},
		}
		got := mediaFilesToInfos(in)
		if len(got) != 3 {
			t.Fatalf("len = %d, want 3", len(got))
		}
		wantTypes := []string{media.TypeDocument, media.TypeAudio, media.TypeImage}
		wantNames := []string{"report.pdf", "voice.mp3", "photo.jpg"}
		for i, info := range got {
			if info.Type != wantTypes[i] {
				t.Errorf("[%d] Type = %q; want %q", i, info.Type, wantTypes[i])
			}
			if info.FileName != wantNames[i] {
				t.Errorf("[%d] FileName = %q; want %q", i, info.FileName, wantNames[i])
			}
			if info.FilePath != in[i].Path {
				t.Errorf("[%d] FilePath = %q; want %q", i, info.FilePath, in[i].Path)
			}
			if info.ContentType != in[i].MimeType {
				t.Errorf("[%d] ContentType = %q; want %q", i, info.ContentType, in[i].MimeType)
			}
		}
	})

	t.Run("end-to-end with BuildMediaTags produces expected tags", func(t *testing.T) {
		in := []bus.MediaFile{
			{Path: "/tmp/x.pdf", MimeType: "application/pdf", Filename: "HDDT (12).pdf"},
			{Path: "/tmp/y.mp3", MimeType: "audio/mpeg", Filename: "song.mp3"},
		}
		tags := media.BuildMediaTags(mediaFilesToInfos(in))
		// Document: name attribute included (BuildMediaTags emits name=).
		if !contains(tags, `<media:document name="HDDT (12).pdf">`) {
			t.Errorf("missing document tag with name; got: %q", tags)
		}
		// Audio: bare tag (no name attribute, no transcript yet).
		if !contains(tags, "<media:audio>") {
			t.Errorf("missing audio tag; got: %q", tags)
		}
	})
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
