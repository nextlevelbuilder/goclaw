package personal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Zalo CDN URLs often carry no usable file extension, so downloaded images
// land as ".bin". Downstream media classification infers MIME purely from
// the file extension, so a ".bin" image is silently treated as a document
// and never reaches the model's vision input. fixDownloadedExt must rename
// it based on sniffed content, regardless of the URL-derived extension.
func TestFixDownloadedExtRenamesSniffedImage(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "goclaw_zca_test.bin")
	png := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 64)...)
	if err := os.WriteFile(p, png, 0644); err != nil {
		t.Fatal(err)
	}
	got := fixDownloadedExt(p)
	if !strings.HasSuffix(got, ".png") {
		t.Fatalf("fixDownloadedExt(%q) = %q, want .png suffix", p, got)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("renamed file missing: %v", err)
	}
}

// Non-image content (documents, etc.) must be left untouched — this fix is
// scoped to unblocking vision input, not reclassifying arbitrary files.
func TestFixDownloadedExtLeavesNonImage(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "goclaw_zca_doc.bin")
	if err := os.WriteFile(p, []byte("%PDF-1.4 not an image"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := fixDownloadedExt(p); got != p {
		t.Fatalf("fixDownloadedExt(%q) = %q, want unchanged", p, got)
	}
}

// A file whose extension already matches an image type must not be touched,
// even though sniffing would also say "image" — avoids pointless renames.
func TestFixDownloadedExtLeavesCorrectExtension(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "goclaw_zca_test.jpg")
	jpg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 64)...)
	if err := os.WriteFile(p, jpg, 0644); err != nil {
		t.Fatal(err)
	}
	if got := fixDownloadedExt(p); got != p {
		t.Fatalf("fixDownloadedExt(%q) = %q, want unchanged (already .jpg)", p, got)
	}
}
