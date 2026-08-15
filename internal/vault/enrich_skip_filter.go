package vault

import (
	"log/slog"
	"regexp"
	"strings"
)

// Compiled patterns for meaningless filenames that should skip enrichment.
var (
	reDigitsOnly = regexp.MustCompile(`^[0-9]+$`)
	reUUID       = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	reHexHash    = regexp.MustCompile(`(?i)^[0-9a-f]{8,}$`)
	reMixedJunk  = regexp.MustCompile(`(?i)^(img|tmp|temp|dsc|screenshot|untitled|file|pic|photo|vid|clip|scan|page)[_-][0-9]+$`)
)

// shouldSkipEnrichment returns true if the basename indicates a file that
// would produce noise during enrichment (auto-generated, hash-named, etc.).
// Checks are applied to the stem (basename without extension).
// Unicode/CJK filenames pass through — they carry semantic meaning.
func shouldSkipEnrichment(basename string) bool {
	// Strip extension to get stem.
	stem := basename
	if idx := strings.LastIndex(basename, "."); idx > 0 {
		stem = basename[:idx]
	}

	switch {
	case strings.HasPrefix(stem, "goclaw_gen_"):
		slog.Debug("vault.enrich: skip_generated", "file", basename)
		return true
	case len(stem) < 3:
		slog.Debug("vault.enrich: skip_short", "file", basename)
		return true
	case reDigitsOnly.MatchString(stem):
		slog.Debug("vault.enrich: skip_digits", "file", basename)
		return true
	case reUUID.MatchString(stem):
		slog.Debug("vault.enrich: skip_uuid", "file", basename)
		return true
	case reHexHash.MatchString(stem):
		slog.Debug("vault.enrich: skip_hex_hash", "file", basename)
		return true
	case reMixedJunk.MatchString(stem):
		slog.Debug("vault.enrich: skip_mixed_junk", "file", basename)
		return true
	}
	return false
}
