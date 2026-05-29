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
