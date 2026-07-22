package http

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// supportedChannelTypes is every platform GoClaw can instantiate. It exists to
// pin the HTTP validator against its WS twin in
// internal/gateway/methods/channel_instances.go, which keeps an identical list
// in its own test. When the two switches drift, the WS-driven UI rejects
// channels the HTTP API accepts (this has happened three times: facebook,
// pancake, bitrix24).
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
			t.Errorf("HTTP validator rejects %q; the WS twin and the UI dropdown offer it", ct)
		}
	}
}

func TestIsValidChannelType_RejectsUnknown(t *testing.T) {
	for _, ct := range []string{"", "ws", "dingding", "DingTalk", "telegram2"} {
		if isValidChannelType(ct) {
			t.Errorf("HTTP validator accepts unsupported type %q", ct)
		}
	}
}
