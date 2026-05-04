package i18n

import (
	"os"
	"strings"
	"sync"
)

// Default returns the system-wide default locale, used wherever a per-request
// locale is unavailable (cron jobs, heartbeat ticks, internal background
// processing). Resolution order:
//
//  1. GOCLAW_DEFAULT_LOCALE — explicit override for operators who want a
//     specific language regardless of the host environment.
//  2. POSIX system locale (LC_ALL > LC_MESSAGES > LANG) — picked up
//     automatically on Linux hosts where the operator already configured
//     the desktop / shell language.
//  3. DefaultLocale (compile-time fallback, currently "en").
//
// The result is cached on first resolution because env vars are not expected
// to change at runtime; tests can clear via ResetDefaultForTest().
func Default() string {
	defaultLocaleOnce.Do(func() { cachedDefault = resolveDefault() })
	return cachedDefault
}

var (
	defaultLocaleOnce sync.Once
	cachedDefault     string
)

// ResetDefaultForTest clears the cached default so tests can re-run resolution
// after manipulating env vars. Not exported via package docs deliberately —
// this is a test-only escape hatch.
func ResetDefaultForTest() {
	defaultLocaleOnce = sync.Once{}
	cachedDefault = ""
}

func resolveDefault() string {
	// 1. Explicit operator override. Skip Normalize() because it lossily
	// returns DefaultLocale for any unrecognised value — we want unrecognised
	// values to fall through to the next layer rather than short-circuit to "en".
	if v := os.Getenv("GOCLAW_DEFAULT_LOCALE"); v != "" {
		v = strings.ToLower(strings.TrimSpace(v))
		if IsSupported(v) {
			return v
		}
		// Try BCP-47 short-form like "en-US" → "en".
		if len(v) >= 2 && IsSupported(v[:2]) {
			return v[:2]
		}
	}
	// 2. POSIX-style host locale.
	for _, env := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if loc := normalizePOSIXLocale(os.Getenv(env)); loc != "" {
			return loc
		}
	}
	// 3. Compile-time fallback.
	return DefaultLocale
}

// normalizePOSIXLocale extracts a supported locale code from a POSIX
// LANG-style string. Examples:
//
//	"ko_KR.UTF-8" → "ko"
//	"en_US"       → "en"
//	"C" / "POSIX" → ""  (untranslatable)
//	""            → ""
func normalizePOSIXLocale(v string) string {
	if v == "" || v == "C" || v == "POSIX" {
		return ""
	}
	// Strip encoding suffix: ko_KR.UTF-8 → ko_KR
	if idx := strings.IndexByte(v, '.'); idx > 0 {
		v = v[:idx]
	}
	// Strip modifier: ko_KR@something → ko_KR
	if idx := strings.IndexByte(v, '@'); idx > 0 {
		v = v[:idx]
	}
	// Strip region: ko_KR → ko (Normalize accepts both forms but the bare
	// language code is what our catalog keys on).
	if idx := strings.IndexByte(v, '_'); idx > 0 {
		v = v[:idx]
	}
	v = strings.ToLower(v)
	if !IsSupported(v) {
		return ""
	}
	return v
}
