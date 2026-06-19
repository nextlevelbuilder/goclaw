package telegram

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mymmrac/telego"
	ta "github.com/mymmrac/telego/telegoapi"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

func TestSendPlaceholderUpdateCreatesLazyPlaceholderWhenMissing(t *testing.T) {
	caller := &recordingTelegramCaller{}
	bot, err := telego.NewBot(
		"123456:abcdefghijklmnopqrstuvwxyzABCDE1234",
		telego.WithAPICaller(caller),
		telego.WithDiscardLogger(),
	)
	if err != nil {
		t.Fatalf("NewBot: %v", err)
	}

	ch := &Channel{
		BaseChannel: channels.NewBaseChannel("telegram", nil, nil),
		bot:         bot,
	}
	ch.SetRunning(true)

	status := bus.OutboundMessage{
		Channel: "telegram",
		ChatID:  "123",
		Content: "⚡ Running code...",
		Metadata: map[string]string{
			"placeholder_update":  "true",
			"reply_to_message_id": "77",
		},
	}
	if err := ch.Send(context.Background(), status); err != nil {
		t.Fatalf("first placeholder update: %v", err)
	}
	if got := caller.methodNames(); strings.Join(got, ",") != "sendMessage" {
		t.Fatalf("methods after first update = %v, want sendMessage", got)
	}
	if _, ok := ch.placeholders.Load("123"); !ok {
		t.Fatal("placeholder id was not stored after lazy send")
	}

	status.Content = "🔍 Searching the web..."
	if err := ch.Send(context.Background(), status); err != nil {
		t.Fatalf("second placeholder update: %v", err)
	}
	if got := caller.methodNames(); strings.Join(got, ",") != "sendMessage,editMessageText" {
		t.Fatalf("methods after second update = %v, want sendMessage,editMessageText", got)
	}

	final := bus.OutboundMessage{
		Channel:  "telegram",
		ChatID:   "123",
		Content:  "Done",
		Metadata: map[string]string{},
	}
	if err := ch.Send(context.Background(), final); err != nil {
		t.Fatalf("final send: %v", err)
	}
	if got := caller.methodNames(); strings.Join(got, ",") != "sendMessage,editMessageText,editMessageText" {
		t.Fatalf("methods after final = %v, want sendMessage,editMessageText,editMessageText", got)
	}
	if _, ok := ch.placeholders.Load("123"); ok {
		t.Fatal("placeholder survived final answer handoff")
	}
}

type recordingTelegramCaller struct {
	calls []recordedTelegramCall
}

type recordedTelegramCall struct {
	method string
	body   map[string]any
}

func (c *recordingTelegramCaller) Call(_ context.Context, url string, data *ta.RequestData) (*ta.Response, error) {
	method := url[strings.LastIndex(url, "/")+1:]
	body := map[string]any{}
	if len(data.BodyRaw) > 0 {
		_ = json.Unmarshal(data.BodyRaw, &body)
	}
	c.calls = append(c.calls, recordedTelegramCall{method: method, body: body})

	result := json.RawMessage(`{"message_id":101,"date":0,"chat":{"id":123,"type":"private"}}`)
	return &ta.Response{Ok: true, Result: result}, nil
}

func (c *recordingTelegramCaller) methodNames() []string {
	names := make([]string, 0, len(c.calls))
	for _, call := range c.calls {
		names = append(names, call.method)
	}
	return names
}
