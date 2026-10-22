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

func TestExtFromMimeParametersAndCase(t *testing.T) {
	for _, tc := range []struct{ mime, want string }{
		{"IMAGE/JPEG", ".jpg"},
		{"image/png; charset=binary", ".png"},
		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document; charset=binary", ".docx"},
		{"Application/Vnd.Openxmlformats-Officedocument.Spreadsheetml.Sheet", ".xlsx"},
		{"image/png-invalid", ""},
		{"application/pdf-not-pdf", ""},
	} {
		t.Run(tc.mime, func(t *testing.T) {
			if got := ExtFromMime(tc.mime); got != tc.want {
				t.Errorf("ExtFromMime(%q) = %q, want %q", tc.mime, got, tc.want)
			}
		})
	}
}
