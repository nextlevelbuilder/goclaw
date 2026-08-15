package vault

import "testing"

func TestIsIncludedExtension(t *testing.T) {
	cases := []struct {
		ext     string
		include bool
		want    string
	}{
		// Text/code → note
		{".md", true, "note"},
		{".go", true, "note"},
		{".ts", true, "note"},
		{".tsx", true, "note"},
		{".py", true, "note"},
		{".sql", true, "note"},
		// Images → media
		{".png", true, "media"},
		{".jpg", true, "media"},
		{".webp", true, "media"},
		// Video → media
		{".mp4", true, "media"},
		{".mkv", true, "media"},
		// Audio → media
		{".mp3", true, "media"},
		{".wav", true, "media"},
		// Office → document
		{".pdf", true, "document"},
		{".docx", true, "document"},
		{".xlsx", true, "document"},
		{".pptx", true, "document"},
		// Binary / archive / exe → excluded
		{".exe", false, ""},
		{".bin", false, ""},
		{".dll", false, ""},
		{".zip", false, ""},
		{".gz", false, ""}, // filepath.Ext returns only last segment — ".tar.gz" → ".gz"
		{".dmg", false, ""},
		{".iso", false, ""},
		{".so", false, ""},
		{".dylib", false, ""},
		// Empty extension
		{"", false, ""},
		// Unknown
		{".xyz", false, ""},
	}
	for _, c := range cases {
		gotInclude, gotDocType := isIncludedExtension(c.ext)
		if gotInclude != c.include || gotDocType != c.want {
			t.Errorf("isIncludedExtension(%q) = (%v, %q); want (%v, %q)",
				c.ext, gotInclude, gotDocType, c.include, c.want)
		}
	}
}

func TestExtensionWhitelistMapCoverage(t *testing.T) {
	// Sanity: ensure the map has expected canonical entries.
	required := []string{".md", ".png", ".pdf", ".docx", ".mp4", ".mp3"}
	for _, ext := range required {
		if _, ok := extensionDocType[ext]; !ok {
			t.Errorf("extensionDocType missing required extension %q", ext)
		}
	}
}
