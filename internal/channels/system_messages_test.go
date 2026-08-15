package channels

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/systemmessages"
)

func TestBaseChannelRendersConfiguredSystemMessage(t *testing.T) {
	cfg := config.Default()
	cfg.Messages.Messages = map[string]config.LocalizedSystemMessage{
		systemmessages.KeyPairingGroupRequired: {
			i18n.LocaleEN: "Custom group code {{code}}",
		},
	}

	base := NewBaseChannel("telegram-main", nil, nil)
	base.SetSystemMessages(systemmessages.NewResolver(cfg))

	got := base.SystemMessage(i18n.LocaleEN, systemmessages.KeyPairingGroupRequired, systemmessages.Vars{"code": "XYZ"})
	want := "Custom group code XYZ"
	if got != want {
		t.Fatalf("SystemMessage = %q, want %q", got, want)
	}
}

func TestBaseChannelSystemMessageUsesConfiguredDefaultLocale(t *testing.T) {
	cfg := config.Default()
	cfg.Messages.DefaultLocale = i18n.LocaleVI
	cfg.Messages.Messages = map[string]config.LocalizedSystemMessage{
		systemmessages.KeyPairingAccountRequired: {
			i18n.LocaleVI: "Ghép {{platform}} {{sender_id}} bằng {{code}}",
			i18n.LocaleEN: "Pair {{platform}} {{sender_id}} with {{code}}",
		},
	}

	base := NewBaseChannel("telegram-main", nil, nil)
	base.SetSystemMessages(systemmessages.NewResolver(cfg))

	got := base.SystemMessage("", systemmessages.KeyPairingAccountRequired, systemmessages.Vars{
		"platform":  "Telegram",
		"sender_id": "U123",
		"code":      "XYZ",
	})
	want := "Ghép Telegram U123 bằng XYZ"
	if got != want {
		t.Fatalf("SystemMessage empty locale = %q, want %q", got, want)
	}
}

func TestBaseChannelSystemMessageFallsBackWithoutResolver(t *testing.T) {
	base := NewBaseChannel("telegram-main", nil, nil)
	got := base.SystemMessage(i18n.LocaleEN, systemmessages.KeyPairingApproved, systemmessages.Vars{"app_name": "GoClaw"})
	want := "✅ GoClaw access approved. Send a message to start chatting."
	if got != want {
		t.Fatalf("SystemMessage fallback = %q, want %q", got, want)
	}
}
