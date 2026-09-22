package common

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// MaxSlugLen mirrors the RFC-1035-ish 63-char DNS label limit so the slug
// is safe to embed in any future host or path segment.
const MaxSlugLen = 63

// ReservedSlugs are URL paths the gateway may want for operational endpoints.
// Reject these at registration to keep the namespace open for future use.
var ReservedSlugs = map[string]struct{}{
	"zalo":     {},
	"webhook":  {},
	"_health":  {},
	"_metrics": {},
}

var (
	// Both ends alphanumeric so validator matches what DeriveSlugFromName trims.
	slugRE          = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,61}[a-z0-9]$`)
	nonAlphanumRE   = regexp.MustCompile(`[^a-z0-9]+`)
	collapseHyphens = regexp.MustCompile(`-+`)
)

// ErrSlugInvalid is returned by ValidateSlug for any failed check.
var ErrSlugInvalid = errors.New("zalo_common: invalid slug")

// ValidateSlug enforces ^[a-z0-9][a-z0-9-]{1,62}$ and rejects ReservedSlugs.
func ValidateSlug(s string) error {
	if s == "" {
		return fmt.Errorf("%w: empty", ErrSlugInvalid)
	}
	if len(s) > MaxSlugLen {
		return fmt.Errorf("%w: %d chars exceeds max %d", ErrSlugInvalid, len(s), MaxSlugLen)
	}
	if !slugRE.MatchString(s) {
		return fmt.Errorf("%w: %q must match ^[a-z0-9][a-z0-9-]{1,62}$", ErrSlugInvalid, s)
	}
	if _, reserved := ReservedSlugs[s]; reserved {
		return fmt.Errorf("%w: %q is reserved", ErrSlugInvalid, s)
	}
	return nil
}

// DeriveSlugFromName produces a stable URL-safe slug from a channel name.
// Lowercase, replace runs of non-alphanumerics with single hyphen,
// trim leading/trailing hyphens, clamp to MaxSlugLen.
func DeriveSlugFromName(name string) string {
	s := strings.ToLower(name)
	s = nonAlphanumRE.ReplaceAllString(s, "-")
	s = collapseHyphens.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > MaxSlugLen {
		s = strings.TrimRight(s[:MaxSlugLen], "-")
	}
	return s
}
