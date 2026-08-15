package media

import "testing"

func TestExtFromMimeArchiveTypes(t *testing.T) {
	tests := []struct {
		mime string
		want string
	}{
		{mime: "application/zip", want: ".zip"},
		{mime: "application/x-zip-compressed", want: ".zip"},
		{mime: "application/x-tar", want: ".tar"},
		{mime: "application/gzip", want: ".gz"},
	}

	for _, tt := range tests {
		t.Run(tt.mime, func(t *testing.T) {
			if got := ExtFromMime(tt.mime); got != tt.want {
				t.Fatalf("ExtFromMime(%q) = %q, want %q", tt.mime, got, tt.want)
			}
		})
	}
}
