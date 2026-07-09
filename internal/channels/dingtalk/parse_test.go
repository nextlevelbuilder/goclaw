package dingtalk

import (
	"testing"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"
)

func TestParse_ConversationTypeMapsToPeerKind(t *testing.T) {
	for _, tc := range []struct {
		convType string
		wantGrp  bool
	}{
		{conversationTypeDirect, false},
		{conversationTypeGroup, true},
	} {
		in, err := parse(&chatbot.BotCallbackDataModel{
			Msgtype: "text", ConversationType: tc.convType, SenderStaffId: "s",
			Text: chatbot.BotCallbackDataTextModel{Content: "x"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if in.IsGroup != tc.wantGrp {
			t.Errorf("conversationType %q: IsGroup = %v, want %v", tc.convType, in.IsGroup, tc.wantGrp)
		}
	}
}

// A markdown message puts its body in text.content, not content. Reading
// content yields an empty message and no error — a silent drop.
func TestParse_MarkdownReadsTextContent(t *testing.T) {
	in, err := parse(&chatbot.BotCallbackDataModel{
		Msgtype:       "markdown",
		SenderStaffId: "s",
		Text:          chatbot.BotCallbackDataTextModel{Content: "# heading"},
		Content:       map[string]any{"text": "wrong place", "title": "t"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if in.Text != "# heading" {
		t.Errorf("Text = %q, want the text.content value", in.Text)
	}
}

// DingTalk sends content as an object on some types and as a JSON string on
// others. Both must parse identically.
func TestParse_ContentAsObjectOrString(t *testing.T) {
	asObject := map[string]any{"downloadCode": "dc-1"}
	asString := `{"downloadCode":"dc-1"}`

	for name, content := range map[string]any{"object": asObject, "string": asString} {
		t.Run(name, func(t *testing.T) {
			in, err := parse(&chatbot.BotCallbackDataModel{
				Msgtype: "picture", SenderStaffId: "s", Content: content,
			})
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(in.Media) != 1 || in.Media[0].DownloadCode != "dc-1" {
				t.Fatalf("media = %+v", in.Media)
			}
			if in.Media[0].Kind != "picture" {
				t.Errorf("kind = %q", in.Media[0].Kind)
			}
		})
	}
}

func TestParse_NilAndEmptyContent(t *testing.T) {
	for name, content := range map[string]any{"nil": nil, "empty string": "  "} {
		t.Run(name, func(t *testing.T) {
			in, err := parse(&chatbot.BotCallbackDataModel{
				Msgtype: "picture", SenderStaffId: "s", Content: content,
			})
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(in.Media) != 0 {
				t.Errorf("media = %+v, want none", in.Media)
			}
		})
	}
}

// DingTalk transcribes voice notes and ships the text in content.recognition.
// Feishu has no equivalent and must run STT on every clip.
func TestParse_AudioUsesRecognitionAsText(t *testing.T) {
	in, err := parse(&chatbot.BotCallbackDataModel{
		Msgtype: "audio", SenderStaffId: "s",
		Content: map[string]any{"downloadCode": "dc", "recognition": "  转写文本  "},
	})
	if err != nil {
		t.Fatal(err)
	}
	if in.Text != "转写文本" {
		t.Errorf("Text = %q, want the trimmed recognition", in.Text)
	}
	if len(in.Media) != 1 || in.Media[0].FileName != "audio.amr" {
		t.Errorf("media = %+v", in.Media)
	}
}

// Rich text has an old and a new shape. Only one is populated at a time.
func TestParse_RichTextBothShapes(t *testing.T) {
	newShape := map[string]any{
		"richText": []any{
			map[string]any{"text": "hello "},
			map[string]any{"text": "world"},
			map[string]any{"downloadCode": "dc-img", "type": "picture"},
		},
	}
	oldShape := map[string]any{
		"richTextList": []any{
			map[string]any{"text": "hello "},
			map[string]any{"text": "world"},
			map[string]any{"downloadCode": "dc-img", "type": "picture"},
		},
	}

	for name, content := range map[string]any{"new": newShape, "old": oldShape} {
		t.Run(name, func(t *testing.T) {
			in, err := parse(&chatbot.BotCallbackDataModel{
				Msgtype: "richText", SenderStaffId: "s", Content: content,
			})
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if in.Text != "hello world" {
				t.Errorf("Text = %q", in.Text)
			}
			if len(in.Media) != 1 || in.Media[0].Kind != "picture" {
				t.Errorf("media = %+v", in.Media)
			}
		})
	}
}

func TestParse_FileCarriesName(t *testing.T) {
	in, err := parse(&chatbot.BotCallbackDataModel{
		Msgtype: "file", SenderStaffId: "s",
		Content: map[string]any{"downloadCode": "dc", "fileName": "report.pdf"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Media) != 1 || in.Media[0].FileName != "report.pdf" {
		t.Errorf("media = %+v", in.Media)
	}
}

// Message types DingTalk may add later must be dropped, not guessed at.
func TestParse_UnknownMsgTypeIsInert(t *testing.T) {
	in, err := parse(&chatbot.BotCallbackDataModel{
		Msgtype: "someNewTypeIn2027", SenderStaffId: "s",
		Content: map[string]any{"whatever": true},
	})
	if err != nil {
		t.Fatalf("unknown msgtype must not error: %v", err)
	}
	if in.Text != "" || len(in.Media) != 0 {
		t.Errorf("unknown msgtype produced content: %+v", in)
	}
}

// The SDK states the mention outright. The upstream connector has no mention
// gate at all and leans on the platform only delivering @'d group messages.
func TestParse_MentionFromIsInAtList(t *testing.T) {
	for _, want := range []bool{true, false} {
		in, err := parse(&chatbot.BotCallbackDataModel{
			Msgtype: "text", SenderStaffId: "s", IsInAtList: want,
			Text: chatbot.BotCallbackDataTextModel{Content: "x"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if in.MentionedBot != want {
			t.Errorf("MentionedBot = %v, want %v", in.MentionedBot, want)
		}
	}
}

func TestParse_WebhookExpiryConverted(t *testing.T) {
	const ms = 1_800_000_000_000
	in, err := parse(&chatbot.BotCallbackDataModel{
		Msgtype: "text", SenderStaffId: "s",
		SessionWebhookExpiredTime: ms,
		Text:                      chatbot.BotCallbackDataTextModel{Content: "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if in.WebhookExpires.UnixMilli() != ms {
		t.Errorf("WebhookExpires = %v", in.WebhookExpires)
	}
}

func TestParse_MalformedContentErrors(t *testing.T) {
	_, err := parse(&chatbot.BotCallbackDataModel{
		Msgtype: "picture", SenderStaffId: "s", Content: `{not json`,
	})
	if err == nil {
		t.Fatal("want error on malformed content")
	}
}

func TestChatID(t *testing.T) {
	tests := []struct {
		name  string
		scope string
		in    inbound
		want  string
	}{
		{"dm routes on sender", GroupSessionScopeGroup,
			inbound{IsGroup: false, SenderID: "staff-1", ConversationID: "cid"}, "staff-1"},
		{"group scope group", GroupSessionScopeGroup,
			inbound{IsGroup: true, SenderID: "staff-1", ConversationID: "cid"}, "cid"},
		{"group scope group_sender", GroupSessionScopeGroupSender,
			inbound{IsGroup: true, SenderID: "staff-1", ConversationID: "cid"}, "cid:staff-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &Channel{cfg: Config{GroupSessionScope: tc.scope}}
			if got := c.chatID(&tc.in); got != tc.want {
				t.Errorf("chatID = %q, want %q", got, tc.want)
			}
		})
	}
}
