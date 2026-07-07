package telegram

import (
	"context"
	"testing"
	"time"

	"github.com/mymmrac/telego"
)

// newTriggerChannel returns a Channel with a pre-warmed trigger-word cache so
// matchesTriggerWords can be tested without a store.
func newTriggerChannel(words ...string) *Channel {
	return &Channel{
		triggerWords:   normalizeWakeWords(words),
		triggerWordsAt: time.Now(),
	}
}

func TestChannelMatchesTriggerWords(t *testing.T) {
	ctx := context.Background()
	c := newTriggerChannel("Паша", "Чмо")

	if !c.matchesTriggerWords(ctx, &telego.Message{Text: "эй, Паша"}) {
		t.Error("expected match in message text")
	}
	if !c.matchesTriggerWords(ctx, &telego.Message{Caption: "смотри, чмо!"}) {
		t.Error("expected match in media caption")
	}
	if c.matchesTriggerWords(ctx, &telego.Message{Text: "чмоки"}) {
		t.Error("substring must not match")
	}
	if c.matchesTriggerWords(ctx, &telego.Message{Text: "hello"}) {
		t.Error("unrelated text must not match")
	}

	// nil agentStore + expired cache → fails open, never matches, never panics.
	empty := &Channel{}
	if empty.matchesTriggerWords(ctx, &telego.Message{Text: "Паша"}) {
		t.Error("channel with no trigger words must never match")
	}
}

func TestNormalizeWakeWords(t *testing.T) {
	set := normalizeWakeWords([]string{"Паша", "  Слышь ", "ЧМО", "", "   "})
	if len(set) != 3 {
		t.Fatalf("expected 3 normalized words, got %d: %v", len(set), set)
	}
	for _, w := range []string{"паша", "слышь", "чмо"} {
		if _, ok := set[w]; !ok {
			t.Errorf("expected normalized set to contain %q", w)
		}
	}
}

func TestTextHasWakeWord(t *testing.T) {
	set := normalizeWakeWords([]string{"Паша", "Слышь", "Чмо"})

	cases := []struct {
		name string
		text string
		want bool
	}{
		{"exact", "Паша", true},
		{"uppercase", "ПАША иди сюда", true},
		{"trailing punctuation", "паша,", true},
		{"leading phrase and bang", "эй, чмо!", true},
		{"mid sentence", "ну слышь ты", true},
		{"substring not whole word", "чмоки в щечку", false},
		{"diminutive not whole word", "Пашка молодец", false},
		{"empty text", "", false},
		{"latin only", "hey pasha", false},
		{"unrelated", "привет всем", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := textHasWakeWord(tc.text, set); got != tc.want {
				t.Errorf("textHasWakeWord(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestTextHasWakeWord_EmptySet(t *testing.T) {
	if textHasWakeWord("Паша тут", nil) {
		t.Error("empty set must never match")
	}
	if textHasWakeWord("Паша тут", map[string]struct{}{}) {
		t.Error("empty set must never match")
	}
}
