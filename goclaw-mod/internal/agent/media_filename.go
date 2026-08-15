// Package agent - media_filename.go provides pure helpers to derive a safe,
// human-readable disk filename from a user-provided original filename, and to
// generate a short random id used as a collision-avoidance suffix.
//
// Disk naming scheme (used by persistMedia):
//
//	{sanitizeFilename(stem)}-{shortID(8)}{ext}
//
// Callers MUST fall back to a UUID name when sanitizeFilename returns "".
// This keeps backward compatibility for voice notes, clipboard paste, and
// tool-generated media that arrive without an original filename.
package agent

import (
	"crypto/rand"
	"encoding/hex"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// slugRe collapses any run of non-ASCII-lowercase-alphanumeric chars into "-".
// Applied on the Latin-dominant branch after normalization + lower-casing.
var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// unsafeRe matches filesystem-unsafe characters that must be stripped from
// non-Latin filenames (CJK, Arabic, etc.) where we want to preserve the original
// script but still guarantee a safe on-disk name.
var unsafeRe = regexp.MustCompile(`[\x00-\x1f/\\:*?"<>|]`)

// viMap handles Vietnamese-specific letters that NFD combining-mark stripping
// does not decompose (đ/Đ are precomposed in Unicode without a base+mark form).
// Applied BEFORE NFD normalization on the Latin branch.
var viMap = map[rune]rune{
	'đ': 'd', 'Đ': 'd',
}

// maxFilenameRunes caps the stem at 60 runes to keep on-disk names short and
// avoid platform path-length issues. Rune-based (not byte-based) to stay safe
// for multi-byte scripts.
const maxFilenameRunes = 60

// isDominantLatin returns true when more than 50% of the runes are Latin
// letters or ASCII digits. This distinguishes "Báo cáo Q4" (Latin-with-accents,
// slug it) from "猫の写真" (CJK, preserve runes).
func isDominantLatin(runes []rune) bool {
	if len(runes) == 0 {
		return false
	}
	latin := 0
	for _, r := range runes {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			latin++
			continue
		}
		// Latin-1 Supplement + Latin Extended ranges cover European accents.
		if unicode.Is(unicode.Latin, r) {
			latin++
		}
	}
	return latin*2 > len(runes)
}

// sanitizeFilename derives a safe stem (no extension) from a user-provided
// filename. Returns "" when the input is empty or sanitizes to nothing — the
// caller must then fall back to a UUID-only name.
//
// Guarantees on the return value:
//   - never contains "/", "\\", "..", or ASCII control chars
//   - never starts or ends with "-"
//   - len([]rune(out)) <= maxFilenameRunes
//
// Branching:
//   - Latin-dominant input is accent-stripped, lowercased, and slugified.
//   - Non-Latin-dominant input (CJK, Arabic, etc.) keeps its original runes
//     with only filesystem-unsafe characters removed.
func sanitizeFilename(orig string) string {
	orig = strings.TrimSpace(orig)
	if orig == "" {
		return ""
	}

	// Strip the extension — caller re-appends the canonical ext from MIME.
	ext := filepath.Ext(orig)
	stem := strings.TrimSuffix(orig, ext)

	// Defense-in-depth: strip any path components and reject traversal markers.
	stem = filepath.Base(stem)
	if stem == "." || stem == ".." || strings.Contains(stem, "..") {
		return ""
	}
	// Dotfiles with no real stem (e.g. ".env") have empty pre-ext content —
	// filepath.Ext(".env") returns ".env", so stem == "". Already handled below.
	stem = strings.TrimSpace(stem)
	if stem == "" {
		return ""
	}

	runes := []rune(stem)
	if isDominantLatin(runes) {
		stem = latinSlug(runes)
	} else {
		stem = nonLatinClean(runes)
	}

	// Cap to maxFilenameRunes (rune-based).
	capped := []rune(stem)
	if len(capped) > maxFilenameRunes {
		capped = capped[:maxFilenameRunes]
	}
	out := strings.Trim(string(capped), "-")
	return out
}

// latinSlug applies Vietnamese mapping, NFD decomposition + combining-mark
// stripping, lower-casing, and the slugRe replacement to produce an
// ASCII-only kebab-case stem.
func latinSlug(runes []rune) string {
	// Apply Vietnamese đ/Đ map pre-NFD (NFD does not decompose them).
	mapped := make([]rune, 0, len(runes))
	for _, r := range runes {
		if m, ok := viMap[r]; ok {
			mapped = append(mapped, m)
			continue
		}
		mapped = append(mapped, r)
	}
	// NFD: base letter + combining marks → drop marks (unicode.Mn).
	decomposed := norm.NFD.String(string(mapped))
	var b strings.Builder
	b.Grow(len(decomposed))
	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	lowered := strings.ToLower(b.String())
	// Replace non-[a-z0-9] runs with "-", then collapse/trim.
	slug := slugRe.ReplaceAllString(lowered, "-")
	return strings.Trim(slug, "-")
}

// nonLatinClean preserves the script of non-Latin inputs while removing
// control chars and filesystem-unsafe characters. Whitespace is collapsed to
// a single space (kept, since CJK users expect spaces preserved).
func nonLatinClean(runes []rune) string {
	s := string(runes)
	s = unsafeRe.ReplaceAllString(s, "")
	// Strip any remaining control runes not caught by unsafeRe.
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// shortID returns n lowercase hex chars sourced from crypto/rand. n must be
// even and > 0; otherwise shortID panics (caller bug — always called with 8).
func shortID(n int) string {
	if n <= 0 || n%2 != 0 {
		panic("shortID: n must be a positive even number")
	}
	b := make([]byte, n/2)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read on modern Go never fails in practice; panic signals
		// a catastrophic OS-level issue worth surfacing.
		panic("shortID: crypto/rand failure: " + err.Error())
	}
	return hex.EncodeToString(b)
}
