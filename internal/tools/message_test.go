package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMediaPath(t *testing.T) {
	tmpDir := os.TempDir()

	tests := []struct {
		name   string
		input  string
		want   string
		wantOK bool
	}{
		{"valid temp file", "MEDIA:" + filepath.Join(tmpDir, "test.png"), filepath.Join(tmpDir, "test.png"), true},
		{"valid nested temp file", "MEDIA:" + filepath.Join(tmpDir, "sub", "file.txt"), filepath.Join(tmpDir, "sub", "file.txt"), true},
		{"with spaces around prefix", "  MEDIA:" + filepath.Join(tmpDir, "test.png") + "  ", filepath.Join(tmpDir, "test.png"), true},
		{"no prefix", filepath.Join(tmpDir, "test.png"), "", false},
		{"empty after prefix", "MEDIA:", "", false},
		{"relative path", "MEDIA:relative/path.txt", "", false},
		{"dot path", "MEDIA:.", "", false},
		{"outside temp dir", "MEDIA:/etc/passwd", "", false},
		{"traversal attack", "MEDIA:" + filepath.Join(tmpDir, "..", "etc", "passwd"), "", false},
		{"home directory", "MEDIA:" + filepath.Join(os.Getenv("HOME"), "secret.txt"), "", false},
		{"empty string", "", "", false},
		{"just MEDIA", "MEDIA", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseMediaPath(tt.input)
			if ok != tt.wantOK {
				t.Errorf("parseMediaPath(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("parseMediaPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
