package methods

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// supportedChannelTypes mirrors the list in
// internal/http/channel_instances_type_test.go. Both tests exist because the
// two isValidChannelType switches are hand-copied and have drifted three times
// (facebook, pancake, bitrix24). Adding a platform to one switch but not the
// other now fails here.
var supportedChannelTypes = []string{
	channels.TypeBitrix24,
	channels.TypeDingtalk,
	channels.TypeDiscord,
	channels.TypeFacebook,
	channels.TypeFeishu,
	channels.TypePancake,
	channels.TypeSlack,
	channels.TypeTelegram,
	channels.TypeWhatsApp,
	channels.TypeZaloOA,
	channels.TypeZaloPersonal,
}

func TestIsValidChannelType_AcceptsEverySupportedType(t *testing.T) {
	for _, ct := range supportedChannelTypes {
		if !isValidChannelType(ct) {
			t.Errorf("WS validator rejects %q; the HTTP twin and the UI dropdown offer it", ct)
		}
	}
}

func TestIsValidChannelType_RejectsUnknown(t *testing.T) {
	for _, ct := range []string{"", "ws", "dingding", "DingTalk", "telegram2"} {
		if isValidChannelType(ct) {
			t.Errorf("WS validator accepts unsupported type %q", ct)
		}
	}
}
