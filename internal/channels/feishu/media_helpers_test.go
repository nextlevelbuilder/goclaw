package feishu

import (
	"testing"
)

// --- isImageContentType ---

func TestIsImageContentType(t *testing.T) {
	cases := []struct {
		ct   string
		want bool
	}{
		{"image/png", true},
		{"image/jpeg", true},
		{"image/gif", true},
		{"image/webp", true},
		{"image", true},
		{"video/mp4", false},
		{"audio/ogg", false},
		{"application/pdf", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.ct, func(t *testing.T) {
			got := isImageContentType(tc.ct)
			if got != tc.want {
				t.Errorf("isImageContentType(%q) = %v, want %v", tc.ct, got, tc.want)
			}
		})
	}
}

// --- detectFileType ---

func TestDetectFileType(t *testing.T) {
	cases := []struct {
		fileName string
		want     string
	}{
		{"voice.opus", "opus"},
		{"audio.ogg", "opus"},
		{"video.mp4", "mp4"},
		{"clip.mov", "mp4"},
		{"movie.avi", "mp4"},
		{"screen.wmv", "mp4"},
		{"film.mkv", "mp4"},
		{"doc.pdf", "pdf"},
		{"letter.doc", "doc"},
		{"report.docx", "doc"},
		{"data.xls", "xls"},
		{"data.xlsx", "xls"},
		{"slides.ppt", "ppt"},
		{"slides.pptx", "ppt"},
		{"unknown.zip", "stream"},
		{"noext", "stream"},
		{"", "stream"},
		{"UPPER.PDF", "pdf"},  // case-insensitive via ToLower on ext
		{"Mixed.Opus", "opus"},
	}
	for _, tc := range cases {
		t.Run(tc.fileName, func(t *testing.T) {
			got := detectFileType(tc.fileName)
			if got != tc.want {
				t.Errorf("detectFileType(%q) = %q, want %q", tc.fileName, got, tc.want)
			}
		})
	}
}
