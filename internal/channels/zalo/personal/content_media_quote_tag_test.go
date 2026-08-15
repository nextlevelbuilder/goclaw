package personal

import "testing"

// Without a tag in content, the later agent-side enrichment step
// (enrichImageIDs/enrichImagePaths) has nothing to attach a real file path
// to — a downloaded quoted image would be reachable only via vision, with
// no way for the model to reference it for forwarding. buildQuoteMediaTag
// closes that gap by rendering the same bare <media:image> placeholder a
// direct (non-quoted) attachment gets.
func TestBuildQuoteMediaTag(t *testing.T) {
	tests := []struct {
		name  string
		paths []string
		want  string
	}{
		{"no paths", nil, ""},
		{"single image", []string{"/tmp/goclaw_zca_abc123.jpg"}, "<media:image>"},
		{"multiple images", []string{"/tmp/a.jpg", "/tmp/b.png"}, "<media:image>\n<media:image>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildQuoteMediaTag(tt.paths); got != tt.want {
				t.Errorf("buildQuoteMediaTag(%v) = %q, want %q", tt.paths, got, tt.want)
			}
		})
	}
}
