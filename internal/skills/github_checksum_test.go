package skills

import (
	"errors"
	"testing"
)

func TestParseChecksums(t *testing.T) {
	input := []byte(`# comment
abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890  lazygit_Linux_x86_64.tar.gz
1111111111111111111111111111111111111111111111111111111111111111 *lazygit_Linux_arm64.tar.gz
not a valid line at all
`)
	m, err := ParseChecksums(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 2 {
		t.Fatalf("want 2 entries, got %d: %+v", len(m), m)
	}
	if m["lazygit_Linux_x86_64.tar.gz"] == "" {
		t.Error("missing amd64 entry")
	}
	if m["lazygit_Linux_arm64.tar.gz"] == "" {
		t.Error("missing arm64 entry (binary prefix *)")
	}
}

func TestParseChecksums_Empty(t *testing.T) {
	if _, err := ParseChecksums([]byte("")); err == nil {
		t.Error("empty file should error")
	}
}

func TestVerifyChecksum(t *testing.T) {
	if err := VerifyChecksum("aBc", "abc"); err != nil {
		t.Errorf("case-insensitive match failed: %v", err)
	}
	err := VerifyChecksum("abc", "xyz")
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Errorf("want ErrChecksumMismatch, got %v", err)
	}
	if err := VerifyChecksum("", "abc"); err == nil {
		t.Error("empty expected should fail")
	}
}

func TestFindChecksumAsset(t *testing.T) {
	rel := &GitHubRelease{
		Assets: []GitHubAsset{
			{Name: "binary.tar.gz"},
			{Name: "binary.tar.gz.sha256"},
			{Name: "checksums.txt"},
		},
	}
	a := FindChecksumAsset(rel, "binary.tar.gz")
	if a == nil || a.Name != "binary.tar.gz.sha256" {
		t.Errorf("should prefer per-asset .sha256, got %v", a)
	}

	rel2 := &GitHubRelease{
		Assets: []GitHubAsset{
			{Name: "binary.tar.gz"},
			{Name: "SHA256SUMS"},
		},
	}
	a = FindChecksumAsset(rel2, "binary.tar.gz")
	if a == nil || a.Name != "SHA256SUMS" {
		t.Errorf("should fall back to SHA256SUMS, got %v", a)
	}

	rel3 := &GitHubRelease{Assets: []GitHubAsset{{Name: "binary.tar.gz"}}}
	if FindChecksumAsset(rel3, "binary.tar.gz") != nil {
		t.Error("no checksum asset should return nil")
	}
}
