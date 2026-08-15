package whatsapp

import (
	"errors"
	"testing"
)

// --- mimeToExt ---

func TestMimeToExt(t *testing.T) {
	tests := []struct {
		mime string
		want string
	}{
		{"image/jpeg", ".jpg"},
		{"image/jpeg; charset=utf-8", ".jpg"}, // HasPrefix match
		{"image/png", ".png"},
		{"image/webp", ".webp"},
		{"video/mp4", ".mp4"},
		{"audio/ogg", ".ogg"},
		{"audio/ogg; codecs=opus", ".ogg"},
		{"audio/mpeg", ".mp3"},
		{"application/pdf", ".pdf"},
		{"application/octet-stream", ".bin"},
		{"text/plain", ".bin"},
		{"", ".bin"},
		{"image/gif", ".bin"}, // not in the switch → default
	}

	for _, tt := range tests {
		t.Run(tt.mime, func(t *testing.T) {
			got := mimeToExt(tt.mime)
			if got != tt.want {
				t.Errorf("mimeToExt(%q) = %q, want %q", tt.mime, got, tt.want)
			}
		})
	}
}

// --- classifyDownloadError ---

func TestClassifyDownloadError(t *testing.T) {
	tests := []struct {
		name    string
		errMsg  string
		want    string
	}{
		{"timeout", "connection timeout exceeded", "timeout"},
		{"deadline exceeded", "context deadline exceeded", "timeout"},
		{"decrypt error", "failed to decrypt media", "decrypt_error"},
		{"cipher error", "cipher: message authentication failed", "decrypt_error"},
		{"404 not found", "server returned 404", "expired"},
		{"not found text", "media not found on server", "expired"},
		{"unsupported type", "unsupported media type", "unsupported"},
		{"unknown generic error", "connection reset by peer", "unknown"},
		{"empty error", "", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := errors.New(tt.errMsg)
			got := classifyDownloadError(err)
			if got != tt.want {
				t.Errorf("classifyDownloadError(%q) = %q, want %q", tt.errMsg, got, tt.want)
			}
		})
	}
}

// --- scheduleMediaCleanup: no-op with empty paths ---

func TestScheduleMediaCleanup_EmptyPaths(t *testing.T) {
	// Should not panic with empty paths.
	scheduleMediaCleanup(nil, 0)
	scheduleMediaCleanup([]string{}, 0)
}
