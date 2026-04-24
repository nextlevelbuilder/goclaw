package i18n

import "testing"

// TestI18nKey_TtsGeminiTextOnly_AllCatalogs verifies that MsgTtsGeminiTextOnly
// is present in all three locale catalogs and returns a translated string
// (not the key literal itself).
func TestI18nKey_TtsGeminiTextOnly_AllCatalogs(t *testing.T) {
	locales := []string{LocaleEN, LocaleVI, LocaleZH}
	for _, locale := range locales {
		t.Run(locale, func(t *testing.T) {
			got := T(locale, MsgTtsGeminiTextOnly)
			if got == "" {
				t.Errorf("locale %q: T returned empty string for MsgTtsGeminiTextOnly", locale)
			}
			if got == MsgTtsGeminiTextOnly {
				t.Errorf("locale %q: T returned key literal %q — key missing from catalog", locale, MsgTtsGeminiTextOnly)
			}
		})
	}
}
