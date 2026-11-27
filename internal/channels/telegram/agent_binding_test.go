package telegram

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// boundAgentFakeStore implements just the AgentStore methods validateBoundAgent
// needs; the embedded interface makes any other call panic (none expected).
// It records whether lookups carried tenant scope, mirroring the real scoped
// store behavior tested in TestAgentTriggerWords_ScopesTenantAndLoadsIdentity.
type boundAgentFakeStore struct {
	store.AgentStore
	byID       map[uuid.UUID]*store.AgentData
	byKey      map[string]*store.AgentData
	lookups    atomic.Int64
	lastTenant uuid.UUID
	missingErr error
}

func (f *boundAgentFakeStore) GetByKey(ctx context.Context, key string) (*store.AgentData, error) {
	f.lookups.Add(1)
	f.lastTenant = store.TenantIDFromContext(ctx)
	if ag, ok := f.byKey[key]; ok {
		return ag, nil
	}
	return nil, f.missingErr
}

func (f *boundAgentFakeStore) GetByID(ctx context.Context, id uuid.UUID) (*store.AgentData, error) {
	f.lookups.Add(1)
	f.lastTenant = store.TenantIDFromContext(ctx)
	if ag, ok := f.byID[id]; ok {
		return ag, nil
	}
	return nil, f.missingErr
}

// newBoundAgentChannel builds a Channel with the given store and channel default
// agent, the same construction pattern as wake_words_test.go.
func newBoundAgentChannel(agentStore store.AgentStore, defaultAgent string) *Channel {
	c := &Channel{BaseChannel: channels.NewBaseChannel(channels.TypeTelegram, nil, nil), agentStore: agentStore}
	c.SetAgentID(defaultAgent)
	c.SetTenantID(uuid.New())
	return c
}

func TestResolveBoundAgentID_Empty(t *testing.T) {
	c := newBoundAgentChannel(nil, "default-agent")
	if got := c.resolveBoundAgentID(context.Background(), ""); got != "" {
		t.Errorf("resolveBoundAgentID(\"\") = %q, want \"\"", got)
	}
}

func TestResolveBoundAgentID_NoAgentStore_TrustsConfig(t *testing.T) {
	// Config-based channels have no agent store; the binding is trusted as-is
	// (the inbound consumer fails closed if the agent turns out to be unknown).
	c := newBoundAgentChannel(nil, "default-agent")
	if got := c.resolveBoundAgentID(context.Background(), "bound-agent"); got != "bound-agent" {
		t.Errorf("resolveBoundAgentID = %q, want %q", got, "bound-agent")
	}
}

func TestResolveBoundAgentID_KeyLookup(t *testing.T) {
	id := uuid.New()
	fake := &boundAgentFakeStore{
		missingErr: errors.New("not found"),
		byKey:      map[string]*store.AgentData{"agent-b": {BaseModel: store.BaseModel{ID: id}, AgentKey: "agent-b"}},
	}
	c := newBoundAgentChannel(fake, "default-agent")

	if got := c.resolveBoundAgentID(context.Background(), "agent-b"); got != "agent-b" {
		t.Errorf("resolveBoundAgentID(agent-b) = %q, want %q", got, "agent-b")
	}
	if fake.lastTenant != c.TenantID() {
		t.Errorf("lookup tenant = %s, want %s (must be tenant-scoped)", fake.lastTenant, c.TenantID())
	}
}

func TestResolveBoundAgentID_UUIDNormalizesToKey(t *testing.T) {
	// UUID in config is normalized to agent_key: routing and session keys use
	// agent_key, so a UUID and its key must not produce two different routes.
	id := uuid.New()
	fake := &boundAgentFakeStore{
		missingErr: errors.New("not found"),
		byID:       map[uuid.UUID]*store.AgentData{id: {BaseModel: store.BaseModel{ID: id}, AgentKey: "agent-b"}},
	}
	c := newBoundAgentChannel(fake, "default-agent")

	if got := c.resolveBoundAgentID(context.Background(), id.String()); got != "agent-b" {
		t.Errorf("resolveBoundAgentID(uuid) = %q, want %q", got, "agent-b")
	}
}

func TestResolveBoundAgentID_UnknownAgent_FallsBackAndCaches(t *testing.T) {
	fake := &boundAgentFakeStore{missingErr: errors.New("not found")}
	c := newBoundAgentChannel(fake, "default-agent")

	for i := range 3 {
		if got := c.resolveBoundAgentID(context.Background(), "ghost-agent"); got != "" {
			t.Fatalf("iteration %d: resolveBoundAgentID(ghost) = %q, want \"\"", i, got)
		}
	}
	// Invalid result is cached too: exactly one store lookup for three attempts.
	if n := fake.lookups.Load(); n != 1 {
		t.Errorf("store lookups = %d, want 1 (invalid binding must be cached)", n)
	}
}

func TestResolveBoundAgentID_CachesValidResult(t *testing.T) {
	id := uuid.New()
	fake := &boundAgentFakeStore{
		missingErr: errors.New("not found"),
		byKey:      map[string]*store.AgentData{"agent-b": {BaseModel: store.BaseModel{ID: id}, AgentKey: "agent-b"}},
	}
	c := newBoundAgentChannel(fake, "default-agent")

	c.resolveBoundAgentID(context.Background(), "agent-b")
	c.resolveBoundAgentID(context.Background(), "agent-b")
	if n := fake.lookups.Load(); n != 1 {
		t.Errorf("store lookups = %d, want 1 (valid binding must be cached)", n)
	}
}

func TestEffectiveInboundAgentID(t *testing.T) {
	id := uuid.New()
	bound := &store.AgentData{BaseModel: store.BaseModel{ID: id}, AgentKey: "agent-b"}
	fake := &boundAgentFakeStore{
		missingErr: errors.New("not found"),
		byKey:      map[string]*store.AgentData{"agent-b": bound},
		byID:       map[uuid.UUID]*store.AgentData{id: bound},
	}

	cfg := config.TelegramConfig{
		Groups: map[string]*config.TelegramGroupConfig{
			"-1001": {AgentID: "agent-b"},
			"-1002": {
				AgentID: "agent-b",
				Topics: map[string]*config.TelegramTopicConfig{
					"7": {AgentID: "agent-c"}, // unknown on purpose
				},
			},
		},
	}

	tests := []struct {
		name    string
		isGroup bool
		chat    string
		topicID int
		want    string
	}{
		{"dm falls back to default", false, "42", 0, "default-agent"},
		{"group without binding uses default", true, "-999", 0, "default-agent"},
		{"group binding wins over default", true, "-1001", 0, "agent-b"},
		{"unknown binding falls back to default", true, "-1002", 7, "default-agent"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newBoundAgentChannel(fake, "default-agent")
			c.config = cfg
			got := c.effectiveInboundAgentID(context.Background(), tc.chat, tc.topicID, tc.isGroup)
			if got != tc.want {
				t.Errorf("effectiveInboundAgentID(%s, topic=%d, group=%v) = %q, want %q", tc.chat, tc.topicID, tc.isGroup, got, tc.want)
			}
		})
	}
}

func TestEffectiveAgentUUID(t *testing.T) {
	defID, boundID := uuid.New(), uuid.New()
	fake := &boundAgentFakeStore{
		missingErr: errors.New("not found"),
		byKey: map[string]*store.AgentData{
			"default-agent": {BaseModel: store.BaseModel{ID: defID}, AgentKey: "default-agent"},
			"agent-b":       {BaseModel: store.BaseModel{ID: boundID}, AgentKey: "agent-b"},
		},
	}
	cfg := config.TelegramConfig{
		Groups: map[string]*config.TelegramGroupConfig{
			"-1001": {AgentID: "agent-b"},
			"-1002": {AgentID: "ghost-agent"}, // unknown on purpose
		},
	}

	tests := []struct {
		name    string
		isGroup bool
		chat    string
		want    uuid.UUID
	}{
		{"dm uses default agent uuid", false, "42", defID},
		{"bound group uses bound agent uuid", true, "-1001", boundID},
		{"unbound group uses default agent uuid", true, "-999", defID},
		{"unknown binding falls back to default agent uuid", true, "-1002", defID},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newBoundAgentChannel(fake, "default-agent")
			c.config = cfg
			got, err := c.effectiveAgentUUID(context.Background(), tc.chat, 0, tc.isGroup)
			if err != nil {
				t.Fatalf("effectiveAgentUUID error: %v", err)
			}
			if got != tc.want {
				t.Errorf("effectiveAgentUUID = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestEffectiveAgentUUID_NoStore(t *testing.T) {
	// Config-trust channel: a UUID literal stays usable for permission lookups,
	// an agent_key cannot be resolved (callers skip the check rather than guess).
	id := uuid.New()
	c := newBoundAgentChannel(nil, "default-agent")
	c.config = config.TelegramConfig{
		Groups: map[string]*config.TelegramGroupConfig{
			"-1001": {AgentID: id.String()},
			"-1002": {AgentID: "agent-b"},
		},
	}

	if got, err := c.effectiveAgentUUID(context.Background(), "-1001", 0, true); err != nil || got != id {
		t.Errorf("uuid binding: got %s, err %v; want %s", got, err, id)
	}
	if _, err := c.effectiveAgentUUID(context.Background(), "-1002", 0, true); err == nil {
		t.Error("agent_key binding without a store must not resolve to a UUID")
	}
}

func TestEffectiveInboundAgentID_TopicOverridesGroup(t *testing.T) {
	groupAgent := &store.AgentData{AgentKey: "agent-group"}
	topicAgent := &store.AgentData{AgentKey: "agent-topic"}
	fake := &boundAgentFakeStore{
		missingErr: errors.New("not found"),
		byKey: map[string]*store.AgentData{
			"agent-group": groupAgent,
			"agent-topic": topicAgent,
		},
	}
	cfg := config.TelegramConfig{
		Groups: map[string]*config.TelegramGroupConfig{
			"-1001": {
				AgentID: "agent-group",
				Topics: map[string]*config.TelegramTopicConfig{
					"5": {AgentID: "agent-topic"},
				},
			},
		},
	}
	c := newBoundAgentChannel(fake, "default-agent")
	c.config = cfg

	if got := c.effectiveInboundAgentID(context.Background(), "-1001", 5, true); got != "agent-topic" {
		t.Errorf("topic binding = %q, want agent-topic", got)
	}
	if got := c.effectiveInboundAgentID(context.Background(), "-1001", 0, true); got != "agent-group" {
		t.Errorf("group binding (no topic) = %q, want agent-group", got)
	}
}
