// Package i18n provides a lightweight message catalog for localizing
// user-facing error messages. Messages use fmt.Sprintf-style templates.
package i18n

import "maps"

import "fmt"

// Supported locales.
const (
	LocaleEN = "en"
	LocaleVI = "vi"
	LocaleZH = "zh"
	LocaleKO = "ko"

	DefaultLocale = LocaleEN
)

// catalogs maps locale → key → message template.
var catalogs = map[string]map[string]string{}

// register adds a set of messages for a locale. Called from catalog_*.go init().
func register(locale string, msgs map[string]string) {
	if catalogs[locale] == nil {
		catalogs[locale] = make(map[string]string, len(msgs))
	}
	maps.Copy(catalogs[locale], msgs)
}

// T returns a localized message for the given key.
// If the key is not found in the requested locale, it falls back to English.
// If the key is not found in English either, the key itself is returned.
// Optional args are applied via fmt.Sprintf if the template contains verbs.
func T(locale, key string, args ...any) string {
	msg := lookup(locale, key)
	if len(args) > 0 {
		return fmt.Sprintf(msg, args...)
	}
	return msg
}

func lookup(locale, key string) string {
	if cat, ok := catalogs[locale]; ok {
		if msg, ok := cat[key]; ok {
			return msg
		}
	}
	// Fallback to English
	if locale != LocaleEN {
		if cat, ok := catalogs[LocaleEN]; ok {
			if msg, ok := cat[key]; ok {
				return msg
			}
		}
	}
	return key
}

// IsSupported returns true if the locale is a known language.
func IsSupported(locale string) bool {
	switch locale {
	case LocaleEN, LocaleVI, LocaleZH, LocaleKO:
		return true
	}
	return false
}

// Normalize returns a supported locale or the default.
func Normalize(locale string) string {
	if IsSupported(locale) {
		return locale
	}
	// Handle common prefixes: "en-US" → "en", "vi-VN" → "vi", "zh-CN" → "zh"
	if len(locale) >= 2 {
		prefix := locale[:2]
		if IsSupported(prefix) {
			return prefix
		}
	}
	return DefaultLocale
}
