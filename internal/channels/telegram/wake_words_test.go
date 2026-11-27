package telegram

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mymmrac/telego"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// triggerFakeStore implements just the two AgentStore methods agentTriggerWords
// needs; the embedded interface makes any other call panic (none expected).
// GetAgentContextFiles mirrors the real store: it requires tenant scope in ctx.
type triggerFakeStore struct {
	store.AgentStore
	agentID uuid.UUID                                  // ID returned by GetByKey when byKey has no entry
	byKey   map[string]*store.AgentData                // optional key → agent mapping
	files   []store.AgentContextFileData               // used when byID has no entry
	byID    map[uuid.UUID][]store.AgentContextFileData // optional per-agent context files
}

func (f *triggerFakeStore) GetByKey(ctx context.Context, key string) (*store.AgentData, error) {
	if ag, ok := f.byKey[key]; ok {
		return ag, nil
	}
	return &store.AgentData{BaseModel: store.BaseModel{ID: f.agentID}}, nil
}

func (f *triggerFakeStore) GetAgentContextFiles(ctx context.Context, id uuid.UUID) ([]store.AgentContextFileData, error) {
	if store.TenantIDFromContext(ctx) == uuid.Nil {
		return nil, fmt.Errorf("tenant_id required")
	}
	if files, ok := f.byID[id]; ok {
		return files, nil
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
			{FileName: "IDENTITY.md", Content: "Name: Rex\nTrigger words: Alice, Boss"},
		},
	}
	c := &Channel{BaseChannel: channels.NewBaseChannel(channels.TypeTelegram, nil, nil), agentStore: fake}
	c.SetAgentID("my-agent")
	c.SetTenantID(uuid.New())

	set := c.agentTriggerWords(context.Background(), boundAgentRef{key: "my-agent", id: fake.agentID})
	if len(set) != 2 {
		t.Fatalf("expected 2 trigger words loaded, got %d: %v", len(set), set)
	}
	if !c.matchesTriggerWords(context.Background(), &telego.Message{Text: "hey Alice"}) {
		t.Error("expected trigger match after loading from IDENTITY.md")
	}
}

// newTriggerChannel returns a Channel with a pre-warmed trigger-word cache so
// matchesTriggerWords can be tested without a store.
func newTriggerChannel(words ...string) *Channel {
	c := &Channel{BaseChannel: channels.NewBaseChannel(channels.TypeTelegram, nil, nil)}
	c.SetAgentID("test-agent")
	c.triggerWords = map[string]triggerWordsEntry{
		"test-agent": {words: normalizeWakeWords(words), at: time.Now()},
	}
	return c
}

func TestChannelMatchesTriggerWords(t *testing.T) {
	ctx := context.Background()
	c := newTriggerChannel("Alice", "Boss")

	if !c.matchesTriggerWords(ctx, &telego.Message{Text: "hey, Alice"}) {
		t.Error("expected match in message text")
	}
	if !c.matchesTriggerWords(ctx, &telego.Message{Caption: "look, boss!"}) {
		t.Error("expected match in media caption")
	}
	if c.matchesTriggerWords(ctx, &telego.Message{Text: "bossy talk"}) {
		t.Error("substring must not match")
	}
	if c.matchesTriggerWords(ctx, &telego.Message{Text: "hello there"}) {
		t.Error("unrelated text must not match")
	}

	// nil agentStore + expired cache → fails open, never matches, never panics.
	empty := &Channel{BaseChannel: channels.NewBaseChannel(channels.TypeTelegram, nil, nil)}
	if empty.matchesTriggerWords(ctx, &telego.Message{Text: "Alice"}) {
		t.Error("channel with no trigger words must never match")
	}
}

// Bound groups must load trigger words for the bound agent, not the channel
// default: one bot instance can serve several agents across groups/topics.
func TestMatchesTriggerWords_UsesBoundGroupAgent(t *testing.T) {
	aID, bID := uuid.New(), uuid.New()
	fake := &triggerFakeStore{
		byKey: map[string]*store.AgentData{
			"agent-a": {BaseModel: store.BaseModel{ID: aID}, AgentKey: "agent-a"},
			"agent-b": {BaseModel: store.BaseModel{ID: bID}, AgentKey: "agent-b"},
		},
		byID: map[uuid.UUID][]store.AgentContextFileData{
			aID: {{FileName: "IDENTITY.md", Content: "Trigger words: Alice"}},
			bID: {{FileName: "IDENTITY.md", Content: "Trigger words: Bob"}},
		},
	}
	c := &Channel{BaseChannel: channels.NewBaseChannel(channels.TypeTelegram, nil, nil), agentStore: fake}
	c.SetAgentID("agent-a")
	c.SetTenantID(uuid.New())
	c.config = config.TelegramConfig{
		Groups: map[string]*config.TelegramGroupConfig{
			"-100": {AgentID: "agent-b"},
		},
	}

	ctx := context.Background()
	bound := &telego.Message{Chat: telego.Chat{ID: -100, Type: "supergroup"}, Text: "hey Bob"}
	if !c.matchesTriggerWords(ctx, bound) {
		t.Error("bound agent's trigger word must match in the bound group")
	}
	if c.matchesTriggerWords(ctx, &telego.Message{Chat: telego.Chat{ID: -100, Type: "supergroup"}, Text: "hey Alice"}) {
		t.Error("default agent's trigger word must not match in the bound group")
	}

	// A group without a binding still uses the channel default agent's set.
	if !c.matchesTriggerWords(ctx, &telego.Message{Chat: telego.Chat{ID: -200, Type: "supergroup"}, Text: "hey Alice"}) {
		t.Error("default agent's trigger word must match in an unbound group")
	}
	if c.matchesTriggerWords(ctx, &telego.Message{Chat: telego.Chat{ID: -200, Type: "supergroup"}, Text: "hey Bob"}) {
		t.Error("bound agent's trigger word must not match in an unbound group")
	}
}

func TestNormalizeWakeWords(t *testing.T) {
	set := normalizeWakeWords([]string{"Alice", "  Boss ", "CHIEF", "", "   "})
	if len(set) != 3 {
		t.Fatalf("expected 3 normalized words, got %d: %v", len(set), set)
	}
	for _, w := range []string{"alice", "boss", "chief"} {
		if _, ok := set[w]; !ok {
			t.Errorf("expected normalized set to contain %q", w)
		}
	}
}

func TestTextHasWakeWord(t *testing.T) {
	set := normalizeWakeWords([]string{"Alice", "Boss", "Chief"})

	cases := []struct {
		name string
		text string
		want bool
	}{
		{"exact", "Alice", true},
		{"uppercase", "ALICE come here", true},
		{"trailing punctuation", "alice,", true},
		{"leading phrase and bang", "hey, chief!", true},
		{"mid sentence", "well boss you", true},
		{"substring not whole word", "chieftain nearby", false},
		{"plural not whole word", "bosses here", false},
		{"empty text", "", false},
		{"unrelated", "hello everyone", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := textHasWakeWord(tc.text, set); got != tc.want {
				t.Errorf("textHasWakeWord(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

// Non-ASCII letters must be treated as word characters — the whole reason the
// matcher tokenizes on unicode.IsLetter instead of using an ASCII \b regex.
func TestTextHasWakeWord_UnicodeWordBoundary(t *testing.T) {
	set := normalizeWakeWords([]string{"café"})
	if !textHasWakeWord("meet at café tonight", set) {
		t.Error("whole non-ASCII word must match")
	}
	if textHasWakeWord("two cafés opened", set) {
		t.Error("plural (trailing non-ASCII letter) must not match — proves the tokenizer is Unicode-aware, unlike ASCII \\b")
	}
}

func TestTextHasWakeWord_EmptySet(t *testing.T) {
	if textHasWakeWord("Alice here", nil) {
		t.Error("empty set must never match")
	}
	if textHasWakeWord("Alice here", map[string]struct{}{}) {
		t.Error("empty set must never match")
	}
}

func TestResolveMessageSender(t *testing.T) {
	// Channel post: From is nil → synthesize sender from the channel.
	post := &telego.Message{Chat: telego.Chat{ID: -100123, Type: "channel", Title: "My Channel", Username: "mychan"}}
	u, isCh := resolveMessageSender(post)
	if !isCh {
		t.Error("channel post must be detected as channel")
	}
	if u == nil || u.ID != -100123 || u.FirstName != "My Channel" || u.Username != "mychan" {
		t.Errorf("synthetic sender = %+v, want channel-derived", u)
	}

	// Group message with a real sender: returned unchanged, not a channel.
	from := &telego.User{ID: 42, FirstName: "Ann"}
	grp := &telego.Message{Chat: telego.Chat{ID: -55, Type: "supergroup"}, From: from}
	u2, isCh2 := resolveMessageSender(grp)
	if isCh2 || u2 != from {
		t.Errorf("group sender = %+v isChannel=%v, want original sender, not channel", u2, isCh2)
	}

	// DM with no From and not a channel: nil sender (dropped by caller).
	dm := &telego.Message{Chat: telego.Chat{ID: 7, Type: "private"}}
	if u3, isCh3 := resolveMessageSender(dm); u3 != nil || isCh3 {
		t.Errorf("private no-From = %+v isChannel=%v, want nil/false", u3, isCh3)
	}
}
