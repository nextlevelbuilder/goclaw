package slack

import (
	"strings"
	"testing"
)

// --- classifyMime ---

func TestClassifyMime(t *testing.T) {
	tests := []struct {
		mime string
		want string
	}{
		{"image/jpeg", "image"},
		{"image/png", "image"},
		{"image/gif", "image"},
		{"image/webp", "image"},
		{"audio/mpeg", "audio"},
		{"audio/ogg", "audio"},
		{"audio/wav", "audio"},
		{"application/pdf", "document"},
		{"text/plain", "document"},
		{"application/octet-stream", "document"},
		{"video/mp4", "document"}, // video falls through to document in slack
		{"", "document"},
	}

	for _, tt := range tests {
		t.Run(tt.mime, func(t *testing.T) {
			got := classifyMime(tt.mime)
			if got != tt.want {
				t.Errorf("classifyMime(%q) = %q, want %q", tt.mime, got, tt.want)
			}
		})
	}
}

// --- buildMediaTags ---

func TestBuildMediaTags_Empty(t *testing.T) {
	got := buildMediaTags(nil)
	if got != "" {
		t.Errorf("buildMediaTags(nil) = %q, want empty", got)
	}
}

func TestBuildMediaTags_Image(t *testing.T) {
	items := []mediaItem{{Type: "image", FileName: "photo.jpg"}}
	got := buildMediaTags(items)
	if got != "<media:image>" {
		t.Errorf("buildMediaTags(image) = %q, want <media:image>", got)
	}
}

func TestBuildMediaTags_Audio(t *testing.T) {
	items := []mediaItem{{Type: "audio", FileName: "clip.mp3"}}
	got := buildMediaTags(items)
	if got != "<media:audio>" {
		t.Errorf("buildMediaTags(audio) = %q, want <media:audio>", got)
	}
}

func TestBuildMediaTags_Document(t *testing.T) {
	items := []mediaItem{{Type: "document", FileName: "report.pdf"}}
	got := buildMediaTags(items)
	if !strings.Contains(got, "<media:document") {
		t.Errorf("buildMediaTags(document) = %q, want <media:document...>", got)
	}
	if !strings.Contains(got, "report.pdf") {
		t.Errorf("buildMediaTags(document) should include filename, got %q", got)
	}
}

func TestBuildMediaTags_FromReplyAnnotation(t *testing.T) {
	items := []mediaItem{{Type: "image", FileName: "img.jpg", FromReply: true}}
	got := buildMediaTags(items)
	if !strings.Contains(got, "(from replied message)") {
		t.Errorf("buildMediaTags with FromReply=true should add annotation, got %q", got)
	}
}

func TestBuildMediaTags_Multiple(t *testing.T) {
	items := []mediaItem{
		{Type: "image", FileName: "a.jpg"},
		{Type: "audio", FileName: "b.mp3"},
		{Type: "document", FileName: "c.pdf"},
	}
	got := buildMediaTags(items)
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 tag lines, got %d: %q", len(lines), got)
	}
}

func TestBuildMediaTags_UnknownTypeSkipped(t *testing.T) {
	items := []mediaItem{{Type: "video", FileName: "v.mp4"}}
	got := buildMediaTags(items)
	// video is not handled → no tag produced.
	if got != "" {
		t.Errorf("buildMediaTags(video) = %q, want empty (unhandled type)", got)
	}
}
