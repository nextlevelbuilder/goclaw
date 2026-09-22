package i18n

import (
	"strings"
	"testing"
)

func TestI18nKey_ZaloOA_AllCatalogs(t *testing.T) {
	keys := []string{
		MsgZaloOAUnsupportedAttachment,
		MsgZaloOAInvalidChannelType,
		MsgZaloOAMissingAppID,
		MsgZaloOARedirectURIRequired,
		MsgZaloOAStateGenFailed,
		MsgZaloOAInvalidState,
		MsgZaloOACodeExchangeFailed,
		MsgZaloOAConnected,
		MsgZaloOACallbackUnavailable,
		MsgZaloOAOAIDMismatch,
	}
	for _, locale := range []string{LocaleEN, LocaleVI, LocaleZH} {
		for _, key := range keys {
			got := T(locale, key)
			if strings.TrimSpace(got) == "" || got == key {
				t.Errorf("locale %q key %q missing translation", locale, key)
			}
		}
	}
}
