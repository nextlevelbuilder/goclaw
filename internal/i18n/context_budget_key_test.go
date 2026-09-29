package i18n

import (
	"strings"
	"testing"
)

func TestI18nCatalogs_HasContextBudgetExceeded(t *testing.T) {
	for _, locale := range []string{LocaleEN, LocaleVI, LocaleZH, LocaleRU, LocaleKO} {
		msg := lookup(locale, MsgContextBudgetExceeded)
		if msg == MsgContextBudgetExceeded || strings.TrimSpace(msg) == "" {
			t.Errorf("locale=%s: translation missing for %s", locale, MsgContextBudgetExceeded)
		}
	}
}
