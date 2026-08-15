package bitrix24

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

func TestChunkText_ShortStaysOneChunk(t *testing.T) {
	got := chunkText("hello world", 100)
	if len(got) != 1 || got[0] != "hello world" {
		t.Errorf("short text should not be split: %v", got)
	}
}

func TestChunkText_EmptyReturnsNil(t *testing.T) {
	if got := chunkText("", 100); got != nil {
		t.Errorf("empty input should return nil, got %v", got)
	}
	if got := chunkText("   \t\n ", 100); got != nil {
		t.Errorf("whitespace-only should return nil, got %v", got)
	}
}

func TestChunkText_PrefersNewlineBoundary(t *testing.T) {
	text := "line1\nline2\nline3-longer"
	got := chunkText(text, 10)
	if len(got) < 2 {
		t.Fatalf("expected at least 2 chunks, got %v", got)
	}
	// First chunk must end at a line boundary — never mid-word.
	if strings.Contains(got[0], "line3") {
		t.Errorf("first chunk overflowed past boundary: %q", got[0])
	}
}

func TestChunkText_PrefersWhitespaceWhenNoNewline(t *testing.T) {
	text := "one two three four five"
	got := chunkText(text, 8)
	if len(got) < 2 {
		t.Fatalf("expected multi-chunk, got %v", got)
	}
	// Rejoin without losing characters.
	rejoined := strings.Join(got, " ")
	// Allow whitespace shifting but every non-space rune from input must survive.
	origLetters := strings.ReplaceAll(text, " ", "")
	gotLetters := strings.ReplaceAll(rejoined, " ", "")
	if origLetters != gotLetters {
		t.Errorf("chunking lost characters: %q → %q", text, rejoined)
	}
}

func TestChunkText_HardBreakForLongWord(t *testing.T) {
	// No newline, no whitespace — must hard-break on rune boundary.
	text := strings.Repeat("a", 50)
	got := chunkText(text, 10)
	if len(got) < 5 {
		t.Fatalf("expected at least 5 chunks, got %d: %v", len(got), got)
	}
	for i, c := range got {
		if utf8.RuneCountInString(c) > 10 {
			t.Errorf("chunk %d exceeds limit (%d runes): %q", i, utf8.RuneCountInString(c), c)
		}
	}
}

func TestChunkText_UnicodeSafe(t *testing.T) {
	// Vietnamese text — each character takes 2-3 bytes in UTF-8. The byte-
	// length is > limit but the rune-count should stay within.
	text := "Xin chào tôi là trợ lý AI đây là tin nhắn siêu dài"
	got := chunkText(text, 10)
	for i, c := range got {
		if utf8.RuneCountInString(c) > 10 {
			t.Errorf("chunk %d has %d runes, limit 10: %q", i, utf8.RuneCountInString(c), c)
		}
		if !utf8.ValidString(c) {
			t.Errorf("chunk %d is not valid UTF-8", i)
		}
	}
}

func TestChunkText_LimitZeroUsesDefault(t *testing.T) {
	// When limit is <= 0 we should fall back to 4000 — so a short string
	// stays in one chunk.
	got := chunkText("hi", 0)
	if len(got) != 1 || got[0] != "hi" {
		t.Errorf("zero limit: got %v", got)
	}
}

func TestSliceRunes(t *testing.T) {
	h, tail := sliceRunes("abcdef", 3)
	if h != "abc" || tail != "def" {
		t.Errorf("sliceRunes(abcdef, 3) = (%q, %q); want (abc, def)", h, tail)
	}

	// n >= rune count → whole string returned as head.
	h, tail = sliceRunes("abc", 10)
	if h != "abc" || tail != "" {
		t.Errorf("sliceRunes(abc, 10) = (%q, %q); want (abc, '')", h, tail)
	}

	// Unicode: Vietnamese "xin" → 3 runes, bytes differ.
	h, tail = sliceRunes("xinchào", 3)
	if h != "xin" || tail != "chào" {
		t.Errorf("unicode slice: (%q, %q); want (xin, chào)", h, tail)
	}
}

func TestFindChunkBoundary_NewlinePreferred(t *testing.T) {
	// Newline at byte index 5, space at 11. Must cut AFTER newline (index 6).
	s := "line1\nmore content here"
	cut := findChunkBoundary(s, 15)
	if cut != 6 {
		t.Errorf("cut = %d; want 6 (after newline)", cut)
	}
}

func TestFindChunkBoundary_WhitespaceFallback(t *testing.T) {
	// No newline. Cut should land right after the last space inside `limit`.
	s := "one two three four"
	cut := findChunkBoundary(s, 8)
	// First 8 runes: "one two " — last space at index 7, cut = 8.
	if cut != 8 {
		t.Errorf("cut = %d; want 8 (after space)", cut)
	}
}

func TestFindChunkBoundary_HardBreakNoBoundaries(t *testing.T) {
	s := "abcdefghij"
	cut := findChunkBoundary(s, 5)
	if cut != 5 {
		t.Errorf("hard break cut = %d; want 5", cut)
	}
}

func TestIsRateLimitErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", errors.New("generic"), false},
		{"QUERY_LIMIT_EXCEEDED", &APIError{Code: "QUERY_LIMIT_EXCEEDED"}, true},
		{"OPERATION_TIME_LIMIT", &APIError{Code: "OPERATION_TIME_LIMIT"}, true},
		{"other code", &APIError{Code: "expired_token"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRateLimitErr(tc.err); got != tc.want {
				t.Errorf("isRateLimitErr(%v) = %v; want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestSend_NotRunningErrors(t *testing.T) {
	fs := newFakeStore()
	resetWebhookRouterForTest()
	defer resetWebhookRouterForTest()
	fn := FactoryWithPortalStore(fs, "")

	ch, err := fn("b1", nil,
		[]byte(`{"portal":"p","bot_code":"c","bot_name":"n"}`),
		bus.New(), nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	// Channel not started — IsRunning() == false.
	err = ch.Send(context.Background(), bus.OutboundMessage{ChatID: "1", Content: "hi"})
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Errorf("expected 'not running' error, got %v", err)
	}
}

func TestSend_MissingChatID(t *testing.T) {
	fs := newFakeStore()
	resetWebhookRouterForTest()
	defer resetWebhookRouterForTest()
	fn := FactoryWithPortalStore(fs, "")

	ch, err := fn("b1", nil,
		[]byte(`{"portal":"p","bot_code":"c","bot_name":"n"}`),
		bus.New(), nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	bc := ch.(*Channel)
	// Hack: pretend we're initialised without going through Start.
	bc.SetRunning(true)
	bc.startMu.Lock()
	bc.botID = 1
	bc.client = NewClient("portal.bitrix24.com", nil)
	bc.startMu.Unlock()

	err = ch.Send(context.Background(), bus.OutboundMessage{ChatID: "   ", Content: "hi"})
	if err == nil || !strings.Contains(err.Error(), "chat_id") {
		t.Errorf("expected missing chat_id error, got %v", err)
	}
}

func TestSend_EmptyContentIsNoOp(t *testing.T) {
	fs := newFakeStore()
	resetWebhookRouterForTest()
	defer resetWebhookRouterForTest()
	fn := FactoryWithPortalStore(fs, "")

	ch, err := fn("b1", nil,
		[]byte(`{"portal":"p","bot_code":"c","bot_name":"n"}`),
		bus.New(), nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	bc := ch.(*Channel)
	bc.SetRunning(true)
	bc.startMu.Lock()
	bc.botID = 1
	bc.client = NewClient("portal.bitrix24.com", nil)
	bc.startMu.Unlock()

	// No content, no media — must not attempt any HTTP call.
	if err := ch.Send(context.Background(), bus.OutboundMessage{ChatID: "42", Content: "  "}); err != nil {
		t.Errorf("empty content should be no-op, got %v", err)
	}
}

// TestResolveSendOptions verifies the Metadata → sendOptions decoding that
// Send() uses to pick v1 whisper vs v2 public and to thread replyId into
// the v2 fields object. Defaults must preserve pre-refactor behaviour
// (public, no replyId) so any caller missing the keys still works.
func TestResolveSendOptions(t *testing.T) {
	cases := []struct {
		name            string
		meta            map[string]string
		wantVisibility  string
		wantReplyToMID  int
	}{
		{
			name:           "empty metadata defaults to public",
			meta:           nil,
			wantVisibility: VisibilityPublic,
			wantReplyToMID: 0,
		},
		{
			name:           "whisper visibility",
			meta:           map[string]string{MetaKeyVisibility: VisibilityWhisper},
			wantVisibility: VisibilityWhisper,
			wantReplyToMID: 0,
		},
		{
			name:           "explicit public visibility",
			meta:           map[string]string{MetaKeyVisibility: VisibilityPublic},
			wantVisibility: VisibilityPublic,
			wantReplyToMID: 0,
		},
		{
			name:           "unknown visibility value falls back to public",
			meta:           map[string]string{MetaKeyVisibility: "secret"},
			wantVisibility: VisibilityPublic,
			wantReplyToMID: 0,
		},
		{
			name:           "valid numeric message_id parsed for replyToMID",
			meta:           map[string]string{MetaKeyMessageID: "297178"},
			wantVisibility: VisibilityPublic,
			wantReplyToMID: 297178,
		},
		{
			name:           "non-numeric message_id ignored",
			meta:           map[string]string{MetaKeyMessageID: "abc"},
			wantVisibility: VisibilityPublic,
			wantReplyToMID: 0,
		},
		{
			name:           "zero message_id ignored",
			meta:           map[string]string{MetaKeyMessageID: "0"},
			wantVisibility: VisibilityPublic,
			wantReplyToMID: 0,
		},
		{
			name: "whisper with replyToMID still captures both",
			meta: map[string]string{
				MetaKeyVisibility: VisibilityWhisper,
				MetaKeyMessageID:  "12345",
			},
			wantVisibility: VisibilityWhisper,
			wantReplyToMID: 12345,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := resolveSendOptions(tc.meta)
			if opts.visibility != tc.wantVisibility {
				t.Errorf("visibility = %q; want %q", opts.visibility, tc.wantVisibility)
			}
			if opts.replyToMID != tc.wantReplyToMID {
				t.Errorf("replyToMID = %d; want %d", opts.replyToMID, tc.wantReplyToMID)
			}
		})
	}
}

// TestSend_BranchesOnVisibility wires Send() through the HTTP-layer stub
// (captureRT) so we can assert exactly which Bitrix REST method got
// invoked and what params were on the wire. Public visibility → v2;
// whisper visibility → v1 with SKIP_CONNECTOR=Y. The matrix also covers
// replyId propagation on v2 and its absence on v1.
func TestSend_BranchesOnVisibility(t *testing.T) {
	cases := []struct {
		name           string
		metadata       map[string]string
		wantPath       string
		wantFormChecks map[string]string // exact form-key → expected value
		notWantKeys    []string          // form keys that must be absent
	}{
		{
			name: "whisper routes to v1 imbot.message.add with SKIP_CONNECTOR=Y",
			metadata: map[string]string{
				MetaKeyVisibility: VisibilityWhisper,
				MetaKeyMessageID:  "297178", // v1 ignores replyId so must NOT appear
			},
			wantPath: "/rest/imbot.message.add.json",
			wantFormChecks: map[string]string{
				"BOT_ID":         "1",
				"DIALOG_ID":      "chat4878",
				"MESSAGE":        "hi from bot",
				"SKIP_CONNECTOR": "Y",
			},
			notWantKeys: []string{
				"fields[replyId]",
				"replyId",
				"botId",
			},
		},
		{
			name: "public with replyId routes to v2 + fields[replyId]",
			metadata: map[string]string{
				MetaKeyVisibility: VisibilityPublic,
				MetaKeyMessageID:  "297196",
			},
			wantPath: "/rest/imbot.v2.Chat.Message.send.json",
			wantFormChecks: map[string]string{
				"botId":            "1",
				"dialogId":         "chat4878",
				"fields[message]":  "hi from bot",
				"fields[replyId]":  "297196",
			},
			notWantKeys: []string{
				"SKIP_CONNECTOR",
				"BOT_ID",
				"MESSAGE",
			},
		},
		{
			name:     "public without message_id routes to v2 without replyId",
			metadata: map[string]string{MetaKeyVisibility: VisibilityPublic},
			wantPath: "/rest/imbot.v2.Chat.Message.send.json",
			wantFormChecks: map[string]string{
				"botId":           "1",
				"dialogId":        "chat4878",
				"fields[message]": "hi from bot",
			},
			notWantKeys: []string{
				"fields[replyId]",
				"replyId",
				"SKIP_CONNECTOR",
			},
		},
		{
			name:     "missing visibility defaults to v2 public (backward-compat)",
			metadata: nil,
			wantPath: "/rest/imbot.v2.Chat.Message.send.json",
			wantFormChecks: map[string]string{
				"botId":           "1",
				"dialogId":        "chat4878",
				"fields[message]": "hi from bot",
			},
			notWantKeys: []string{
				"SKIP_CONNECTOR",
				"fields[replyId]",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := &captureRT{result: `{"result":{"id":1}}`}
			client := newStubClient("test.bitrix24.com", rt)
			ch, _ := newFakeChannelWithClient(t, client)
			ch.SetRunning(true)

			msg := bus.OutboundMessage{
				ChatID:   "chat4878",
				Content:  "hi from bot",
				Metadata: tc.metadata,
			}
			if err := ch.Send(context.Background(), msg); err != nil {
				t.Fatalf("Send error: %v", err)
			}

			if len(rt.paths) != 1 {
				t.Fatalf("expected exactly 1 HTTP call, got %d (paths=%v)", len(rt.paths), rt.paths)
			}
			if rt.paths[0] != tc.wantPath {
				t.Errorf("path = %q; want %q", rt.paths[0], tc.wantPath)
			}

			form := rt.reqs[0]
			for k, want := range tc.wantFormChecks {
				if got := form.Get(k); got != want {
					t.Errorf("form[%q] = %q; want %q (full form: %v)", k, got, want, form)
				}
			}
			for _, k := range tc.notWantKeys {
				if v := form.Get(k); v != "" {
					t.Errorf("form[%q] should be absent, got %q", k, v)
				}
			}
		})
	}
}

// TestSend_OL_EchoPrefixPrepended verifies the openline sender-tag echo: whatever
// handle.go placed in MetaKeySenderPrefix is prepended verbatim to the outbound
// text so the Open Channel connector can route the reply back to the right
// external message. send.go is format-agnostic — handle.go decides the shape
// ("#msgId" for 3-token, "[name] #msgId" for legacy).
func TestSend_OL_EchoPrefixPrepended(t *testing.T) {
	cases := []struct {
		name    string
		prefix  string
		wantMsg string
	}{
		{
			name:    "3-token echo is msgId only",
			prefix:  "#777888",
			wantMsg: "#777888 hi from bot",
		},
		{
			name:    "legacy echo keeps full [name] #msgId",
			prefix:  "[Trung Hee] #7957717404177",
			wantMsg: "[Trung Hee] #7957717404177 hi from bot",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := &captureRT{result: `{"result":{"id":1}}`}
			client := newStubClient("test.bitrix24.com", rt)
			ch, _ := newFakeChannelWithClient(t, client)
			ch.SetRunning(true)

			msg := bus.OutboundMessage{
				ChatID:  "chat4878",
				Content: "hi from bot",
				Metadata: map[string]string{
					MetaKeyVisibility:   VisibilityPublic,
					MetaKeySenderPrefix: tc.prefix,
				},
			}
			if err := ch.Send(context.Background(), msg); err != nil {
				t.Fatalf("Send error: %v", err)
			}
			if len(rt.reqs) != 1 {
				t.Fatalf("expected exactly 1 HTTP call, got %d", len(rt.reqs))
			}
			if got := rt.reqs[0].Get("fields[message]"); got != tc.wantMsg {
				t.Errorf("fields[message] = %q; want %q", got, tc.wantMsg)
			}
		})
	}
}

// TestBuildAddressMention covers the address-user resolver that prepends the
// `[USER=<id>][/USER]` BBCode to outbound replies in group chats. The format
// is intentionally empty-named so Bitrix renders the user's current display
// name from id at delivery time (sidesteps escaping for names with BBCode
// metacharacters and reflects renames between turns).
//
// Consumer-side gating (cmd/gateway_consumer_normal.go) is responsible for
// only setting `bitrix_address_user_id` in group inbounds and skipping
// synthetic senders. This test pins the channel-side behaviour given that
// gating contract.
func TestBuildAddressMention(t *testing.T) {
	cases := []struct {
		name  string
		meta  map[string]string
		botID int
		want  string
	}{
		{
			name:  "no_metadata_returns_empty",
			meta:  nil,
			botID: 940,
			want:  "",
		},
		{
			name:  "empty_user_id_returns_empty",
			meta:  map[string]string{"bitrix_address_user_id": ""},
			botID: 940,
			want:  "",
		},
		{
			name:  "whitespace_user_id_returns_empty",
			meta:  map[string]string{"bitrix_address_user_id": "   "},
			botID: 940,
			want:  "",
		},
		{
			name:  "real_user_id_returns_bbcode",
			meta:  map[string]string{"bitrix_address_user_id": "62"},
			botID: 940,
			want:  "[USER=62][/USER]",
		},
		{
			name:  "trims_user_id_whitespace",
			meta:  map[string]string{"bitrix_address_user_id": "  62  "},
			botID: 940,
			want:  "[USER=62][/USER]",
		},
		{
			// Self-mention guard: bot's own numeric id matches addressee →
			// suppress to avoid weird "@Bot Synity" prefix in bot's own message.
			name:  "self_mention_suppressed",
			meta:  map[string]string{"bitrix_address_user_id": "940"},
			botID: 940,
			want:  "",
		},
		{
			// Bot id unknown (channel not yet started) → don't apply guard,
			// trust the consumer's gating. Returning the BBCode is harmless;
			// Bitrix will render whatever user the id resolves to.
			name:  "unknown_bot_id_skips_self_guard",
			meta:  map[string]string{"bitrix_address_user_id": "940"},
			botID: 0,
			want:  "[USER=940][/USER]",
		},
		{
			// Synthetic openline per-participant id is not a real Bitrix user —
			// emitting [USER=openlines:...] would render as literal garbage.
			name:  "synthetic_openline_id_suppressed",
			meta:  map[string]string{"bitrix_address_user_id": "openlines:tamgiac:chat4878:111222"},
			botID: 940,
			want:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildAddressMention(tc.meta, tc.botID)
			if got != tc.want {
				t.Errorf("buildAddressMention(%v, %d) = %q; want %q", tc.meta, tc.botID, got, tc.want)
			}
		})
	}
}

// (Send() integration with httptest server is covered by existing send tests
// — adding a new httptest pipeline just for the prepend would duplicate that
// scaffolding for what is logically a single string-concat call site. The
// helper test above pins the behaviour; trust the existing send pipeline
// for chunk routing.)
