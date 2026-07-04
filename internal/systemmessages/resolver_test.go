package systemmessages

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
)

func TestResolverUsesLocaleOverrideAndVariables(t *testing.T) {
	cfg := config.Default()
	cfg.Messages.Messages = map[string]config.LocalizedSystemMessage{
		KeyPairingGroupRequired: {
			i18n.LocaleVI: "{{app_name}} cần ghép nối nhóm. Mã: {{code}}.",
		},
	}

	r := NewResolver(cfg)
	got := r.Render(i18n.LocaleVI, KeyPairingGroupRequired, Vars{"app_name": "AcmeBot", "code": "ABCD1234"})
	want := "AcmeBot cần ghép nối nhóm. Mã: ABCD1234."
	if got != want {
		t.Fatalf("Render override = %q, want %q", got, want)
	}
}

func TestResolverUsesConfiguredDefaultLocaleWhenCallerLocaleIsEmpty(t *testing.T) {
	cfg := config.Default()
	cfg.Messages.DefaultLocale = i18n.LocaleVI
	cfg.Messages.Messages = map[string]config.LocalizedSystemMessage{
		KeyPairingGroupRequired: {
			i18n.LocaleVI: "Nhóm cần ghép nối. Mã: {{code}}.",
			i18n.LocaleEN: "Group needs pairing. Code: {{code}}.",
		},
	}

	r := NewResolver(cfg)
	got := r.Render("", KeyPairingGroupRequired, Vars{"code": "ABCD1234"})
	want := "Nhóm cần ghép nối. Mã: ABCD1234."
	if got != want {
		t.Fatalf("Render empty locale with configured default = %q, want %q", got, want)
	}
}

func TestResolverFallsBackToEnglishOverrideThenDefaultTemplate(t *testing.T) {
	cfg := config.Default()
	cfg.Messages.Messages = map[string]config.LocalizedSystemMessage{
		KeyPairingAccountRequired: {
			i18n.LocaleEN: "Pair {{platform}} user {{sender_id}} with {{code}}",
		},
	}

	r := NewResolver(cfg)
	got := r.Render(i18n.LocaleKO, KeyPairingAccountRequired, Vars{
		"platform":  "Telegram",
		"sender_id": "U123",
		"code":      "PAIRME",
	})
	want := "Pair Telegram user U123 with PAIRME"
	if got != want {
		t.Fatalf("Render English fallback = %q, want %q", got, want)
	}

	got = r.Render(i18n.LocaleVI, KeyPairingApproved, Vars{"app_name": "GoClaw"})
	want = "✅ GoClaw access approved. Send a message to start chatting."
	if got != want {
		t.Fatalf("Render default template = %q, want %q", got, want)
	}
}

func TestResolverLeavesUnknownVariablesVisible(t *testing.T) {
	cfg := config.Default()
	cfg.Messages.Messages = map[string]config.LocalizedSystemMessage{
		KeyPairingGroupRequired: {
			i18n.LocaleEN: "Known {{code}} unknown {{missing}}",
		},
	}

	r := NewResolver(cfg)
	got := r.Render(i18n.LocaleEN, KeyPairingGroupRequired, Vars{"code": "123"})
	want := "Known 123 unknown {{missing}}"
	if got != want {
		t.Fatalf("Render unknown variables = %q, want %q", got, want)
	}
}

func TestKnownMessagesExposeDefaultsForUI(t *testing.T) {
	defs := Defaults()
	for _, key := range []string{KeyPairingAccountRequired, KeyPairingGroupRequired, KeyPairingGroupPrivateRequired, KeyPairingApproved} {
		if defs[key].Key != key {
			t.Fatalf("Defaults()[%q] missing or wrong key", key)
		}
		if defs[key].Template == "" {
			t.Fatalf("Defaults()[%q] has empty template", key)
		}
	}
}
