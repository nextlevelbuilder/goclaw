package telegram

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mymmrac/telego"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// triggerFakeStore implements just the two AgentStore methods agentTriggerWords
// needs; the embedded interface makes any other call panic (none expected).
// GetAgentContextFiles mirrors the real store: it requires tenant scope in ctx.
type triggerFakeStore struct {
	store.AgentStore
	agentID uuid.UUID
	files   []store.AgentContextFileData
}

func (f *triggerFakeStore) GetByKey(ctx context.Context, key string) (*store.AgentData, error) {
	return &store.AgentData{BaseModel: store.BaseModel{ID: f.agentID}}, nil
}

func (f *triggerFakeStore) GetAgentContextFiles(ctx context.Context, id uuid.UUID) ([]store.AgentContextFileData, error) {
	if store.TenantIDFromContext(ctx) == uuid.Nil {
		return nil, fmt.Errorf("tenant_id required")
	}
	return f.files, nil
}

// agentTriggerWords must propagate tenant scope to GetAgentContextFiles, or the
// scoped store errors and trigger words silently never load (groups never fire).
func TestAgentTriggerWords_ScopesTenantAndLoadsIdentity(t *testing.T) {
	fake := &triggerFakeStore{
		agentID: uuid.New(),
		files: []store.AgentContextFileData{
			{FileName: "SOUL.md", Content: "irrelevant"},
			{FileName: "IDENTITY.md", Content: "Name: Pasha\nTrigger words: Паша, Чмо"},
		},
	}
	c := &Channel{BaseChannel: channels.NewBaseChannel(channels.TypeTelegram, nil, nil), agentStore: fake}
	c.SetAgentID("bot-pasha")
	c.SetTenantID(uuid.New())

	set := c.agentTriggerWords(context.Background())
	if len(set) != 2 {
		t.Fatalf("expected 2 trigger words loaded, got %d: %v", len(set), set)
	}
	if !c.matchesTriggerWords(context.Background(), &telego.Message{Text: "эй Паша"}) {
		t.Error("expected trigger match after loading from IDENTITY.md")
	}
}

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
