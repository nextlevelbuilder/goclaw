// Package security provides input normalization and allowlist matching for
// workstation command execution. All checks operate on structured argv
// (no shell interpolation) — injection prevention is architectural, not regex-based.
package security

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

// zeroWidthChars is the set of Unicode zero-width / invisible characters
// that could be used to bypass string-equality checks without NFKC normalization.
// These are stripped AFTER NFKC normalization as an additional defense.
//
// Red-team bypass corpus:
//   - U+200B ZERO WIDTH SPACE
//   - U+200C ZERO WIDTH NON-JOINER
//   - U+200D ZERO WIDTH JOINER
//   - U+FEFF ZERO WIDTH NO-BREAK SPACE (BOM)
//   - U+00AD SOFT HYPHEN
var zeroWidthChars = map[rune]bool{
	'\u200B': true, // ZERO WIDTH SPACE
	'\u200C': true, // ZERO WIDTH NON-JOINER
	'\u200D': true, // ZERO WIDTH JOINER
	'\uFEFF': true, // ZERO WIDTH NO-BREAK SPACE (BOM)
	'\u00AD': true, // SOFT HYPHEN
}

// NormalizeCmd applies NFKC Unicode normalization to collapse lookalike characters
// (fullwidth substitutes, decomposed forms, ligatures) into canonical ASCII equivalents,
// then strips zero-width invisible characters.
//
// C2 fix: Must be called on Cmd and every Arg element before any allowlist or
// character validation. Without normalization, "echo $\u200b(whoami)" bypasses
// string-equality checks (red-team bypass #5/#6).
//
// Examples of what NFKC collapses:
//   - U+FF24 'Ｄ' (FULLWIDTH LATIN CAPITAL LETTER D) → 'D'
//   - U+00BC '¼' (VULGAR FRACTION ONE QUARTER) → "1/4"
//   - U+2126 'Ω' (OHM SIGN) → U+03A9 'Ω' (GREEK CAPITAL LETTER OMEGA)
func NormalizeCmd(s string) string {
	// Step 1: NFKC normalization — collapses fullwidth, ligatures, decomposed forms.
	s = norm.NFKC.String(s)

	// Step 2: Strip zero-width / invisible characters.
	if strings.IndexFunc(s, func(r rune) bool { return zeroWidthChars[r] }) == -1 {
		return s // fast path: no zero-width chars
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if !zeroWidthChars[r] {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// containsDangerousBytes returns true if s contains NUL (\x00), CR (\r), or LF (\n).
// These characters are blocked regardless of allowlist match status.
// NUL can corrupt log entries; CR/LF enable header-injection in networked contexts.
func containsDangerousBytes(s string) bool {
	return strings.ContainsRune(s, '\x00') ||
		strings.ContainsRune(s, '\r') ||
		strings.ContainsRune(s, '\n')
}

