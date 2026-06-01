// Phase 5: catalog parity for all Git credential keys.
//
// Why a dedicated test: a new git i18n key added to keys.go without
// translations in all three catalogs would silently fall back to the key
// string in production for vi/zh users. This test enumerates every
// MsgGitCred* constant via the registered catalogs and asserts each locale
// returns a non-key, non-empty message.
package i18n

import (
	"strings"
	"testing"
)

// gitKeys lists every git credential key. Kept hand-curated (one line each)
// rather than reflect-walked because the constants are in a single block in
// keys.go and adding a new one is a one-line change here too.
var gitKeys = []string{
	MsgGitCredHostMismatch,
	MsgGitCredNoMatch,
	MsgGitCredUnsupportedType,
	MsgGitCredTokenInvalid,
	MsgGitCredTokenControlChar,
	MsgGitCredHostUserinfoRejected,
	MsgGitCredSSHPassphraseUnsupported,
	MsgGitCredSSHKeyInvalid,
	MsgGitCredHostScopeRequired,
	MsgGitCredHostScopeInvalid,
	MsgGitCredBlobMissingField,
	MsgGitCredUnsupportedCredType,
}

func TestI18nCatalogs_HasGitKeys(t *testing.T) {
	for _, locale := range []string{LocaleEN, LocaleVI, LocaleZH} {
		for _, key := range gitKeys {
			msg := lookup(locale, key)
			if msg == key {
				t.Errorf("locale=%s key=%s falls back to key string (translation missing)", locale, key)
				continue
			}
			if strings.TrimSpace(msg) == "" {
				t.Errorf("locale=%s key=%s has empty translation", locale, key)
			}
		}
	}
}
