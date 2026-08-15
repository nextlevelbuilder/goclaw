package skills

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrChecksumMismatch is returned when a computed SHA256 doesn't match the expected value.
var ErrChecksumMismatch = errors.New("github: checksum mismatch")

// checksumLineRE parses `<sha256>  <filename>` or `<sha256> *<filename>` lines.
var checksumLineRE = regexp.MustCompile(`(?m)^([a-fA-F0-9]{64})[ \t]+\*?(\S.*)$`)

// FindChecksumAsset scans a release's assets looking for a checksum file.
// Returns nil if none found.
// Lookup order:
//  1. <assetName>.sha256
//  2. checksums.txt
//  3. SHA256SUMS / SHA256SUMS.txt
//  4. *_checksums.txt (pattern match)
//  5. *.sha256sums
func FindChecksumAsset(release *GitHubRelease, assetName string) *GitHubAsset {
	if release == nil {
		return nil
	}
	targetPerAsset := strings.ToLower(assetName + ".sha256")
	byLower := func(n string) string { return strings.ToLower(n) }

	// Pass 1: exact per-asset .sha256 companion.
	for i := range release.Assets {
		if byLower(release.Assets[i].Name) == targetPerAsset {
			return &release.Assets[i]
		}
	}
	// Pass 2: common aggregate names.
	priorities := []string{"checksums.txt", "sha256sums", "sha256sums.txt", "sha256sum.txt"}
	for _, p := range priorities {
		for i := range release.Assets {
			if byLower(release.Assets[i].Name) == p {
				return &release.Assets[i]
			}
		}
	}
	// Pass 3: fuzzier match — trailing `_checksums.txt` / `-checksums.txt` / `.sha256sums`.
	for i := range release.Assets {
		n := byLower(release.Assets[i].Name)
		if strings.HasSuffix(n, "_checksums.txt") || strings.HasSuffix(n, "-checksums.txt") {
			return &release.Assets[i]
		}
		if strings.HasSuffix(n, ".sha256sums") {
			return &release.Assets[i]
		}
	}
	return nil
}

// ParseChecksums parses a checksums.txt-style file into a map of filename → SHA256.
// Accepts both `<sha>  <name>` (two spaces) and `<sha> *<name>` (binary prefix).
// Unrecognized lines are ignored (comments, empty lines).
func ParseChecksums(content []byte) (map[string]string, error) {
	matches := checksumLineRE.FindAllSubmatch(content, -1)
	out := make(map[string]string, len(matches))
	for _, m := range matches {
		sha := strings.ToLower(string(m[1]))
		name := strings.TrimSpace(string(m[2]))
		// `sha256sum ./file` emits `./file` in the name column. Strip the
		// leading `./` so the lookup in the caller (keyed by bare asset
		// basename) matches. Real release checksums almost never use this
		// form, but the guard is defensive and essentially free.
		name = strings.TrimPrefix(name, "./")
		// In "checksums.txt" the name may be just a basename. Keep as-is;
		// caller looks up by asset name which matches basename.
		out[name] = sha
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid checksum entries found")
	}
	return out, nil
}

// VerifyChecksum does a constant-time comparison between expected and actual hex strings.
func VerifyChecksum(expected, actual string) error {
	e := strings.ToLower(strings.TrimSpace(expected))
	a := strings.ToLower(strings.TrimSpace(actual))
	if len(e) == 0 || len(a) == 0 {
		return ErrChecksumMismatch
	}
	if subtle.ConstantTimeCompare([]byte(e), []byte(a)) != 1 {
		return fmt.Errorf("%w (expected %s, got %s)", ErrChecksumMismatch, e, a)
	}
	return nil
}
