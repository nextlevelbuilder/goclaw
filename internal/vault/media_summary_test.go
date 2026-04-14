package vault

import (
	"strings"
	"testing"
)

func TestSynthesizeMediaSummary(t *testing.T) {
	cases := []struct {
		name        string
		path        string
		mime        string
		wantSubstrs []string
	}{
		{
			name:        "image with semantic tokens",
			path:        "photos/vacation/cat-on-beach.png",
			mime:        "image/png",
			wantSubstrs: []string{"Image", "cat", "on", "beach", "photos/vacation"},
		},
		{
			name:        "pdf with version",
			path:        "docs/specs/api-v2.pdf",
			mime:        "application/pdf",
			wantSubstrs: []string{"Document", "api", "v2", "docs/specs"},
		},
		{
			name:        "image with camera prefix no semantic tokens",
			path:        "photos/IMG_20240101_001.jpg",
			mime:        "image/jpeg",
			wantSubstrs: []string{"Image", "photos"},
		},
		{
			name:        "root file no parent dir",
			path:        "standalone.md",
			mime:        "text/markdown",
			wantSubstrs: []string{"standalone"},
		},
		{
			name:        "office doc docx",
			path:        "reports/quarterly-review.docx",
			mime:        "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			wantSubstrs: []string{"Document", "quarterly", "review", "reports"},
		},
		{
			name:        "audio file",
			path:        "audio/meeting-notes.mp3",
			mime:        "audio/mpeg",
			wantSubstrs: []string{"Audio", "meeting", "notes"},
		},
		{
			name:        "video file",
			path:        "videos/demo-clip.mp4",
			mime:        "video/mp4",
			wantSubstrs: []string{"Video", "demo", "clip"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SynthesizeMediaSummary(c.path, c.mime)
			if got == "" {
				t.Fatalf("empty summary for %s", c.path)
			}
			if len(got) > mediaSummaryMaxLen {
				t.Errorf("summary too long: %d > %d", len(got), mediaSummaryMaxLen)
			}
			for _, sub := range c.wantSubstrs {
				if !strings.Contains(got, sub) {
					t.Errorf("summary %q missing substring %q", got, sub)
				}
			}
		})
	}
}

func TestSynthesizeMediaSummary_Deterministic(t *testing.T) {
	// Pure function invariant: same input → same output across calls.
	path := "photos/vacation/cat-on-beach.png"
	mime := "image/png"
	a := SynthesizeMediaSummary(path, mime)
	b := SynthesizeMediaSummary(path, mime)
	if a != b {
		t.Errorf("non-deterministic: %q vs %q", a, b)
	}
}

func TestSynthesizeMediaSummary_EmptyMimeFallsBackToExt(t *testing.T) {
	// No mime → must infer category from extension whitelist.
	got := SynthesizeMediaSummary("photos/cat.png", "")
	if !strings.Contains(got, "Image") {
		t.Errorf("expected Image label for .png with empty mime, got %q", got)
	}
	got = SynthesizeMediaSummary("docs/manual.pdf", "")
	if !strings.Contains(got, "Document") {
		t.Errorf("expected Document label for .pdf with empty mime, got %q", got)
	}
}
