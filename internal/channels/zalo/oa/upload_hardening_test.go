package oa

import (
	"context"
	"strings"
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want func(string) bool // matcher
	}{
		{"plain", "report.pdf", func(s string) bool { return s == "report.pdf" }},
		{"strip path", "/etc/passwd", func(s string) bool { return s == "passwd" }},
		{"trim spaces", "  doc.txt  ", func(s string) bool { return s == "doc.txt" }},
		{"dot only", ".", func(s string) bool { return strings.HasPrefix(s, "file-") && strings.HasSuffix(s, ".bin") }},
		{"double dot", "..", func(s string) bool { return strings.HasPrefix(s, "file-") && strings.HasSuffix(s, ".bin") }},
		{"empty", "", func(s string) bool { return strings.HasPrefix(s, "file-") && strings.HasSuffix(s, ".bin") }},
		{"path traversal", "../../etc/passwd", func(s string) bool { return s == "passwd" }},
		{"long name capped", strings.Repeat("a", 300) + ".pdf", func(s string) bool { return len(s) <= 200 }},
		{"unicode preserved", "báo cáo.pdf", func(s string) bool { return s == "báo cáo.pdf" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeFilename(tc.in)
			if !tc.want(got) {
				t.Errorf("sanitizeFilename(%q) = %q, predicate failed", tc.in, got)
			}
		})
	}
}

func TestExtFromURL_AcceptsAnySafeExt(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"https://cdn.example/foo.jpg", ".jpg"},
		{"https://cdn.example/foo.JPEG", ".jpeg"},
		{"https://cdn.example/foo.pdf?token=abc", ".pdf"},
		{"https://cdn.example/foo.docx", ".docx"},
		{"https://cdn.example/foo.mp4", ".mp4"},
		{"https://cdn.example/foo.m4a", ".m4a"},
		{"https://cdn.example/foo.zip", ".zip"},
		{"https://cdn.example/foo.webp", ".webp"},
		{"https://cdn.example/foo", ".bin"},
		{"https://cdn.example/foo.weirdest", ".bin"},
		{"https://cdn.example/foo.sh-bad", ".bin"},
		{"https://cdn.example/foo.x.y", ".y"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := extFromURL(tc.in); got != tc.want {
				t.Errorf("extFromURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSendFile_RejectsZeroBytes(t *testing.T) {
	t.Parallel()
	api, captured, _ := newAPIServer(t, apiServerOpts{})
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, api, refresh, &fakeStore{})

	_, err := c.SendFile(context.Background(), "u1", []byte{}, "empty.txt")
	if err == nil {
		t.Fatal("expected error for zero-byte file")
	}
	if !strings.Contains(err.Error(), "empty") && !strings.Contains(err.Error(), "zero") {
		t.Errorf("err = %v, want 'empty/zero' message", err)
	}
	if len(*captured) != 0 {
		t.Errorf("captured %d HTTP calls; expected 0 (rejected before upload)", len(*captured))
	}
}

