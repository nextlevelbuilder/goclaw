package telegram

import (
	"testing"

	"github.com/mymmrac/telego"
)

// --- detectMention ---

func TestDetectMention_EmptyBotUsername(t *testing.T) {
	ch := &Channel{}
	msg := &telego.Message{Text: "@somebot hello"}
	// Empty botUsername → always false (fail closed).
	if ch.detectMention(msg, "") {
		t.Error("detectMention with empty botUsername should return false")
	}
}

func TestDetectMention_TextMentionEntity(t *testing.T) {
	ch := &Channel{}
	// Message that mentions @testbot via a mention entity.
	msg := &telego.Message{
		Text: "@testbot please help",
		Entities: []telego.MessageEntity{
			{Type: "mention", Offset: 0, Length: 8},
		},
	}
	if !ch.detectMention(msg, "testbot") {
		t.Error("detectMention should return true when bot is @mentioned via entity")
	}
}

func TestDetectMention_CaseInsensitiveUsername(t *testing.T) {
	ch := &Channel{}
	msg := &telego.Message{
		Text: "@TestBot please help",
		Entities: []telego.MessageEntity{
			{Type: "mention", Offset: 0, Length: 8},
		},
	}
	if !ch.detectMention(msg, "testbot") {
		t.Error("detectMention should be case-insensitive for username match")
	}
}

func TestDetectMention_DifferentUserMentioned(t *testing.T) {
	ch := &Channel{}
	msg := &telego.Message{
		Text: "@otherbot hello",
		Entities: []telego.MessageEntity{
			{Type: "mention", Offset: 0, Length: 9},
		},
	}
	if ch.detectMention(msg, "testbot") {
		t.Error("detectMention should return false when different user is mentioned")
	}
}

func TestDetectMention_FallbackSubstringCheck(t *testing.T) {
	ch := &Channel{}
	// No entities but text contains @testbot → fallback substring match.
	msg := &telego.Message{Text: "hey @testbot can you help?"}
	if !ch.detectMention(msg, "testbot") {
		t.Error("detectMention should return true via fallback substring check")
	}
}

func TestDetectMention_CaptionMention(t *testing.T) {
	ch := &Channel{}
	// Media message with mention in caption.
	msg := &telego.Message{
		Caption: "@testbot look at this photo",
		CaptionEntities: []telego.MessageEntity{
			{Type: "mention", Offset: 0, Length: 8},
		},
	}
	if !ch.detectMention(msg, "testbot") {
		t.Error("detectMention should detect mention in caption entities")
	}
}

func TestDetectMention_ReplyToBotMessage(t *testing.T) {
	ch := &Channel{}
	// Replying to bot's message = implicit mention.
	msg := &telego.Message{
		Text: "thanks",
		ReplyToMessage: &telego.Message{
			From: &telego.User{Username: "testbot"},
		},
	}
	if !ch.detectMention(msg, "testbot") {
		t.Error("detectMention should return true when replying to bot's message")
	}
}

func TestDetectMention_ReplyToDifferentUser(t *testing.T) {
	ch := &Channel{}
	msg := &telego.Message{
		Text: "cool",
		ReplyToMessage: &telego.Message{
			From: &telego.User{Username: "someoneelse"},
		},
	}
	if ch.detectMention(msg, "testbot") {
		t.Error("detectMention should return false when replying to a different user")
	}
}

func TestDetectMention_NoMentionInPlainText(t *testing.T) {
	ch := &Channel{}
	msg := &telego.Message{Text: "just a plain message"}
	if ch.detectMention(msg, "testbot") {
		t.Error("detectMention should return false for plain text with no mention")
	}
}

func TestDetectMention_BotCommandWithAtSuffix(t *testing.T) {
	ch := &Channel{}
	// /start@testbot as a bot_command entity.
	msg := &telego.Message{
		Text: "/start@testbot",
		Entities: []telego.MessageEntity{
			{Type: "bot_command", Offset: 0, Length: 14},
		},
	}
	if !ch.detectMention(msg, "testbot") {
		t.Error("detectMention should return true for /cmd@botname entity")
	}
}

// --- hasOtherMention ---

func TestHasOtherMention_NoMentions(t *testing.T) {
	ch := &Channel{}
	msg := &telego.Message{Text: "plain text"}
	if ch.hasOtherMention(msg, "mybot") {
		t.Error("hasOtherMention should return false for plain text")
	}
}

func TestHasOtherMention_OnlyOwnMention(t *testing.T) {
	ch := &Channel{}
	// Only @mybot is mentioned → should NOT trigger hasOtherMention.
	msg := &telego.Message{
		Text: "@mybot hello",
		Entities: []telego.MessageEntity{
			{Type: "mention", Offset: 0, Length: 6},
		},
	}
	if ch.hasOtherMention(msg, "mybot") {
		t.Error("hasOtherMention should return false when only own bot is mentioned")
	}
}

func TestHasOtherMention_AnotherBotMentioned(t *testing.T) {
	ch := &Channel{}
	// @otherbot is mentioned.
	msg := &telego.Message{
		Text: "@otherbot help",
		Entities: []telego.MessageEntity{
			{Type: "mention", Offset: 0, Length: 9},
		},
	}
	if !ch.hasOtherMention(msg, "mybot") {
		t.Error("hasOtherMention should return true when a different bot is @mentioned")
	}
}

func TestHasOtherMention_CommandAddressedToOtherBot(t *testing.T) {
	ch := &Channel{}
	// /start@otherbot — command addressed to another bot.
	msg := &telego.Message{
		Text: "/start@otherbot",
		Entities: []telego.MessageEntity{
			{Type: "bot_command", Offset: 0, Length: 15},
		},
	}
	if !ch.hasOtherMention(msg, "mybot") {
		t.Error("hasOtherMention should return true for /cmd@otherbot entity")
	}
}

func TestHasOtherMention_CommandAddressedToOwnBot(t *testing.T) {
	ch := &Channel{}
	// /start@mybot — addressed to own bot → should NOT trigger.
	msg := &telego.Message{
		Text: "/start@mybot",
		Entities: []telego.MessageEntity{
			{Type: "bot_command", Offset: 0, Length: 12},
		},
	}
	if ch.hasOtherMention(msg, "mybot") {
		t.Error("hasOtherMention should return false for /cmd@mybot (own bot)")
	}
}

func TestHasOtherMention_CaptionWithOtherMention(t *testing.T) {
	ch := &Channel{}
	msg := &telego.Message{
		Caption: "@otherbot look at this",
		CaptionEntities: []telego.MessageEntity{
			{Type: "mention", Offset: 0, Length: 9},
		},
	}
	if !ch.hasOtherMention(msg, "mybot") {
		t.Error("hasOtherMention should detect other @mention in caption")
	}
}

// --- stripBotMention ---

func TestStripBotMention_RemovesMention(t *testing.T) {
	got := stripBotMention("@viet_super_bot vẽ ảnh minh họa", "viet_super_bot")
	want := "vẽ ảnh minh họa"
	if got != want {
		t.Errorf("stripBotMention = %q, want %q", got, want)
	}
}

func TestStripBotMention_CaseInsensitive(t *testing.T) {
	got := stripBotMention("@Viet_Super_Bot hello", "viet_super_bot")
	if got != "hello" {
		t.Errorf("stripBotMention case-insensitive = %q, want %q", got, "hello")
	}
}

func TestStripBotMention_PreservesOtherMentions(t *testing.T) {
	got := stripBotMention("@viet_super_bot hỏi @alice về X", "viet_super_bot")
	want := "hỏi @alice về X"
	if got != want {
		t.Errorf("stripBotMention = %q, want %q", got, want)
	}
}

func TestStripBotMention_WordBoundary(t *testing.T) {
	// @viet_super_bot2 must NOT match @viet_super_bot (different bot with similar prefix).
	got := stripBotMention("@viet_super_bot2 hello", "viet_super_bot")
	if got != "@viet_super_bot2 hello" {
		t.Errorf("stripBotMention should not match prefix; got %q", got)
	}
}

func TestStripBotMention_EmptyUsername(t *testing.T) {
	text := "@anything else"
	if got := stripBotMention(text, ""); got != text {
		t.Errorf("stripBotMention with empty botUsername should return text unchanged; got %q", got)
	}
}

func TestStripBotMention_MultipleOccurrences(t *testing.T) {
	got := stripBotMention("hey @viet_super_bot, @viet_super_bot help!", "viet_super_bot")
	// Both removed; internal spacing/punctuation preserved.
	want := "hey ,  help!"
	if got != want {
		t.Errorf("stripBotMention multi = %q, want %q", got, want)
	}
}

func TestStripBotMention_PreservesEmailLike(t *testing.T) {
	// "@viet_super_bot" embedded inside a word (e.g. email/URL) must NOT be stripped.
	// Telegram mentions require a leading word-boundary, so inline matches are false positives.
	in := "contact@viet_super_bot.com please"
	if got := stripBotMention(in, "viet_super_bot"); got != in {
		t.Errorf("stripBotMention should not strip mention embedded in word; got %q, want %q", got, in)
	}
}

func TestStripBotMention_OnlyMentionBecomesEmpty(t *testing.T) {
	if got := stripBotMention("@viet_super_bot", "viet_super_bot"); got != "" {
		t.Errorf("mention-only input should become empty; got %q", got)
	}
}

// --- isServiceMessage ---

func TestIsServiceMessage_WithText(t *testing.T) {
	msg := &telego.Message{Text: "hello world"}
	if isServiceMessage(msg) {
		t.Error("message with text is NOT a service message")
	}
}

func TestIsServiceMessage_WithCaption(t *testing.T) {
	msg := &telego.Message{Caption: "photo caption"}
	if isServiceMessage(msg) {
		t.Error("message with caption is NOT a service message")
	}
}

func TestIsServiceMessage_WithPhoto(t *testing.T) {
	msg := &telego.Message{Photo: []telego.PhotoSize{{FileID: "x"}}}
	if isServiceMessage(msg) {
		t.Error("message with photo is NOT a service message")
	}
}

func TestIsServiceMessage_WithAudio(t *testing.T) {
	msg := &telego.Message{Audio: &telego.Audio{FileID: "x"}}
	if isServiceMessage(msg) {
		t.Error("message with audio is NOT a service message")
	}
}

func TestIsServiceMessage_WithDocument(t *testing.T) {
	msg := &telego.Message{Document: &telego.Document{FileID: "x"}}
	if isServiceMessage(msg) {
		t.Error("message with document is NOT a service message")
	}
}

func TestIsServiceMessage_EmptyMessage(t *testing.T) {
	msg := &telego.Message{}
	if !isServiceMessage(msg) {
		t.Error("empty message (no text, caption, media) is a service message")
	}
}

func TestIsServiceMessage_WithSticker(t *testing.T) {
	msg := &telego.Message{Sticker: &telego.Sticker{FileID: "x"}}
	if isServiceMessage(msg) {
		t.Error("message with sticker is NOT a service message")
	}
}

func TestIsServiceMessage_WithVoice(t *testing.T) {
	msg := &telego.Message{Voice: &telego.Voice{FileID: "x"}}
	if isServiceMessage(msg) {
		t.Error("message with voice is NOT a service message")
	}
}

func TestIsServiceMessage_WithLocation(t *testing.T) {
	msg := &telego.Message{Location: &telego.Location{Longitude: 1.0}}
	if isServiceMessage(msg) {
		t.Error("message with location is NOT a service message")
	}
}

func TestIsServiceMessage_WithPoll(t *testing.T) {
	msg := &telego.Message{Poll: &telego.Poll{ID: "p1"}}
	if isServiceMessage(msg) {
		t.Error("message with poll is NOT a service message")
	}
}
