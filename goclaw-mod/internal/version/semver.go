// Package version provides shared semantic version comparison utilities.
package version

import (
	"strconv"
	"strings"
)

// IsNewer returns true if version a is newer than b.
// Both versions may include "v" prefix and pre-release suffixes (stripped before comparison).
// Returns false if either version is "dev" or empty.
func IsNewer(a, b string) bool {
	a = strings.TrimPrefix(a, "v")
	b = strings.TrimPrefix(b, "v")
	if a == "dev" || a == "" || b == "dev" || b == "" {
		return false
	}
	return Compare(a, b) > 0
}

// Compare compares two semver strings. Returns >0 if a > b, <0 if a < b, 0 if equal.
// Strips "v" prefix and pre-release suffixes before comparison.
func Compare(a, b string) int {
	pa := Parse(a)
	pb := Parse(b)
	for i := range 3 {
		if pa[i] != pb[i] {
			return pa[i] - pb[i]
		}
	}
	return 0
}

// Parse extracts [major, minor, patch] from a semver string.
// Strips "v" prefix and pre-release suffixes (e.g. "v1.2.3-5-gabcdef" → [1, 2, 3]).
func Parse(s string) [3]int {
	s = strings.TrimPrefix(s, "v")
	// Strip pre-release suffix: "1.2.3-rc1" → "1.2.3"
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		s = s[:idx]
	}
	var parts [3]int
	for i, p := range strings.SplitN(s, ".", 3) {
		parts[i], _ = strconv.Atoi(p)
	}
	return parts
}
